package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"haganeworks/internal/cwtools"
	"haganeworks/internal/settings"
)

func TestMacAppSelectionStoresAndReadsGameDataRoot(t *testing.T) {
	a := newTestAPI(t)
	root := writeSyntheticGame(t)
	bundle := filepath.Join(root, "hoi4.app")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "common", "example.txt"), []byte("data root"), 0o644); err != nil {
		t.Fatal(err)
	}
	var status appStatus
	if code := a.call("PUT", "/api/app/settings", map[string]any{"game_dir": bundle}, &status); code != http.StatusOK {
		t.Fatalf("select app = %d", code)
	}
	if status.Settings.GameDir != root || status.Game == nil || status.Game.Dir != root {
		t.Fatalf("selected game = %+v, want normalized root %s", status, root)
	}
	a.waitReady()
	store, err := settings.Open(filepath.Join(filepath.Dir(a.dir), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(func(s *settings.Settings) { s.GameDir = bundle }); err != nil {
		t.Fatal(err)
	}
	legacy := app{Deps: Deps{Settings: store}}
	game, err := legacy.game()
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := fs.ReadFile(game, "common/example.txt"); err != nil || string(raw) != "data root" {
		t.Fatalf("read through old bundle setting = %q, %v", raw, err)
	}
}

func TestReferenceTablesRoute(t *testing.T) {
	a := newTestAPI(t)
	var tables map[string]json.RawMessage
	if code := a.call("GET", "/api/app/reference-tables", nil, &tables); code != http.StatusOK || len(tables) != 2 {
		t.Fatalf("reference tables: %d %v", code, tables)
	}
	var ideologies struct {
		Groups        []string         `json:"groups"`
		SubIdeologies []map[string]any `json:"sub_ideologies"`
	}
	if err := json.Unmarshal(tables["ideologies"], &ideologies); err != nil || len(ideologies.Groups) != 4 || len(ideologies.SubIdeologies) != 22 {
		t.Errorf("ideologies = %s (%v)", tables["ideologies"], err)
	}
	var colors struct {
		Global map[string]string            `json:"global"`
		Fonts  map[string]map[string]string `json:"fonts"`
	}
	if err := json.Unmarshal(tables["text_colors"], &colors); err != nil || colors.Global["G"] != "rgb(0, 159, 3)" || colors.Fonts["hoi_18mbs"]["G"] != "rgb(86, 172, 91)" {
		t.Errorf("text_colors = %s (%v)", tables["text_colors"], err)
	}
}

func TestReferenceTablesFromThePlayersFolder(t *testing.T) {
	a := newTestAPIWith(t, map[string]string{
		"ideologies.json":  `{"groups": [], "sub_ideologies": []}`,
		"text_colors.json": `{"global": {}, "fonts": {}}`,
	})
	var tables map[string]json.RawMessage
	if code := a.call("GET", "/api/app/reference-tables", nil, &tables); code != http.StatusOK {
		t.Fatalf("reference tables: %d", code)
	}
	if string(tables["ideologies"]) != `{"groups":[],"sub_ideologies":[]}` || string(tables["text_colors"]) != `{"global":{},"fonts":{}}` {
		t.Errorf("tables = %s / %s", tables["ideologies"], tables["text_colors"])
	}
}

func TestSettingsShowTheCWToolsFolder(t *testing.T) {
	a := newTestAPI(t)
	t.Cleanup(func() { cwtools.SetDir("") })
	var status appStatus
	if code := a.call("PUT", "/api/app/settings", map[string]any{"cwtools_dir": ""}, &status); code != http.StatusOK {
		t.Fatalf("settings: %d", code)
	}
	if status.CWTools.Dir != "" {
		t.Skipf("CWTools rules found automatically in %s", status.CWTools.Dir)
	}
	if status.CWTools.Modifiers != 0 || !strings.Contains(status.CWTools.Error, cwtools.ModifiersFile) {
		t.Errorf("without rules: %+v", status.CWTools)
	}

	dir, _ := filepath.Abs(filepath.Join("..", "..", filepath.FromSlash(cwtools.Folder)))
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(cwtools.ModifiersFile))); err != nil {
		t.Skipf("no CWTools checkout: %v", err)
	}
	if code := a.call("PUT", "/api/app/settings", map[string]any{"cwtools_dir": dir}, &status); code != http.StatusOK {
		t.Fatalf("settings: %d", code)
	}
	if status.Settings.CWToolsDir != dir || status.CWTools != (cwtools.Status{Dir: dir, Modifiers: 803}) {
		t.Errorf("with the checkout: %q %+v", status.Settings.CWToolsDir, status.CWTools)
	}
	var automatic appStatus
	if code := a.call("PUT", "/api/app/settings", map[string]any{"cwtools_dir": ""}, &automatic); code != http.StatusOK || automatic.Settings.CWToolsDir != "" || automatic.CWTools.Dir != "" {
		t.Errorf("back to automatic: %d %+v", code, automatic)
	}
	if code := a.call("PUT", "/api/app/settings", map[string]any{"cwtools_dir": "relative/folder"}, nil); code != http.StatusBadRequest {
		t.Errorf("a relative folder: %d", code)
	}
}

