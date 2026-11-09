package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"

	"haganeworks/internal/settings"
)

const (
	exitOK          = 0
	exitToolError   = 1
	exitUsage       = 2
	exitUnreachable = 3
)

func main() {
	restore := useUTF8Console()
	code := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	restore()
	os.Exit(code)
}

const usageText = `hw calls the tools of Hagane Works.

Usage:
  hw [--config <settings.json>] [--port <n>] <command> [arguments]

Commands:
  tools [--json]                         list the tools (--json: with input schemas)
  describe <tool> [--json]               a tool's description, input schema and field reference
  call <tool> [<json> | @file.json | -]  call a tool with its input (an argument, a file, or
                                         stdin); prints the result as JSON
  fields [<name>]                        the field references (what each tool field expects)
  status [--json]                        the running program: version, port, game index

Options:
  --config <file>  the program's settings file (default: the user config folder's
                   Hagane Works\settings.json), for its port and tools token
  --port <n>       the program's port (default: the settings' port, else 37330)
  --images <dir>   where call saves pictures a tool returns (default: a folder in the
                   temporary folder); the result names the files

When Hagane Works is not running, hw starts it without a window (HaganeWorks.exe
from hw's own folder) and leaves it running.

Exit codes: 0 done, 1 the tool reported an error, 2 a usage error, 3 the program
could not be reached.
`

type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error { return &usageError{fmt.Sprintf(format, args...)} }

type unreachableError struct{ err error }

func (e *unreachableError) Error() string { return e.err.Error() }

type toolError struct {
	detail string
	code   string
}

func (e *toolError) Error() string {
	if e.code != "" && e.code != "tool_error" {
		return e.detail + " (" + e.code + ")"
	}
	return e.detail
}

type options struct {
	config string
	port   int
	images string
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	global := flag.NewFlagSet("hw", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	var opts options
	global.StringVar(&opts.config, "config", "", "")
	global.IntVar(&opts.port, "port", 0, "")
	global.StringVar(&opts.images, "images", "", "")
	help := global.Bool("help", false, "")
	global.BoolVar(help, "h", false, "")
	if err := global.Parse(args); err != nil {
		return fail(stderr, usagef("%v", err))
	}
	rest := global.Args()
	if *help || len(rest) == 0 || rest[0] == "help" {
		fmt.Fprint(stdout, usageText)
		if len(rest) == 0 && !*help {
			return exitUsage
		}
		return exitOK
	}
	command, rest := rest[0], rest[1:]
	if wantsHelp(rest) {
		fmt.Fprint(stdout, commandHelp(command))
		return exitOK
	}
	var err error
	switch command {
	case "tools":
		err = cmdTools(opts, rest, stdout, stderr)
	case "describe":
		err = cmdDescribe(opts, rest, stdout, stderr)
	case "call":
		err = cmdCall(opts, rest, stdin, stdout, stderr)
	case "fields":
		err = cmdFields(opts, rest, stdout, stderr)
	case "status":
		err = cmdStatus(opts, rest, stdout, stderr)
	default:
		err = usagef("unknown command %q; run hw --help", command)
	}
	return fail(stderr, err)
}

func wantsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" || arg == "-help" {
			return true
		}
	}
	return false
}

func commandHelp(command string) string {
	switch command {
	case "tools":
		return "hw tools [--json]\n\nLists the tools, one line each: the name and the first sentence of its description.\n--json prints every tool with its full description and input schema.\n"
	case "describe":
		return "hw describe <tool> [--json]\n\nPrints a tool's description, its input schema (JSON) and the field reference\nfor its fields. --json prints all of it as one JSON object.\n"
	case "call":
		return "hw call <tool> [<json> | @file.json | -]\n\nCalls a tool. Its input is a JSON object: the argument itself, the file after @,\nor stdin with - (no input means {}). The result is printed as JSON on stdout.\nPictures a tool returns are saved as files (see --images) and named in the result.\nA tool error is printed on stderr and exits 1.\n"
	case "fields":
		return "hw fields [<name>]\n\nWithout a name, the index of field references; with one, that reference\n(e.g. focus, events, tokens).\n"
	case "status":
		return "hw status [--json]\n\nPrints the running program's version, port, folders and game index state.\n"
	}
	return usageText
}

