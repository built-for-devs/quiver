package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/built-for-devs/quiver/internal/apperr"
	"github.com/built-for-devs/quiver/internal/config"
)

// fakeMCP is a minimal Streamable HTTP MCP server. tools maps a tool name to
// the result it returns; calls records every tools/call.
type fakeMCP struct {
	t        *testing.T
	sse      bool
	tools    map[string]map[string]any
	handlers map[string]func(args map[string]any) map[string]any
	calls    []map[string]any
}

func (f *fakeMCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer qvr_good" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		ID     *int            `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.ID == nil { // notification
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var result any
	switch req.Method {
	case "initialize":
		w.Header().Set("Mcp-Session-Id", "sess-1")
		result = map[string]any{
			"protocolVersion": "2025-06-18",
			"serverInfo":      map[string]any{"name": "quiver", "version": "1.2.3"},
		}
	case "tools/call":
		if r.Header.Get("Mcp-Session-Id") != "sess-1" {
			f.t.Errorf("tools/call missing session id")
		}
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		json.Unmarshal(req.Params, &p)
		f.calls = append(f.calls, map[string]any{"name": p.Name, "arguments": p.Arguments})
		res, ok := f.tools[p.Name]
		if h, hok := f.handlers[p.Name]; hok {
			res, ok = h(p.Arguments), true
		}
		if !ok {
			res = map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "Campaign not found"}}}
		}
		result = res
	}
	resp, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})
	if f.sse {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", resp)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(resp)
}

func textResult(s string) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": s}}}
}

// run executes the CLI in an isolated config environment.
func run(t *testing.T, env map[string]string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config"))
	for _, k := range []string{config.EnvToken, config.EnvAPIURL, config.EnvWorkspace} {
		t.Setenv(k, "")
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	root := NewRoot()
	var out, errb bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(args)
	err := root.Execute()
	if err != nil {
		err = classifyCobraErr(err)
		printErr(&errb, err)
	}
	return out.String(), errb.String(), apperr.CodeOf(err)
}

func server(t *testing.T, sse bool) (*fakeMCP, map[string]string) {
	f := &fakeMCP{t: t, sse: sse, tools: map[string]map[string]any{
		"list_campaigns":       textResult(`[{"id":"c1","name":"Q3 Launch","status":"active","notes":"x"}]`),
		"apply_context_update": textResult(`{"ok":true}`),
		"get_context":          textResult("# Positioning\nplain markdown"),
	}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, map[string]string{config.EnvToken: "qvr_good", config.EnvAPIURL: srv.URL}
}

func TestCampaignList(t *testing.T) {
	for _, sse := range []bool{false, true} {
		t.Run(fmt.Sprintf("sse=%v", sse), func(t *testing.T) {
			f, env := server(t, sse)
			out, stderr, code := run(t, env, "campaign", "ls", "--status", "active")
			if code != 0 {
				t.Fatalf("code=%d stderr=%s", code, stderr)
			}
			if !strings.Contains(out, "Q3 Launch") || !strings.Contains(out, "STATUS") {
				t.Errorf("table output missing fields:\n%s", out)
			}
			if got := f.calls[0]["arguments"].(map[string]any)["status"]; got != "active" {
				t.Errorf("status arg = %v", got)
			}
		})
	}
}

func TestJSONOutput(t *testing.T) {
	_, env := server(t, false)
	out, _, code := run(t, env, "campaign", "ls", "--json")
	var rows []map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &rows) != nil || rows[0]["id"] != "c1" {
		t.Fatalf("code=%d out=%s", code, out)
	}
	// Plain-text results are wrapped so --json output is always JSON.
	out, _, _ = run(t, env, "context", "show", "--json")
	var wrapped map[string]string
	if json.Unmarshal([]byte(out), &wrapped) != nil || !strings.HasPrefix(wrapped["text"], "# Positioning") {
		t.Fatalf("out=%s", out)
	}
}

func TestExitCodes(t *testing.T) {
	_, env := server(t, false)
	bad := map[string]string{config.EnvToken: "qvr_bad", config.EnvAPIURL: env[config.EnvAPIURL]}

	cases := []struct {
		name string
		env  map[string]string
		args []string
		want int
	}{
		{"not found", env, []string{"campaign", "get", "nope"}, apperr.CodeNotFound},
		{"bad token", bad, []string{"dashboard"}, apperr.CodeAuth},
		{"no token", nil, []string{"dashboard"}, apperr.CodeAuth},
		{"no workspace", map[string]string{config.EnvToken: "qvr_good"}, []string{"dashboard"}, apperr.CodeUsage},
		{"missing arg", env, []string{"campaign", "get"}, apperr.CodeUsage},
		{"unknown flag", env, []string{"campaign", "ls", "--bogus"}, apperr.CodeUsage},
		{"unknown command", env, []string{"bogus"}, apperr.CodeUsage},
		{"bad workspace", map[string]string{config.EnvToken: "qvr_good", config.EnvWorkspace: "evil.com/x"}, []string{"dashboard"}, apperr.CodeUsage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, stderr, code := run(t, tc.env, tc.args...); code != tc.want {
				t.Errorf("code=%d want %d stderr=%s", code, tc.want, stderr)
			}
		})
	}
}