func TestSettingsPort(t *testing.T) {
	a := newTestAPI(t)
	settingsFile := filepath.Join(filepath.Dir(a.dir), "settings.json")
	var status appStatus
	if code := a.call("GET", "/api/app/status", nil, &status); code != http.StatusOK || status.Port != testPort || status.ConfiguredPort != settings.DefaultPort {
		t.Fatalf("status: %d port %d configured %d", code, status.Port, status.ConfiguredPort)
	}
	if code := a.call("PUT", "/api/app/settings", map[string]any{"port": 40000}, &status); code != http.StatusOK {
		t.Fatalf("settings: %d", code)
	}
	if status.Settings.Port != 40000 || status.ConfiguredPort != 40000 || status.Port != testPort {
		t.Errorf("after choosing 40000: %+v", status)
	}
	if raw, err := os.ReadFile(settingsFile); err != nil || !strings.Contains(string(raw), `"port": 40000`) {
		t.Errorf("settings.json = %s (%v)", raw, err)
	}
	for _, port := range []any{80, 65536, -1, 1.5, "40001"} {
		if code := a.call("PUT", "/api/app/settings", map[string]any{"port": port}, nil); code != http.StatusBadRequest {
			t.Errorf("port %v: %d", port, code)
		}
	}
	var reset appStatus
	if code := a.call("PUT", "/api/app/settings", map[string]any{"port": 0}, &reset); code != http.StatusOK || reset.Settings.Port != 0 || reset.ConfiguredPort != settings.DefaultPort {
		t.Errorf("back to the default: %d %+v", code, reset)
	}
	if raw, err := os.ReadFile(settingsFile); err != nil || strings.Contains(string(raw), `"port"`) {
		t.Errorf("settings.json = %s (%v)", raw, err)
	}
}

func TestSettingsCPU(t *testing.T) {
	previous := runtime.GOMAXPROCS(0)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })
	a := newTestAPI(t)
	settingsFile := filepath.Join(filepath.Dir(a.dir), "settings.json")
	cores := runtime.NumCPU()
	var status appStatus
	if code := a.call("GET", "/api/app/status", nil, &status); code != http.StatusOK || status.CPU != settings.CPUAll || status.CPUCores != cores || status.Cores != cores {
		t.Fatalf("status: %d cpu %q cores %d of %d", code, status.CPU, status.CPUCores, status.Cores)
	}
	for _, choice := range []struct {
		cpu   string
		cores int
	}{{settings.CPUOne, 1}, {settings.CPUHalf, max(1, cores/2)}, {settings.CPUAll, cores}} {
		if code := a.call("PUT", "/api/app/settings", map[string]any{"cpu": choice.cpu}, &status); code != http.StatusOK {
			t.Fatalf("cpu %q: %d", choice.cpu, code)
		}
		if status.CPU != choice.cpu || status.CPUCores != choice.cores || runtime.GOMAXPROCS(0) != choice.cores {
			t.Errorf("cpu %q: reply %q %d, GOMAXPROCS %d, want %d", choice.cpu, status.CPU, status.CPUCores, runtime.GOMAXPROCS(0), choice.cores)
		}
		if raw, err := os.ReadFile(settingsFile); err != nil || !strings.Contains(string(raw), `"cpu": "`+choice.cpu+`"`) {
			t.Errorf("settings.json = %s (%v)", raw, err)
		}
	}
	for _, cpu := range []any{"", "2", "75%", 1, true} {
		if code := a.call("PUT", "/api/app/settings", map[string]any{"cpu": cpu}, nil); code != http.StatusBadRequest {
			t.Errorf("cpu %v: %d", cpu, code)
		}
	}

	store, err := settings.Open(settingsFile)
	if err == nil {
		_, err = store.Update(func(s *settings.Settings) { s.CPU = settings.CPUOne })
	}
	if err != nil {
		t.Fatal(err)
	}
	Register(http.NewServeMux(), Deps{Settings: store, GameIndexDir: filepath.Join(t.TempDir(), "game-index"), Port: testPort, FindGame: noInstalledGame})
	if got := runtime.GOMAXPROCS(0); got != 1 {
		t.Errorf("GOMAXPROCS after a start with cpu 1 = %d", got)
	}
}
