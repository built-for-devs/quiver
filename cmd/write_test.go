package cmd

import (
	"fmt"
	"os"
	"path/filepath"
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
		{[]string{"campaign", "status", "c1", "active"}, "update_campaign_status", "map[campaignId:c1 status:active]"},
		{[]string{"campaign", "create", "--name", "Q3", "--start", "2026-10-01"}, "create_campaign", "map[name:Q3 startDate:2026-10-01]"},
		{[]string{"perf", "log", "--campaign", "q3", "--metric", "signups", "--value", "42.5"}, "log_performance", "map[campaignId:q3 metric:signups value:42.5]"},
		{[]string{"content", "log-metrics", "post", "-m", "views=1200", "-m", "source=organic"}, "log_content_metrics", "map[metrics:map[source:organic views:1200] slug:post]"},
		{[]string{"research", "add", "--source", "call", "-f", notes}, "save_research_entry", "map[content:call notes source:call]"},
		{[]string{"task", "done", "t1"}, "complete_task", "map[taskId:t1]"},
		{[]string{"perf", "proposal", "p1", "--reject", "--note", "no"}, "action_proposal", "map[action:reject note:no proposalId:p1]"},
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
		{"campaign", "create"}, // missing required --name
		{"perf", "log", "--campaign", "q", "--metric", "m", "--value", "lots"}, // non-numeric
		{"perf", "proposal", "p1"},                                    // neither approve nor reject
		{"perf", "proposal", "p1", "--approve", "--reject"},           // both
		{"campaign", "status", "c1"},                                  // missing <state>
		{"content", "log-metrics", "post", "-m", "novalue"},           // bad key=value
		{"research", "add", "--source", "call", "-f", "/nonexistent"}, // unreadable file
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