func fail(stderr io.Writer, err error) int {
	if err == nil {
		return exitOK
	}
	fmt.Fprintln(stderr, "hw:", err)
	var usage *usageError
	var unreachable *unreachableError
	switch {
	case errors.As(err, &usage):
		return exitUsage
	case errors.As(err, &unreachable):
		return exitUnreachable
	}
	return exitToolError
}

func flagJSON(command string, args []string, max int) (bool, []string, error) {
	asJSON := false
	var positional []string
	for _, arg := range args {
		switch {
		case arg == "--json" || arg == "-json":
			asJSON = true
		case strings.HasPrefix(arg, "--") && len(arg) > 2:
			return false, nil, usagef("%s: unknown option %s", command, arg)
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) > max {
		return false, nil, usagef("%s: too many arguments; run hw %s --help", command, command)
	}
	return asJSON, positional, nil
}

func cmdTools(opts options, args []string, stdout, stderr io.Writer) error {
	asJSON, _, err := flagJSON("tools", args, 0)
	if err != nil {
		return err
	}
	c, err := connect(opts, stderr)
	if err != nil {
		return err
	}
	var list struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := c.get("/api/tools", &list); err != nil {
		return err
	}
	if asJSON {
		return writeJSON(stdout, list.Tools)
	}
	width := 0
	for _, tool := range list.Tools {
		width = max(width, len(fmt.Sprint(tool["name"])))
	}
	for _, tool := range list.Tools {
		fmt.Fprintf(stdout, "%-*s  %s\n", width, tool["name"], firstSentence(fmt.Sprint(tool["description"])))
	}
	return nil
}

func firstSentence(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if cut := strings.Index(text, ". "); cut >= 0 {
		text = text[:cut+1]
	}
	if runes := []rune(text); len(runes) > 110 {
		text = string(runes[:107]) + "..."
	}
	return text
}

func cmdDescribe(opts options, args []string, stdout, stderr io.Writer) error {
	asJSON, positional, err := flagJSON("describe", args, 1)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return usagef("describe: name a tool; hw tools lists them")
	}
	c, err := connect(opts, stderr)
	if err != nil {
		return err
	}
	var tool struct {
		Name           string          `json:"name"`
		Description    string          `json:"description"`
		InputSchema    json.RawMessage `json:"input_schema"`
		FieldReference *struct {
			Name string `json:"name"`
			Text string `json:"text"`
		} `json:"field_reference"`
	}
	if err := c.get("/api/tools/"+url.PathEscape(positional[0]), &tool); err != nil {
		return err
	}
	if asJSON {
		return writeJSON(stdout, tool)
	}
	schema, err := indentJSON(tool.InputSchema)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s\n\n%s\n\nInput:\n%s\n", tool.Name, tool.Description, schema)
	if tool.FieldReference != nil {
		fmt.Fprintf(stdout, "\nField reference (%s):\n\n%s\n", tool.FieldReference.Name, strings.TrimRight(tool.FieldReference.Text, "\n"))
	}
	return nil
}

func cmdFields(opts options, args []string, stdout, stderr io.Writer) error {
	_, positional, err := flagJSON("fields", args, 1)
	if err != nil {
		return err
	}
	c, err := connect(opts, stderr)
	if err != nil {
		return err
	}
	path := "/api/tools/fields"
	if len(positional) == 1 {
		path += "/" + url.PathEscape(positional[0])
	}
	var doc struct {
		Text string `json:"text"`
	}
	if err := c.get(path, &doc); err != nil {
		return err
	}
	fmt.Fprintln(stdout, strings.TrimRight(doc.Text, "\n"))
	return nil
}

