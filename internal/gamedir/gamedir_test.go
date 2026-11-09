package gamedir

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestLibraryFolderPaths(t *testing.T) {
	vdf := filepath.Join(t.TempDir(), "libraryfolders.vdf")
	body := `"libraryfolders"
{
	"0"
	{
		"path"		"C:\\Program Files (x86)\\Steam"
		"label"		""
	}
	"1"
	{
		"path"		"D:\\SteamLibrary"
		"apps"
		{
			"394360"		"123"
		}
	}
}

`
	if err := os.WriteFile(vdf, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := libraryFolderPaths(vdf)
	want := []string{`C:\Program Files (x86)\Steam`, `D:\SteamLibrary`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %q, want %q", got, want)
	}
}

func TestInspectReadsVersion(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"common", "interface", "gfx", "localisation"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "launcher-settings.json"), []byte(`{"rawVersion":"1.19.3.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	install, err := Inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if install.Version != "1.19.3.0" {
		t.Fatalf("version = %q", install.Version)
	}
	if _, err := Inspect(t.TempDir()); err != ErrNotFound {
		t.Fatalf("empty folder: err = %v, want ErrNotFound", err)
	}
}

func TestInspectMacAppResolvesTheDataRoot(t *testing.T) {
	for _, layout := range []string{"beside", "resources", "resources-game", "macos"} {
		t.Run(layout, func(t *testing.T) {
			root := t.TempDir()
			bundle := filepath.Join(root, "Hearts of Iron IV.app")
			data := root
			switch layout {
			case "resources":
				data = filepath.Join(bundle, "Contents", "Resources")
			case "resources-game":
				data = filepath.Join(bundle, "Contents", "Resources", "game")
			case "macos":
				data = filepath.Join(bundle, "Contents", "MacOS")
			}
			if err := os.MkdirAll(bundle, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"common", "interface", "gfx", "localisation"} {
				if err := os.MkdirAll(filepath.Join(data, name), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(data, "launcher-settings.json"), []byte(`{"rawVersion":"1.19.3.0"}`), 0o644); err != nil {
				t.Fatal(err)
			}
			install, err := Inspect(bundle)
			if err != nil || install.Dir != data || install.Version != "1.19.3.0" || !Valid(bundle) {
				t.Fatalf("Inspect app = %+v, %v; want data root %s", install, err, data)
			}
			if _, err := Inspect(filepath.Join(root, "Missing.app")); err != ErrNotFound {
				t.Fatalf("missing app = %v, want ErrNotFound", err)
			}
		})
	}
	if _, err := Inspect(""); err != ErrNotFound {
		t.Fatalf("empty path = %v, want ErrNotFound", err)
	}
	if _, err := Inspect(filepath.Join(t.TempDir(), "Unrelated.app")); err != ErrNotFound {
		t.Fatalf("invalid app = %v, want ErrNotFound", err)
	}
}

func TestSteamExternalLibraryWithSpacesAndLegacyEntries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Steam")
	library := filepath.Join(t.TempDir(), "External Games", "SteamLibrary")
	if err := os.MkdirAll(filepath.Join(root, "steamapps"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "\"libraryfolders\"\n{\n\t\"1\" \"" + library + "\"\n\t\"2\" {\n\t\t\"path\" \"" + library + "\"\n\t}\n}\n"
	if err := os.WriteFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := librariesFromRoots([]string{root, root}), []string{root, library}; !reflect.DeepEqual(got, want) {
		t.Fatalf("libraries = %q, want %q", got, want)
	}
}

func TestMacUserDataAndSteamRoots(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS paths")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got, want := steamRoots(), []string{filepath.Join(home, "Library", "Application Support", "Steam")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Steam roots = %q, want %q", got, want)
	}
	if got, want := ModDir(), filepath.Join(home, "Documents", "Paradox Interactive", "Hearts of Iron IV", "mod"); got != want {
		t.Fatalf("mod directory = %q, want %q", got, want)
	}
}

func TestFindMacAppInSteamLibrary(t *testing.T) {
	library := filepath.Join(t.TempDir(), "External Games")
	data := filepath.Join(library, "steamapps", "common", steamFolderName, "hoi4.app", "Contents", "Resources")
	for _, name := range []string{"common", "interface", "gfx", "localisation"} {
		if err := os.MkdirAll(filepath.Join(data, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	install, err := findInLibraries([]string{filepath.Join(t.TempDir(), "Empty Steam"), library})
	if err != nil || install.Dir != data {
		t.Fatalf("find in libraries = %+v, %v; want %s", install, err, data)
	}
}
