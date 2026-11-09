package cwtools

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func useDir(t *testing.T, dir string) {
	t.Helper()
	SetDir(dir)
	t.Cleanup(func() { SetDir("") })
}

func writeRules(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, filepath.FromSlash(ModifiersFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestParseModifiersMatchesTheFormerCatalog(t *testing.T) {
	dir, _ := filepath.Abs(filepath.Join("..", "..", filepath.FromSlash(Folder)))
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(ModifiersFile)))
	if err != nil {
		t.Skipf("no CWTools checkout: %v", err)
	}
	modifiers, patterns, err := ParseModifiers(raw)
	if err != nil {
		t.Fatal(err)
	}
	digest := func(list []string) string {
		sum := sha256.Sum256([]byte(strings.Join(list, "\n")))
		return hex.EncodeToString(sum[:])
	}
	if len(modifiers) != 803 || digest(modifiers) != "645dcc2907a15e1bc01be982b311a843472e3ebe25acf0c0278a6657bb28ae15" {
		t.Errorf("modifiers: %d, digest %s", len(modifiers), digest(modifiers))
	}
	if len(patterns) != 37 || digest(patterns) != "79325b16fb1fa2c2f782db7fea8f3a46990573ce4e9d13216461c4b1c2bba9fd" {
		t.Errorf("patterns: %d, digest %s: %v", len(patterns), digest(patterns), patterns)
	}

	useDir(t, dir)
	catalog, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Dir != dir || len(catalog.Modifiers) != 803 || (catalog.Commit != "unknown" && !commitRE.MatchString(catalog.Commit)) {
		t.Errorf("catalog: %s, %d names, commit %q", catalog.Dir, len(catalog.Modifiers), catalog.Commit)
	}
	if status := CurrentStatus(); status.Dir != dir || status.Modifiers != 803 || status.Error != "" {
		t.Errorf("status = %+v", status)
	}
}

func TestParseModifiersReadsOnlyTheModifiersBlock(t *testing.T) {
	modifiers, patterns, err := ParseModifiers([]byte("\xef\xbb\xbfmodifiers = {\n" +
		"  # country\n" +
		"\tpolitical_power_gain = country # a comment\n" +
		"\t<ideology>_drift = politics\n" +
		"\tenum[tech_category]_cost_factor = country\n" +
		"\tenum[sub_unit_modifiers]\n" +
		"\tpolitical_power_gain = politics\n" +
		"}\n" +
		"enums = {\n" +
		"\tenum[general_modifiers_enum] = {\n" +
		"\t\tnot_a_modifier\n" +
		"\t}\n" +
		"}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(modifiers, []string{"political_power_gain"}) || !reflect.DeepEqual(patterns, []string{"<ideology>_drift"}) {
		t.Errorf("modifiers %v, patterns %v", modifiers, patterns)
	}
	if _, _, err := ParseModifiers([]byte("enums = {\n}\n")); err == nil {
		t.Error("a file without a modifiers block parsed")
	}
}

func TestLoadWithoutRules(t *testing.T) {
	empty := t.TempDir()
	useDir(t, empty)
	if _, err := Load(); err == nil {
		t.Skip("rules found beside the test binary or in the working folder")
	}
	status := CurrentStatus()
	if status.Dir != "" || status.Modifiers != 0 || !strings.Contains(status.Error, ModifiersFile) || !strings.Contains(status.Error, empty) {
		t.Errorf("status = %+v", status)
	}
}

func TestLoadRereadsAChangedFile(t *testing.T) {
	dir := writeRules(t, "modifiers = {\n\ta_factor = country\n}\n")
	useDir(t, dir)
	first, err := Load()
	if err != nil || !reflect.DeepEqual(first.Modifiers, []string{"a_factor"}) || first.Commit != "unknown" {
		t.Fatalf("first = %+v, %v", first, err)
	}
	path := filepath.Join(dir, filepath.FromSlash(ModifiersFile))
	if err := os.WriteFile(path, []byte("modifiers = {\n\ta_factor = country\n\tb_factor = state\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	second, err := Load()
	if err != nil || !reflect.DeepEqual(second.Modifiers, []string{"a_factor", "b_factor"}) {
		t.Fatalf("second = %+v, %v", second, err)
	}
}

func TestGitCommitFollowsGitdirAndPackedRefs(t *testing.T) {
	dir := writeRules(t, "modifiers = {\n\ta_factor = country\n}\n")
	gitDir := filepath.Join(t.TempDir(), "modules", "rules")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const commit = "0ddf7f0a13aa8ec0da4b10f2b854fecd5c94d15f"
	files := map[string]string{
		filepath.Join(dir, ".git"):           "gitdir: " + gitDir + "\n",
		filepath.Join(gitDir, "HEAD"):        "ref: refs/heads/master\n",
		filepath.Join(gitDir, "packed-refs"): "# pack-refs with: peeled fully-peeled sorted\n" + commit + " refs/heads/master\n",
	}
	for path, body := range files {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := gitCommit(dir); got != commit {
		t.Errorf("commit = %q", got)
	}
}

func TestMacBundleRulesDoNotDependOnWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "Folder with spaces", "Hagane Works.app", "Contents", "MacOS", "HaganeWorks")
	resources := filepath.Join(filepath.Dir(filepath.Dir(executable)), "Resources", filepath.FromSlash(Folder))
	wd := filepath.Join(root, "unrelated working folder")
	got := candidatePaths("", executable, wd, "darwin")
	want := []string{
		filepath.Join(filepath.Dir(executable), filepath.FromSlash(Folder)),
		resources,
		filepath.Join(wd, filepath.FromSlash(Folder)),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %v; want %v", got, want)
	}
	if err := os.MkdirAll(filepath.Join(resources, "Config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resources, filepath.FromSlash(ModifiersFile)), []byte("modifiers = {\n\ta_factor = country\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(got[1], filepath.FromSlash(ModifiersFile)))
	if err != nil {
		t.Fatal(err)
	}
	names, _, err := ParseModifiers(raw)
	if err != nil || !reflect.DeepEqual(names, []string{"a_factor"}) {
		t.Fatalf("bundled modifiers = %v, %v", names, err)
	}
	configured := filepath.Join(root, "custom rules")
	if got := candidatePaths(configured, executable, wd, "darwin"); got[0] != configured {
		t.Fatalf("explicit rules do not take precedence: %v", got)
	}
	if got := candidatePaths("", executable, wd, "windows"); len(got) != 2 {
		t.Fatalf("macOS-only rules leaked onto Windows: %v", got)
	}
}
