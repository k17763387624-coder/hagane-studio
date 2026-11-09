package gamedir

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const steamFolderName = "Hearts of Iron IV"

var ErrNotFound = errors.New("hearts of iron iv installation not found")

type Install struct {
	Dir     string `json:"dir"`
	Version string `json:"version"`
}

func Valid(dir string) bool {
	return dataRoot(dir) != ""
}

func validDataRoot(dir string) bool {
	if dir == "" {
		return false
	}
	for _, name := range []string{"common", "interface", "gfx", "localisation"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !info.IsDir() {
			return false
		}
	}
	return true
}

func dataRoot(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	dir = filepath.Clean(dir)
	if validDataRoot(dir) {
		return dir
	}
	if !strings.EqualFold(filepath.Ext(dir), ".app") {
		return ""
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	for _, candidate := range []string{
		filepath.Join(dir, "Contents", "Resources"),
		filepath.Join(dir, "Contents", "Resources", "game"),
		filepath.Join(dir, "Contents", "MacOS"),
		filepath.Dir(dir),
	} {
		if validDataRoot(candidate) {
			return candidate
		}
	}
	return ""
}

func Inspect(dir string) (Install, error) {
	dir = dataRoot(dir)
	if dir == "" {
		return Install{}, ErrNotFound
	}
	return Install{Dir: dir, Version: readVersion(dir)}, nil
}

func Find() (Install, error) {
	return findInLibraries(steamLibraries())
}

func findInLibraries(libraries []string) (Install, error) {
	for _, library := range libraries {
		root := filepath.Join(library, "steamapps", "common", steamFolderName)
		for _, candidate := range []string{root, filepath.Join(root, "hoi4.app"), filepath.Join(root, steamFolderName+".app")} {
			if install, err := Inspect(candidate); err == nil {
				return install, nil
			}
		}
	}
	return Install{}, ErrNotFound
}

func readVersion(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "launcher-settings.json"))
	if err != nil {
		return ""
	}
	var settings struct {
		RawVersion string `json:"rawVersion"`
	}
	if json.Unmarshal(raw, &settings) != nil {
		return ""
	}
	return settings.RawVersion
}

func steamLibraries() []string {
	return librariesFromRoots(steamRoots())
}

func librariesFromRoots(roots []string) []string {
	seen := map[string]bool{}
	var libraries []string
	add := func(dir string) {
		dir = filepath.Clean(dir)
		key := dir
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if dir == "." || seen[key] {
			return
		}
		seen[key] = true
		libraries = append(libraries, dir)
	}
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		add(root)
		for _, path := range libraryFolderPaths(filepath.Join(root, "steamapps", "libraryfolders.vdf")) {
			add(path)
		}
	}
	return libraries
}

func steamRoots() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		roots := []string{}
		for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
			if dir := os.Getenv(env); dir != "" {
				roots = append(roots, filepath.Join(dir, "Steam"))
			}
		}
		return append(roots, `C:\Program Files (x86)\Steam`, `C:\Program Files\Steam`)
	case "darwin":
		return []string{filepath.Join(home, "Library", "Application Support", "Steam")}
	default:
		return []string{
			filepath.Join(home, ".steam", "steam"),
			filepath.Join(home, ".local", "share", "Steam"),
			filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
		}
	}
}

func libraryFolderPaths(vdf string) []string {
	file, err := os.Open(vdf)
	if err != nil {
		return nil
	}
	defer file.Close()
	var paths []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := quotedFields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		path := strings.ReplaceAll(fields[1], `\\`, `\`)
		if strings.EqualFold(fields[0], "path") || (decimalKey(fields[0]) && filepath.IsAbs(path)) {
			paths = append(paths, path)
		}
	}
	return paths
}

func decimalKey(key string) bool {
	if key == "" {
		return false
	}
	for _, c := range key {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func quotedFields(line string) []string {
	var fields []string
	for {
		start := strings.IndexByte(line, '"')
		if start < 0 {
			return fields
		}
		line = line[start+1:]
		end := 0
		for end < len(line) && line[end] != '"' {
			if line[end] == '\\' {
				end++
			}
			end++
		}
		if end >= len(line) {
			return fields
		}
		fields = append(fields, line[:end])
		line = line[end+1:]
	}
}

func ModDir() string {
	candidates := userDataCandidates()
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return filepath.Join(dir, "mod")
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	return filepath.Join(candidates[0], "mod")
}

func userDataCandidates() []string {
	home, _ := os.UserHomeDir()
	suffix := filepath.Join("Paradox Interactive", "Hearts of Iron IV")
	switch runtime.GOOS {
	case "windows":
		var candidates []string
		for _, documents := range []string{
			filepath.Join(home, "Documents"),
			filepath.Join(os.Getenv("OneDrive"), "Documents"),
			filepath.Join(home, "OneDrive", "Documents"),
		} {
			if documents != "Documents" {
				candidates = append(candidates, filepath.Join(documents, suffix))
			}
		}
		return candidates
	case "darwin":
		return []string{filepath.Join(home, "Documents", suffix)}
	default:
		return []string{filepath.Join(home, ".local", "share", suffix)}
	}
}
