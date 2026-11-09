package cwtools

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	Repository = "https://github.com/cwtools/cwtools-hoi4-config"

	License = "MIT"

	ModifiersFile = "Config/modifiers.cwt"

	Folder = "third_party/cwtools-hoi4-config"
)

type Catalog struct {
	Dir string

	Modifiers []string

	ModifierPatterns []string

	Commit string
}

type Status struct {
	Dir       string `json:"dir"`
	Modifiers int    `json:"modifiers"`
	Error     string `json:"error"`
}

var (
	mu         sync.Mutex
	configured string
	cache      struct {
		path    string
		modTime time.Time
		size    int64
		catalog Catalog
	}
)

func SetDir(dir string) {
	mu.Lock()
	defer mu.Unlock()
	configured = dir
}

func Load() (*Catalog, error) {
	mu.Lock()
	defer mu.Unlock()
	tried := candidates(configured)
	for _, dir := range tried {
		path := filepath.Join(dir, filepath.FromSlash(ModifiersFile))
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		if cache.path != path || !cache.modTime.Equal(info.ModTime()) || cache.size != info.Size() {
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			modifiers, patterns, err := ParseModifiers(raw)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			cache.path, cache.modTime, cache.size = path, info.ModTime(), info.Size()
			cache.catalog = Catalog{Dir: dir, Modifiers: modifiers, ModifierPatterns: patterns}
		}
		catalog := cache.catalog
		catalog.Commit = gitCommit(dir)
		return &catalog, nil
	}
	return nil, fmt.Errorf("no %s in %s", ModifiersFile, strings.Join(tried, ", "))
}

func CurrentStatus() Status {
	catalog, err := Load()
	if err != nil {
		return Status{Error: err.Error()}
	}
	return Status{Dir: catalog.Dir, Modifiers: len(catalog.Modifiers)}
}

func candidates(configured string) []string {
	executable, _ := os.Executable()
	wd, _ := os.Getwd()
	return candidatePaths(configured, executable, wd, runtime.GOOS)
}

func candidatePaths(configured, executable, wd, platform string) []string {
	var out []string
	add := func(dir string) {
		if dir == "" {
			return
		}
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		for _, seen := range out {
			if strings.EqualFold(seen, dir) {
				return
			}
		}
		out = append(out, dir)
	}
	add(configured)
	if executable != "" {
		dir := filepath.Dir(executable)
		add(filepath.Join(dir, filepath.FromSlash(Folder)))
		if platform == "darwin" && filepath.Base(dir) == "MacOS" && filepath.Base(filepath.Dir(dir)) == "Contents" {
			add(filepath.Join(filepath.Dir(dir), "Resources", filepath.FromSlash(Folder)))
		}
	}
	if wd != "" {
		add(filepath.Join(wd, filepath.FromSlash(Folder)))
	}
	return out
}

var (
	blockRE = regexp.MustCompile(`^modifiers\s*=\s*\{$`)
	entryRE = regexp.MustCompile(`^(\S+)\s*=\s*\S+$`)
)

func ParseModifiers(raw []byte) (modifiers, patterns []string, err error) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	names, shapes := map[string]bool{}, map[string]bool{}
	depth, inBlock, found := 0, false, false
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if at := strings.IndexByte(line, '#'); at >= 0 {
			line = line[:at]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if depth == 0 && blockRE.MatchString(line) {
			inBlock, found = true, true
		} else if inBlock && depth == 1 && !strings.ContainsAny(line, "{}") {
			if match := entryRE.FindStringSubmatch(line); match != nil {
				switch name := match[1]; {
				case strings.Contains(name, "enum["):
				case strings.Contains(name, "<"):
					shapes[name] = true
				default:
					names[name] = true
				}
			}
		}
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if depth <= 0 {
			depth, inBlock = 0, false
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	if !found || len(names) == 0 {
		return nil, nil, errors.New("no modifiers block")
	}
	return sortedKeys(names), sortedKeys(shapes), nil
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

var commitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

func gitCommit(dir string) string {
	gitDir := filepath.Join(dir, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		return "unknown"
	}
	if !info.IsDir() {
		raw, err := os.ReadFile(gitDir)
		if err != nil {
			return "unknown"
		}
		target, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir:")
		if !ok {
			return "unknown"
		}
		gitDir = strings.TrimSpace(target)
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(dir, gitDir)
		}
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "unknown"
	}
	ref, symbolic := strings.CutPrefix(strings.TrimSpace(string(head)), "ref:")
	if !symbolic {
		if commitRE.MatchString(ref) {
			return ref
		}
		return "unknown"
	}
	ref = strings.TrimSpace(ref)
	dirs := []string{gitDir}
	if common, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		shared := strings.TrimSpace(string(common))
		if !filepath.IsAbs(shared) {
			shared = filepath.Join(gitDir, shared)
		}
		dirs = append(dirs, shared)
	}
	for _, root := range dirs {
		if raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ref))); err == nil {
			if commit := strings.TrimSpace(string(raw)); commitRE.MatchString(commit) {
				return commit
			}
		}
		if raw, err := os.ReadFile(filepath.Join(root, "packed-refs")); err == nil {
			for _, line := range strings.Split(string(raw), "\n") {
				if commit, name, ok := strings.Cut(strings.TrimSpace(line), " "); ok && name == ref && commitRE.MatchString(commit) {
					return commit
				}
			}
		}
	}
	return "unknown"
}
