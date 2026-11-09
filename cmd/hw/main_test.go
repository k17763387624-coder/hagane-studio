package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"haganeworks/internal/project"
	"haganeworks/internal/workbench"
)

const fakeToken = "f00dfeedf00dfeedf00dfeedf00dfeedf00dfeedf00dfeedf00dfeedf00dfeed"

type fakeProgram struct {
	config string
	calls  []string
}

func newFakeProgram(t *testing.T) *fakeProgram {
	t.Helper()
	fake := &fakeProgram{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/app/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	tools := []map[string]any{
		{"name": "create_idea", "description": "Create a national spirit. More text here.", "input_schema": map[string]any{"type": "object"}},
		{"name": "list_projects", "description": "List your HOI4 mod projects (id, name).", "input_schema": map[string]any{"type": "object"}},
	}
	mux.HandleFunc("GET /api/tools", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, http.StatusOK, map[string]any{"tools": tools, "program": map[string]any{
			"version": "test", "port": 1, "ready": true, "projects_dir": "C:/projects", "game_dir": "",
			"game_index": map[string]any{"state": "missing"},
		}})
	})
	mux.HandleFunc("GET /api/tools/fields/{doc}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("doc") != "focus" {
			writeTestJSON(w, http.StatusNotFound, map[string]any{"detail": "Unknown field reference", "code": "unknown_field_reference"})
			return
		}
		writeTestJSON(w, http.StatusOK, map[string]any{"name": "focus", "text": "# Focus fields\n"})
	})
	mux.HandleFunc("GET /api/tools/{name}", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, http.StatusOK, map[string]any{"name": r.PathValue("name"), "description": "Create a national spirit.",
			"input_schema": map[string]any{"type": "object"}, "field_reference": map[string]any{"name": "ideas", "text": "# Ideas\n"}})
	})
	mux.HandleFunc("POST /api/tools/{name}", func(w http.ResponseWriter, r *http.Request) {
		var body bytes.Buffer
		_, _ = body.ReadFrom(r.Body)
		fake.calls = append(fake.calls, "POST "+r.URL.Path+" "+body.String())
		switch r.PathValue("name") {
		case "list_projects":
			writeTestJSON(w, http.StatusOK, map[string]any{"total": 1, "projects": []any{map[string]any{"id": 1, "name": "鋼鉄の嵐"}}})
		case "create_idea":
			writeTestJSON(w, http.StatusUnprocessableEntity, map[string]any{"detail": "idea_id collides", "code": "tool_error"})
		case "get_world_image":
			writeTestJSON(w, http.StatusOK, map[string]any{"width": 2, "images": []any{
				map[string]any{"mime_type": "image/png", "data_base64": base64.StdEncoding.EncodeToString([]byte("PNGDATA"))},
			}})
		default:
			writeTestJSON(w, http.StatusNotFound, map[string]any{"detail": "unknown tool \"" + r.PathValue("name") + "\"", "code": "unknown_tool"})
		}
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Hagane-Works", "1")
		if r.URL.Path != "/api/app/ping" && r.Header.Get("Authorization") != "Bearer "+fakeToken {
			writeTestJSON(w, http.StatusUnauthorized, map[string]any{"detail": "missing or wrong tools token"})
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	fake.config = filepath.Join(dir, "settings.json")
	port := server.Listener.Addr().(*net.TCPAddr).Port
	writeFile(t, fake.config, fmt.Sprintf(`{"projects_dir": "", "port": %d}`, port))
	writeFile(t, filepath.Join(dir, "tools-token"), fakeToken+"\n")
	return fake
}

func writeTestJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hw(stdin string, args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestUsageErrors(t *testing.T) {
	fake := newFakeProgram(t)
	for _, test := range []struct {
		args []string
		want string
	}{
		{nil, "Usage:"},
		{[]string{"--config", fake.config, "frobnicate"}, "unknown command"},
		{[]string{"--config", fake.config, "call"}, "name a tool"},
		{[]string{"--config", fake.config, "call", "create_idea", "{not json"}, "must be a JSON object"},
		{[]string{"--config", fake.config, "call", "create_idea", "[1]"}, "must be a JSON object"},
		{[]string{"--config", fake.config, "call", "create_idea", "{}", "extra"}, "one argument"},
		{[]string{"--config", fake.config, "call", "create_idea", "@" + filepath.Join(t.TempDir(), "missing.json")}, "call:"},
		{[]string{"--config", fake.config, "describe"}, "name a tool"},
		{[]string{"--config", fake.config, "tools", "--frobnicate"}, "unknown option"},
		{[]string{"--nope"}, "flag provided but not defined"},
		{[]string{"--config", fake.config, "call", "nope", "{}"}, "unknown tool"},
		{[]string{"--config", fake.config, "fields", "nope"}, "Unknown field reference"},
	} {
		code, stdout, stderr := hw("", test.args...)
		if code != exitUsage || !strings.Contains(stdout+stderr, test.want) {
			t.Errorf("hw %v: exit %d, %q %q; want exit 2 and %q", test.args, code, stdout, stderr, test.want)
		}
	}
	if len(fake.calls) != 1 {
		t.Errorf("calls = %v", fake.calls)
	}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"call", "--help"}, {"tools", "-h"}} {
		code, stdout, _ := hw("", args...)
		if code != exitOK || !strings.Contains(stdout, "hw") {
			t.Errorf("hw %v: exit %d %q", args, code, stdout)
		}
	}
}

func TestCall(t *testing.T) {
	fake := newFakeProgram(t)
	code, stdout, stderr := hw("", "--config", fake.config, "call", "list_projects")
	if code != exitOK || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil || result["total"] != float64(1) {
		t.Fatalf("stdout %q: %v", stdout, err)
	}
	if !strings.Contains(stdout, "鋼鉄の嵐") {
		t.Errorf("stdout is not UTF-8 text: %q", stdout)
	}
	if fake.calls[0] != "POST /api/tools/list_projects {}" {
		t.Errorf("call = %q", fake.calls[0])
	}

	code, stdout, stderr = hw("", "--config", fake.config, "call", "create_idea", `{"project_id": 1}`)
	if code != exitToolError || stdout != "" || !strings.Contains(stderr, "idea_id collides") {
		t.Errorf("tool error: exit %d, %q %q", code, stdout, stderr)
	}

	if code, _, stderr := hw(`{"from": "stdin"}`, "--config", fake.config, "call", "list_projects", "-"); code != exitOK {
		t.Errorf("stdin: exit %d %q", code, stderr)
	}
	dir := t.TempDir()
	utf8File := filepath.Join(dir, "utf8.json")
	writeFile(t, utf8File, "\xEF\xBB\xBF{\"name\": \"Ünïcode\"}")
	units := utf16.Encode([]rune(`{"name": "钢铁意志"}`))
	raw := []byte{0xFF, 0xFE}
	for _, unit := range units {
		raw = append(raw, byte(unit), byte(unit>>8))
	}
	utf16File := filepath.Join(dir, "utf16.json")
	if err := os.WriteFile(utf16File, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{utf8File, utf16File} {
		if code, _, stderr := hw("", "--config", fake.config, "call", "list_projects", "@"+file); code != exitOK {
			t.Errorf("%s: exit %d %q", file, code, stderr)
		}
	}
	last := fake.calls[len(fake.calls)-2:]
	if last[0] != `POST /api/tools/list_projects {"name": "Ünïcode"}` || last[1] != `POST /api/tools/list_projects {"name": "钢铁意志"}` {
		t.Errorf("file input reached the program as %q", last)
	}

	images := t.TempDir()
	code, stdout, _ = hw("", "--config", fake.config, "--images", images, "call", "get_world_image", "{}")
	if code != exitOK || strings.Contains(stdout, "data_base64") {
		t.Fatalf("images: exit %d %s", code, stdout)
	}
	json.Unmarshal([]byte(stdout), &result)
	file := result["images"].([]any)[0].(map[string]any)["file"].(string)
	if data, err := os.ReadFile(file); err != nil || string(data) != "PNGDATA" || filepath.Dir(file) != images {
		t.Errorf("saved picture %s: %q %v", file, data, err)
	}
}

func TestReadCommands(t *testing.T) {
	fake := newFakeProgram(t)
	code, stdout, _ := hw("", "--config", fake.config, "tools")
	if code != exitOK || !strings.Contains(stdout, "create_idea    Create a national spirit.\n") || strings.Contains(stdout, "More text") {
		t.Errorf("tools: exit %d %q", code, stdout)
	}
	code, stdout, _ = hw("", "--config", fake.config, "tools", "--json")
	var tools []map[string]any
	if code != exitOK || json.Unmarshal([]byte(stdout), &tools) != nil || len(tools) != 2 || tools[0]["input_schema"] == nil {
		t.Errorf("tools --json: exit %d %q", code, stdout)
	}
	code, stdout, _ = hw("", "--config", fake.config, "describe", "create_idea")
	if code != exitOK || !strings.Contains(stdout, "Input:\n{") || !strings.Contains(stdout, "Field reference (ideas):") {
		t.Errorf("describe: exit %d %q", code, stdout)
	}
	code, stdout, _ = hw("", "--config", fake.config, "fields", "focus")
	if code != exitOK || stdout != "# Focus fields\n" {
		t.Errorf("fields: exit %d %q", code, stdout)
	}
	code, stdout, _ = hw("", "--config", fake.config, "status")
	if code != exitOK || !strings.Contains(stdout, "Hagane Works test at http://127.0.0.1:") || !regexp.MustCompile(`(?m)^tools:\s+2$`).MatchString(stdout) {
		t.Errorf("status: exit %d %q", code, stdout)
	}
}

func TestUnreachable(t *testing.T) {
	fake := newFakeProgram(t)

	writeFile(t, filepath.Join(filepath.Dir(fake.config), "tools-token"), "nope\n")
	if code, _, stderr := hw("", "--config", fake.config, "tools"); code != exitUnreachable || !strings.Contains(stderr, "refused the tools token") {
		t.Errorf("wrong token: exit %d %q", code, stderr)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	if code, _, stderr := hw("", "--config", fake.config, "--port", strconv.Itoa(port), "tools"); code != exitUnreachable || !strings.Contains(stderr, "not found to start it") {
		t.Errorf("no program: exit %d %q", code, stderr)
	}
}

func TestDesktopProgramFindsMacBundleBesideCLI(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Hagane macOS")
	self := filepath.Join(dir, "hw")
	bundle := filepath.Join(dir, "Hagane Works.app", "Contents", "MacOS", "HaganeWorks")
	if err := os.MkdirAll(filepath.Dir(bundle), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, bundle, "app")
	if got := desktopProgram(self, "darwin"); got != bundle {
		t.Fatalf("desktop program = %q, want %q", got, bundle)
	}
	writeFile(t, filepath.Join(dir, "HaganeWorks"), "loose binary")
	if got, want := desktopProgram(self, "darwin"), filepath.Join(dir, "HaganeWorks"); got != want {
		t.Fatalf("loose desktop program = %q, want %q", got, want)
	}
	if got, want := desktopProgram(self, "windows"), filepath.Join(dir, "HaganeWorks.exe"); got != want {
		t.Fatalf("Windows desktop program = %q, want %q", got, want)
	}
}

func TestEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the program")
	}
	dir := t.TempDir()
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	for _, build := range [][]string{
		{"build", "-o", filepath.Join(dir, "HaganeWorks"+exe), "haganeworks/cmd/haganeworks"},
		{"build", "-o", filepath.Join(dir, "hw"+exe), "haganeworks/cmd/hw"},
	} {
		if out, err := exec.Command("go", build...).CombinedOutput(); err != nil {
			t.Fatalf("go %v: %v\n%s", build, err, out)
		}
	}
	projects := filepath.Join(dir, "projects")
	if _, err := workbench.CreateProject(project.NewStore(projects), workbench.CreateInput{Name: "End To End", CountryTag: "TST", CountryName: "Testland"}); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	config := filepath.Join(dir, "config", "settings.json")
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	settings, _ := json.Marshal(map[string]any{"projects_dir": projects, "game_dir": filepath.Join(dir, "no-game"), "port": port})
	writeFile(t, config, string(settings))

	runHW := func(args ...string) (int, string, string) {
		cmd := exec.Command(filepath.Join(dir, "hw"+exe), append([]string{"--config", config}, args...)...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return code, stdout.String(), stderr.String()
	}

	code, stdout, stderr := runHW("status")
	if match := regexp.MustCompile(`process (\d+)`).FindStringSubmatch(stderr); match != nil {
		pid, _ := strconv.Atoi(match[1])
		t.Cleanup(func() {
			if process, err := os.FindProcess(pid); err == nil {
				_ = process.Kill()
				time.Sleep(200 * time.Millisecond)
			}
		})
	}
	if code != exitOK || !strings.Contains(stdout, "(started by hw, without a window)") {
		t.Fatalf("status: exit %d %q %q", code, stdout, stderr)
	}

	code, stdout, stderr = runHW("call", "list_projects")
	var listed struct {
		Projects []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"projects"`
	}
	if code != exitOK || json.Unmarshal([]byte(stdout), &listed) != nil || len(listed.Projects) != 1 || listed.Projects[0].Name != "End To End" {
		t.Fatalf("list_projects: exit %d %q %q", code, stdout, stderr)
	}
	input := fmt.Sprintf(`{"project_id": %d, "name": "Iron Will", "idea_id": "TST_iron_will", "country_tag": "TST", "display_name_locs": {"zh": "钢铁意志"}}`, listed.Projects[0].ID)
	code, stdout, stderr = runHW("call", "create_idea", input)
	if code != exitOK || !strings.Contains(stdout, `"idea_id": "TST_iron_will"`) || !strings.Contains(stdout, "钢铁意志") {
		t.Fatalf("create_idea: exit %d %q %q", code, stdout, stderr)
	}
	files, _ := filepath.Glob(filepath.Join(projects, "*", "data", "ideas.json"))
	if len(files) != 1 {
		t.Fatalf("ideas.json: %v", files)
	}
	if raw, err := os.ReadFile(files[0]); err != nil || !strings.Contains(string(raw), "TST_iron_will") {
		t.Errorf("the idea is not in %s: %v", files[0], err)
	}

	code, _, stderr = runHW("call", "get_project_overview", `{"project_id": 999}`)
	if code != exitToolError || stderr == "" {
		t.Errorf("missing project: exit %d %q", code, stderr)
	}
}