func TestJSONErrors(t *testing.T) {
	_, env := server(t, false)
	_, stderr, _ := run(t, env, "campaign", "get", "nope", "--json")
	var e struct {
		Error struct {
			Code int    `json:"code"`
			Kind string `json:"kind"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(stderr), &e) != nil || e.Error.Kind != "not_found" || e.Error.Code != 4 {
		t.Fatalf("stderr=%s", stderr)
	}
}

func TestApplyRequiresYes(t *testing.T) {
	f, env := server(t, false)
	_, stderr, code := run(t, env, "context", "apply", "p1")
	if code != apperr.CodeUsage || !strings.Contains(stderr, "--yes") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if len(f.calls) != 0 {
		t.Fatalf("apply ran without --yes: %v", f.calls)
	}
	if _, stderr, code := run(t, env, "context", "apply", "p1", "--yes"); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if len(f.calls) != 1 || f.calls[0]["name"] != "apply_context_update" {
		t.Fatalf("calls=%v", f.calls)
	}
}

func TestWorkspaceDerivesURL(t *testing.T) {
	out, _, code := run(t, map[string]string{config.EnvWorkspace: "tabstack"}, "config", "get", "api_url", "--json")
	if code != 0 || !strings.Contains(out, "https://tabstack.quivergtm.dev/api/mcp") || !strings.Contains(out, `"workspace"`) {
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestToolsCallArgs(t *testing.T) {
	f, env := server(t, false)
	_, stderr, code := run(t, env, "tools", "call", "list_campaigns", "-a", "limit=5", "-a", "mine=true", "-a", "q=hello", "--args", `{"status":"active"}`)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	got := f.calls[0]["arguments"].(map[string]any)
	if got["limit"] != float64(5) || got["mine"] != true || got["q"] != "hello" || got["status"] != "active" {
		t.Fatalf("args=%v", got)
	}
}

func TestLoginRejectsBadPrefix(t *testing.T) {
	if _, _, code := run(t, nil, "auth", "login", "--token", "sk_nope", "--no-verify"); code != apperr.CodeUsage {
		t.Fatalf("code=%d", code)
	}
}

func TestSpecArgMapping(t *testing.T) {
	f, env := server(t, false)
	f.tools["list_tasks"] = textResult(`[]`)
	f.tools["get_content"] = textResult(`{"slug":"launch-post"}`)

	if _, stderr, code := run(t, env, "task", "ls", "--mine", "--today", "--limit", "3"); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	got := f.calls[0]["arguments"].(map[string]any)
	want := map[string]any{"mine": true, "due_today": true, "limit": float64(3)}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("task ls args = %v, want %v (unset flags must not be sent)", got, want)
	}

	if _, stderr, code := run(t, env, "content", "get", "launch-post"); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if got := f.calls[1]["arguments"].(map[string]any); fmt.Sprint(got) != "map[slug:launch-post]" {
		t.Errorf("content get args = %v", got)
	}
}
