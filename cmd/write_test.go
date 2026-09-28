package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/built-for-devs/quiver/internal/apperr"
)

func TestWriteCommandArgs(t *testing.T) {
	notes := filepath.Join(t.TempDir(), "notes.md")
	os.WriteFile(notes, []byte("call notes"), 0o644)

	cases := []struct {
		args []string
		tool string
		want string
	}{
		{[]string{"campaign", "status", "c1", "active"}, "update_campaign_status", "map[campaign_id:c1 status:active]"},
		{[]string{"campaign", "create", "--name", "Q3", "--start", "2026-10-01", "--channel", "linkedin,newsletter"}, "create_campaign",
			"map[channels:[linkedin newsletter] name:Q3 start_date:2026-10-01]"},
		{[]string{"campaign", "get", "Q3 launch"}, "get_campaign", "map[name:Q3 launch]"},
		{[]string{"campaign", "get", "d3191e8e-d312-42a2-acc4-5f66f439361f"}, "get_campaign", "map[campaign_id:d3191e8e-d312-42a2-acc4-5f66f439361f]"},
		{[]string{"perf", "log", "--campaign", "Q3", "-m", "signups=42", "-m", "source=organic"}, "log_performance",
			"map[campaign_name:Q3 metrics:map[signups:42 source:organic]]"},
		{[]string{"content", "log-metrics", "launch-post", "--pageviews", "1200", "--ctr", "3.1"}, "log_content_metrics", "map[ctr:3.1 pageviews:1200 slug:launch-post]"},
		{[]string{"research", "add", "--title", "Acme", "--source", "call", "-f", notes}, "save_research_entry", "map[raw_notes:call notes source_type:call title:Acme]"},
		{[]string{"task", "done", "Review email"}, "complete_task", "map[task_title:Review email]"},
		{[]string{"task", "update", "Review email", "--clear-due", "--status", "in_progress"}, "update_task", "map[due_date:<nil> status:in_progress task_title:Review email]"},
		{[]string{"perf", "proposal", "log1", "--reject"}, "action_proposal", "map[action:rejected log_id:log1]"},
		{[]string{"perf", "proposal", "log1", "--approve", "--yes"}, "action_proposal", "map[action:approved log_id:log1]"},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			f, env := server(t, false)
			f.tools[tc.tool] = textResult(`{"ok":true}`)
			if _, stderr, code := run(t, env, tc.args...); code != 0 {
				t.Fatalf("code=%d stderr=%s", code, stderr)
			}
			if got := fmt.Sprint(f.calls[0]["arguments"]); f.calls[0]["name"] != tc.tool || got != tc.want {
				t.Errorf("%s %s, want %s %s", f.calls[0]["name"], got, tc.tool, tc.want)
			}
		})
	}
}

func TestWriteCommandUsageErrors(t *testing.T) {
	cases := [][]string{
		{"campaign", "create"},                                                        // missing required --name
		{"campaign", "status", "c1", "done"},                                          // not a campaign status
		{"campaign", "ls", "--status", "bogus"},                                       // bad enum flag
		{"content", "log-metrics", "post", "--pageviews", "lots"},                     // non-numeric
		{"perf", "log", "-m", "signups=1"},                                            // neither --campaign nor --artifact
		{"perf", "log", "--campaign", "q", "-m", "novalue"},                           // bad key=value
		{"perf", "proposal", "p1"},                                                    // neither approve nor reject
		{"perf", "proposal", "p1", "--approve", "--reject"},                           // both
		{"perf", "proposal", "p1", "--approve"},                                       // approve needs --yes when non-interactive
		{"campaign", "status", "c1"},                                                  // missing <state>
		{"task", "update", "t1", "--due", "2026-10-01", "--clear-due"},                // conflicting flags
		{"task", "ls", "--today", "--overdue"},                                        // conflicting date filters
		{"research", "add", "--title", "t", "--source", "call", "-f", "/nonexistent"}, // unreadable file
		{"context", "propose", "notAField", "--value", "x", "-r", "why"},              // unknown context field
		{"context", "propose", "brandVoice", "--value", "x"},                          // missing rationale
		{"context", "apply", "brandVoice", "-m", "x", "--yes"},                        // missing value
	}
	for _, args := range cases {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			f, env := server(t, false)
			if _, stderr, code := run(t, env, args...); code != apperr.CodeUsage {
				t.Errorf("code=%d want %d stderr=%s", code, apperr.CodeUsage, stderr)
			}
			if len(f.calls) != 0 {
				t.Errorf("tool called despite usage error: %v", f.calls)
			}
		})
	}
}