func cmdStatus(opts options, args []string, stdout, stderr io.Writer) error {
	asJSON, _, err := flagJSON("status", args, 0)
	if err != nil {
		return err
	}
	c, err := connect(opts, stderr)
	if err != nil {
		return err
	}
	var list struct {
		Program struct {
			Version     string         `json:"version"`
			Port        int            `json:"port"`
			Ready       bool           `json:"ready"`
			ProjectsDir string         `json:"projects_dir"`
			GameDir     string         `json:"game_dir"`
			GameIndex   map[string]any `json:"game_index"`
		} `json:"program"`
		Tools []json.RawMessage `json:"tools"`
	}
	if err := c.get("/api/tools", &list); err != nil {
		return err
	}
	p := list.Program
	if asJSON {
		return writeJSON(stdout, map[string]any{
			"version": p.Version, "address": c.base, "port": p.Port, "ready": p.Ready,
			"projects_dir": p.ProjectsDir, "game_dir": p.GameDir, "game_index": p.GameIndex,
			"tools": len(list.Tools), "started": c.started,
		})
	}
	fmt.Fprintf(stdout, "Hagane Works %s at %s/", p.Version, c.base)
	if c.started {
		fmt.Fprint(stdout, " (started by hw, without a window)")
	}
	fmt.Fprintln(stdout)
	line := func(label string, value any) { fmt.Fprintf(stdout, "%-16s %v\n", label+":", value) }
	line("ready", p.Ready)
	line("projects folder", orNone(p.ProjectsDir))
	line("game folder", orNone(p.GameDir))
	index := p.GameIndex
	state := fmt.Sprint(index["state"])
	if state == "building" {
		state = fmt.Sprintf("building (%v, %v/%v)", index["step"], index["done"], index["total"])
	}
	if errText, _ := index["error"].(string); errText != "" {
		state += ": " + errText
	}
	line("game index", state)
	line("tools", len(list.Tools))
	return nil
}

func orNone(value string) string {
	if value == "" {
		return "(not set)"
	}
	return value
}

func cmdCall(opts options, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usagef("call: name a tool; hw tools lists them")
	}
	if len(args) > 2 {
		return usagef("call: give the input as one argument (quote the JSON), @file.json or -")
	}
	name := args[0]
	input := []byte("{}")
	if len(args) == 2 {
		var err error
		if input, err = readInput(args[1], stdin); err != nil {
			return err
		}
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(input, &object); err != nil || object == nil {
		return usagef("call: the input must be a JSON object: %s", jsonProblem(err))
	}
	c, err := connect(opts, stderr)
	if err != nil {
		return err
	}
	var result map[string]any
	if err := c.post("/api/tools/"+url.PathEscape(name), input, &result); err != nil {
		return err
	}
	if err := saveImages(result, name, opts.images); err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func jsonProblem(err error) string {
	if err == nil {
		return "got null"
	}
	return err.Error()
}

func readInput(arg string, stdin io.Reader) ([]byte, error) {
	var data []byte
	var err error
	switch {
	case arg == "-":
		data, err = io.ReadAll(stdin)
	case strings.HasPrefix(arg, "@"):
		data, err = os.ReadFile(arg[1:])
		if err != nil {
			return nil, usagef("call: %v", err)
		}
	default:
		return []byte(arg), nil
	}
	if err != nil {
		return nil, err
	}
	return decodeText(data), nil
}

func decodeText(data []byte) []byte {
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		return data[3:]
	case len(data) >= 2 && (data[0] == 0xFF && data[1] == 0xFE || data[0] == 0xFE && data[1] == 0xFF):
		big := data[0] == 0xFE
		units := make([]uint16, 0, len(data)/2)
		for i := 2; i+1 < len(data); i += 2 {
			if big {
				units = append(units, uint16(data[i])<<8|uint16(data[i+1]))
			} else {
				units = append(units, uint16(data[i+1])<<8|uint16(data[i]))
			}
		}
		return []byte(string(utf16.Decode(units)))
	}
	return data
}

func saveImages(result map[string]any, tool, dir string) error {
	images, _ := result["images"].([]any)
	if len(images) == 0 {
		return nil
	}
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "hagane-works-images")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	stamp := time.Now().Format("20060102-150405.000")
	for index, raw := range images {
		image, _ := raw.(map[string]any)
		encoded, _ := image["data_base64"].(string)
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			continue
		}
		ext := ".png"
		switch image["mime_type"] {
		case "image/jpeg":
			ext = ".jpg"
		case "image/webp":
			ext = ".webp"
		}
		path := filepath.Join(dir, fmt.Sprintf("%s-%s-%d%s", tool, strings.ReplaceAll(stamp, ".", ""), index+1, ext))
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
		delete(image, "data_base64")
		image["file"] = path
	}
	return nil
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func indentJSON(raw json.RawMessage) (string, error) {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return "", err
	}
	return out.String(), nil
}

