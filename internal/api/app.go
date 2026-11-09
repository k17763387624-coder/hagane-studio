package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"haganeworks/internal/cwtools"
	"haganeworks/internal/domains"
	"haganeworks/internal/export"
	"haganeworks/internal/gamedir"
	"haganeworks/internal/gameindex"
	"haganeworks/internal/gameindex/build"
	"haganeworks/internal/reftables"
	"haganeworks/internal/server"
	"haganeworks/internal/settings"
)

type Deps struct {
	Version  string
	Settings *settings.Store

	Exports *export.Jobs

	Quit func()

	GameIndexDir string

	Port int

	FindGame func() (gamedir.Install, error)
}

type app struct {
	Deps
	index *gameindex.Index
}

func Register(mux *http.ServeMux, deps Deps) {
	applyCPU(deps.Settings.Get())
	reftables.Load(deps.Settings.TablesDir())
	cwtools.SetDir(deps.Settings.Get().CWToolsDir)
	a := &app{Deps: deps}
	root := deps.GameIndexDir
	if root == "" {
		if dir, err := gameindex.DefaultRoot(); err == nil {
			root = dir
		} else {
			log.Printf("game index kept in memory: %v", err)
		}
	}
	index, err := gameindex.New(a.game, build.Datasets(), root)
	if err != nil {
		panic(err)
	}
	a.index = index
	gameindex.SetDefault(index)
	index.Ensure()

	mux.HandleFunc("GET /api/app/status", a.status)
	mux.HandleFunc("PUT /api/app/settings", a.updateSettings)
	mux.HandleFunc("GET /api/app/folders", a.folders)
	mux.HandleFunc("POST /api/app/quit", a.quit)
	mux.HandleFunc("GET /api/app/game-index", a.gameIndexStatus)
	mux.HandleFunc("POST /api/app/game-index/rebuild", a.rebuildGameIndex)
	mux.HandleFunc("GET /api/app/reference-tables", a.referenceTables)
	p := &projects{settings: deps.Settings}
	registerProjects(mux, p)
	registerBaseMods(mux, p)
	registerCountries(mux, p)
	registerExports(mux, p, deps.Exports)
	if err := registerImports(mux, p); err != nil {
		log.Printf("import staging unavailable: %v", err)
	}
	registerDomains(mux, domains.Deps{Open: p.open, Game: a.game})
}

func (a *app) game() (fs.FS, error) {
	install, err := gamedir.Inspect(a.Settings.Get().GameDir)
	if err != nil {
		return nil, errNoGameDir
	}
	return os.DirFS(install.Dir), nil
}

var errNoGameDir = errors.New("set the Hearts of Iron IV folder in settings first")

func applyCPU(current settings.Settings) {
	runtime.GOMAXPROCS(current.CPUCores())
}

type appStatus struct {
	Version  string            `json:"version"`
	Settings settings.Settings `json:"settings"`

	Game *gamedir.Install `json:"game"`

	ModDir string `json:"mod_dir"`

	Ready bool `json:"ready"`

	CWTools cwtools.Status `json:"cwtools"`

	Port           int `json:"port"`
	ConfiguredPort int `json:"configured_port"`

	CPU      string `json:"cpu"`
	CPUCores int    `json:"cpu_cores"`
	Cores    int    `json:"cores"`
}

func (a *app) status(w http.ResponseWriter, r *http.Request) {
	current := a.Settings.Get()

	if current.GameDir == "" {
		find := a.FindGame
		if find == nil {
			find = gamedir.Find
		}
		if install, err := find(); err == nil {
			if saved, err := a.Settings.Update(func(s *settings.Settings) { s.GameDir = install.Dir }); err == nil {
				current = saved
				a.index.Ensure()
			}
		}
	}
	server.JSON(w, http.StatusOK, a.describe(current))
}

func (a *app) gameIndexStatus(w http.ResponseWriter, r *http.Request) {
	server.JSON(w, http.StatusOK, a.index.Status())
}

func (a *app) rebuildGameIndex(w http.ResponseWriter, r *http.Request) {
	server.JSON(w, http.StatusOK, a.index.Rebuild())
}

func (a *app) referenceTables(w http.ResponseWriter, r *http.Request) {
	server.JSON(w, http.StatusOK, map[string]json.RawMessage{
		"ideologies":  reftables.Raw(reftables.IdeologiesFile),
		"text_colors": reftables.Raw(reftables.TextColorsFile),
	})
}

func (a *app) describe(current settings.Settings) appStatus {
	status := appStatus{
		Version: a.Version, Settings: current, ModDir: current.ModDir, Port: a.Port, ConfiguredPort: current.ListenPort(),
		CPU: current.CPUSetting(), CPUCores: current.CPUCores(), Cores: runtime.NumCPU(),
	}
	if install, err := gamedir.Inspect(current.GameDir); err == nil {
		status.Game = &install
	}
	if status.ModDir == "" {
		status.ModDir = gamedir.ModDir()
	}
	status.Ready = current.ProjectsDir != "" && status.Game != nil
	status.CWTools = cwtools.CurrentStatus()
	return status
}