func TestContextPropose(t *testing.T) {
	f, env := server(t, false)
	f.tools["propose_context_update"] = textResult(`{"ok":true}`)
	dir := t.TempDir()
	list := filepath.Join(dir, "words.md") // extension doesn't matter: field is a list
	os.WriteFile(list, []byte("- fast\n- reliable\n"), 0o644)

	if _, stderr, code := run(t, env, "context", "propose", "wordsToUse", "-f", list, "-r", "call feedback", "--source", "Acme call"); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	got := fmt.Sprint(f.calls[1]["arguments"])
	want := `map[proposals:[map[current:["fast"] field:wordsToUse proposed:[fast reliable] rationale:call feedback]] source_note:Acme call]`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}

	// Text fields are sent verbatim, minus trailing newlines.
	f.calls = nil
	text := filepath.Join(dir, "pos.md")
	os.WriteFile(text, []byte("For teams: X.\n\n- not a list\n"), 0o644)
	run(t, env, "context", "propose", "positioningStatement", "-f", text, "-r", "why")
	p := f.calls[1]["arguments"].(map[string]any)["proposals"].([]any)[0].(map[string]any)
	if p["proposed"] != "For teams: X.\n\n- not a list" || p["current"] != "For teams..." {
		t.Errorf("proposal=%v", p)
	}

	// A list field rejects a non-list value.
	os.WriteFile(list, []byte("just text"), 0o644)
	if _, _, code := run(t, env, "context", "propose", "wordsToUse", "-f", list, "-r", "why"); code != apperr.CodeValidation {
		t.Errorf("non-list for list field: code=%d", code)
	}
}

func TestContextShowField(t *testing.T) {
	_, env := server(t, false)
	out, _, code := run(t, env, "context", "show", "--field", "wordsToUse")
	if code != 0 || out != "- fast\n" {
		t.Errorf("list field: code=%d out=%q", code, out)
	}
	out, _, _ = run(t, env, "context", "show", "--field", "positioningStatement")
	if out != "For teams...\n" {
		t.Errorf("text field: out=%q", out)
	}
}

func TestContextRestoreByNumber(t *testing.T) {
	f, env := server(t, false)
	f.tools["get_context_history"] = textResult(`[{"id":"v2-id","version":2},{"id":"v1-id","version":1}]`)
	f.tools["restore_context_version"] = textResult(`{"ok":true}`)
	if _, stderr, code := run(t, env, "context", "restore", "1", "--yes"); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if got := fmt.Sprint(f.calls[1]["arguments"]); got != "map[version_id:v1-id]" {
		t.Errorf("args=%s", got)
	}
	if _, _, code := run(t, env, "context", "restore", "9", "--yes"); code != apperr.CodeNotFound {
		t.Errorf("missing version: code=%d", code)
	}
}

func TestGuardedCommands(t *testing.T) {
	cases := []struct {
		args []string
		tool string
	}{
		{[]string{"campaign", "delete", "c1"}, "delete_campaign"},
		{[]string{"artifact", "delete", "a1"}, "delete_artifact"},
		{[]string{"content", "archive", "launch-post"}, "archive_content"},
		{[]string{"content", "delete", "launch-post"}, "delete_content"},
		{[]string{"research", "delete", "r1"}, "delete_research_entry"},
		{[]string{"research", "quote", "delete", "q1"}, "delete_quote"},
		{[]string{"session", "delete", "s1"}, "delete_session"},
		{[]string{"competitor", "remove", "k1"}, "remove_competitor"},
		{[]string{"competitor", "scan"}, "run_competitor_scan"},
		{[]string{"competitor", "cadence", "weekly"}, "set_competitive_intel"},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			f, env := server(t, false)
			f.tools[tc.tool] = textResult(`{"ok":true}`)
			_, stderr, code := run(t, env, tc.args...)
			if code != apperr.CodeUsage || !strings.Contains(stderr, "--yes") || len(f.calls) != 0 {
				t.Fatalf("without --yes: code=%d calls=%v stderr=%s", code, f.calls, stderr)
			}
			if _, stderr, code := run(t, env, append(tc.args, "--yes")...); code != 0 || f.calls[0]["name"] != tc.tool {
				t.Fatalf("with --yes: code=%d calls=%v stderr=%s", code, f.calls, stderr)
			}
		})
	}

	// Unguarded: reversible or harmless.
	for _, args := range [][]string{{"competitor", "cadence", "off"}, {"artifact", "archive", "a1"}} {
		f, env := server(t, false)
		f.tools["set_competitive_intel"] = textResult(`{"ok":true}`)
		f.tools["archive_artifact"] = textResult(`{"ok":true}`)
		if _, stderr, code := run(t, env, args...); code != 0 {
			t.Errorf("%v: code=%d stderr=%s", args, code, stderr)
		}
	}
}

func TestCompetitorPages(t *testing.T) {
	f, env := server(t, false)
	f.tools["add_competitor"] = textResult(`{"ok":true}`)
	_, stderr, code := run(t, env, "competitor", "add", "--name", "Acme", "--homepage", "https://acme.dev",
		"--page", "pricing=https://acme.dev/pricing?plan=a", "--page", "docs=https://acme.dev/docs")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	want := "map[homepage_url:https://acme.dev name:Acme pages:[map[surface:pricing url:https://acme.dev/pricing?plan=a] map[surface:docs url:https://acme.dev/docs]]]"
	if got := fmt.Sprint(f.calls[0]["arguments"]); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if _, _, code := run(t, env, "competitor", "add", "--name", "A", "--homepage", "https://a.dev", "--page", "website=https://a.dev"); code != apperr.CodeUsage {
		t.Errorf("bad surface: code=%d", code)
	}
}