type client struct {
	base    string
	token   string
	started bool
	http    *http.Client
}

var startWait = 60 * time.Second

func connect(opts options, stderr io.Writer) (*client, error) {
	path := opts.config
	if path == "" {
		var err error
		if path, err = settings.DefaultPath(); err != nil {
			return nil, &unreachableError{err}
		}
	} else if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	port := opts.port
	if port == 0 {
		store, err := settings.Open(path)
		if err != nil {
			return nil, &unreachableError{fmt.Errorf("read settings %s: %w", path, err)}
		}
		port = store.Get().ListenPort()
	}
	c := &client{base: fmt.Sprintf("http://127.0.0.1:%d", port), http: &http.Client{Timeout: 15 * time.Minute}}
	if !c.running() {
		if err := c.start(path, port, stderr); err != nil {
			return nil, &unreachableError{err}
		}
	}
	token, err := settings.ReadToolsToken(path)
	if err != nil {
		return nil, &unreachableError{fmt.Errorf("no tools token beside %s (%v); has Hagane Works started with these settings?", path, err)}
	}
	c.token = token
	return c, nil
}

func (c *client) running() bool {
	probe := http.Client{Timeout: 2 * time.Second}
	res, err := probe.Get(c.base + "/api/app/ping")
	if err != nil {
		return false
	}
	res.Body.Close()
	return res.Header.Get("X-Hagane-Works") != ""
}

func (c *client) start(path string, port int, stderr io.Writer) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	program := desktopProgram(self, runtime.GOOS)
	if _, err := os.Stat(program); err != nil {
		return fmt.Errorf("Hagane Works is not running at %s/ and %s was not found to start it", c.base, program)
	}
	args := []string{"--no-window", "--port", fmt.Sprint(port)}
	if path != "" {
		args = append(args, "--config", path)
	}
	pid, err := startDetached(program, args)
	if err != nil {
		return fmt.Errorf("could not start %s: %w", program, err)
	}
	fmt.Fprintf(stderr, "hw: started Hagane Works without a window (process %d) at %s/; open that address in a browser to see it, and quit it there (Settings)\n", pid, c.base)
	deadline := time.Now().Add(startWait)
	for time.Now().Before(deadline) {
		if c.running() {
			c.started = true
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("started %s, but it did not answer at %s/ within %s (is another program using port %d?)", program, c.base, startWait, port)
}

func desktopProgram(self, platform string) string {
	name := "HaganeWorks"
	if platform == "windows" {
		name += ".exe"
	}
	dir := filepath.Dir(self)
	candidates := []string{filepath.Join(dir, name)}
	if platform == "darwin" {
		for _, bundle := range []string{"Hagane Works.app", "HaganeWorks.app"} {
			candidates = append(candidates, filepath.Join(dir, bundle, "Contents", "MacOS", name))
		}
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return candidates[0]
}

func (c *client) get(path string, out any) error { return c.do("GET", path, nil, out) }

func (c *client) post(path string, body []byte, out any) error { return c.do("POST", path, body, out) }

func (c *client) do(method, path string, body []byte, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return &unreachableError{fmt.Errorf("Hagane Works at %s/ did not answer: %w", c.base, err)}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return &unreachableError{err}
	}
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return json.Unmarshal(raw, out)
	}
	var problem struct {
		Detail any    `json:"detail"`
		Code   string `json:"code"`
	}
	_ = json.Unmarshal(raw, &problem)
	detail := fmt.Sprint(problem.Detail)
	if problem.Detail == nil {
		detail = strings.TrimSpace(string(raw))
	}
	switch {
	case res.StatusCode == http.StatusUnauthorized:
		return &unreachableError{fmt.Errorf("Hagane Works at %s/ refused the tools token (%s); is it running with other settings? (--config)", c.base, detail)}
	case res.StatusCode == http.StatusForbidden:
		return &unreachableError{fmt.Errorf("Hagane Works at %s/ refused the request: %s", c.base, detail)}
	case problem.Code == "unknown_tool":
		return usagef("%s", detail)
	case problem.Code == "unknown_field_reference":
		return usagef("%s", detail)
	}
	if detail == "" {
		detail = res.Status
	}
	return &toolError{detail: detail, code: problem.Code}
}