func (a *app) updateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProjectsDir *string `json:"projects_dir"`
		GameDir     *string `json:"game_dir"`
		ModDir      *string `json:"mod_dir"`
		CWToolsDir  *string `json:"cwtools_dir"`
		Port        *int    `json:"port"`
		CPU         *string `json:"cpu"`
	}
	if err := server.ReadJSON(r, 1<<16, &body); err != nil {
		server.Fail(w, http.StatusBadRequest, err)
		return
	}
	if body.ProjectsDir != nil {
		dir, err := absoluteDir(*body.ProjectsDir, true)
		if err != nil {
			server.Fail(w, http.StatusBadRequest, err)
			return
		}
		body.ProjectsDir = &dir
	}
	if body.GameDir != nil {
		install, err := gamedir.Inspect(*body.GameDir)
		if err != nil {
			server.Fail(w, http.StatusBadRequest, errors.New("this folder is not a Hearts of Iron IV installation"))
			return
		}
		body.GameDir = &install.Dir
	}
	if body.ModDir != nil && *body.ModDir != "" {
		dir, err := absoluteDir(*body.ModDir, true)
		if err != nil {
			server.Fail(w, http.StatusBadRequest, err)
			return
		}
		body.ModDir = &dir
	}
	if body.CWToolsDir != nil && *body.CWToolsDir != "" {
		dir, err := absoluteDir(*body.CWToolsDir, false)
		if err != nil {
			server.Fail(w, http.StatusBadRequest, err)
			return
		}
		body.CWToolsDir = &dir
	}
	if body.Port != nil && *body.Port != 0 && (*body.Port < 1024 || *body.Port > 65535) {
		server.Fail(w, http.StatusBadRequest, errors.New("choose a port from 1024 to 65535"))
		return
	}
	if body.CPU != nil && !settings.ValidCPU(*body.CPU) {
		server.Fail(w, http.StatusBadRequest, errors.New(`choose "1", "50%" or "100%" for the CPU use`))
		return
	}
	saved, err := a.Settings.Update(func(s *settings.Settings) {
		if body.ProjectsDir != nil {
			s.ProjectsDir = *body.ProjectsDir
		}
		if body.GameDir != nil {
			s.GameDir = filepath.Clean(*body.GameDir)
		}
		if body.ModDir != nil {
			s.ModDir = *body.ModDir
		}
		if body.CWToolsDir != nil {
			s.CWToolsDir = *body.CWToolsDir
		}
		if body.Port != nil {
			s.Port = *body.Port
		}
		if body.CPU != nil {
			s.CPU = *body.CPU
		}
	})
	if err != nil {
		server.Fail(w, http.StatusInternalServerError, err)
		return
	}
	if body.GameDir != nil {
		a.index.Ensure()
	}
	cwtools.SetDir(saved.CWToolsDir)
	applyCPU(saved)
	server.JSON(w, http.StatusOK, a.describe(saved))
}

func absoluteDir(dir string, create bool) (string, error) {
	dir = strings.TrimSpace(dir)
	if !filepath.IsAbs(dir) {
		return "", errors.New("choose a full folder path")
	}
	dir = filepath.Clean(dir)
	if create {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("not a folder")
	}
	return dir, nil
}

type folderEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func (a *app) folders(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		server.JSON(w, http.StatusOK, map[string]any{"path": "", "parent": nil, "entries": roots()})
		return
	}
	if !filepath.IsAbs(dir) {
		server.Fail(w, http.StatusBadRequest, errors.New("path must be absolute"))
		return
	}
	dir = filepath.Clean(dir)
	items, err := os.ReadDir(dir)
	if err != nil {
		server.Fail(w, http.StatusBadRequest, err)
		return
	}
	entries := []folderEntry{}
	for _, item := range items {
		name := item.Name()
		if !item.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "$") {
			continue
		}
		entries = append(entries, folderEntry{Name: name, Path: filepath.Join(dir, name)})
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	var parent any
	if up := filepath.Dir(dir); up != dir {
		parent = up
	} else {
		parent = ""
	}
	server.JSON(w, http.StatusOK, map[string]any{"path": dir, "parent": parent, "entries": entries})
}

func roots() []folderEntry {
	var entries []folderEntry
	if home, err := os.UserHomeDir(); err == nil {
		entries = append(entries, folderEntry{Name: home, Path: home})
	}
	if runtime.GOOS == "windows" {
		for letter := 'A'; letter <= 'Z'; letter++ {
			drive := string(letter) + `:\`
			if _, err := os.Stat(drive); err == nil {
				entries = append(entries, folderEntry{Name: drive, Path: drive})
			}
		}
	} else {
		entries = append(entries, folderEntry{Name: "/", Path: "/"})
	}
	return entries
}

func (a *app) quit(w http.ResponseWriter, r *http.Request) {
	server.JSON(w, http.StatusOK, map[string]bool{"ok": true})
	if a.Quit != nil {
		go a.Quit()
	}
}
