package cmd

import (
	"github.com/spf13/cobra"

	"github.com/built-for-devs/quiver/internal/apperr"
)

// Commands that map 1:1 to an MCP tool.
//
// Tool names come from the Quiver docs. Argument keys are NOT documented
// there. They follow the camelCase convention of Quiver's public content API
// (contentType, canonicalUrl) but are unverified: check them against the live
// schema with `quiver tools describe <tool>` and fix them here.

func newDashboardCmd() *cobra.Command {
	return toolSpec{
		use:   "dashboard",
		short: "Workspace summary",
		tool:  "get_dashboard_summary",
	}.command()
}

// campaignFields are the editable campaign fields shared by create and update.
var campaignFields = []flagSpec{
	str("description", "description", "campaign description"),
	str("priority", "priority", "priority"),
	str("owner", "owner", "owner"),
	str("start", "startDate", "start date (YYYY-MM-DD)"),
	str("end", "endDate", "end date (YYYY-MM-DD)"),
}

func newCampaignCmd() *cobra.Command {
	return group("campaign", "Campaigns", []string{"campaigns"}, []toolSpec{
		{use: "ls", aliases: []string{"list"}, short: "List campaigns", tool: "list_campaigns",
			flags: []flagSpec{str("status", "status", "filter by status (e.g. active)"), limitFlag}},
		{use: "get <id>", short: "Show a campaign", tool: "get_campaign", argKeys: []string{"campaignId"}},
		{use: "create", short: "Create a campaign", tool: "create_campaign",
			example: `  quiver campaign create --name "Q3 launch" --owner sam --start 2026-10-01`,
			flags:   append([]flagSpec{str("name", "name", "campaign name").req(), str("status", "status", "initial status")}, campaignFields...)},
		{use: "update <id>", short: "Update campaign fields", tool: "update_campaign", argKeys: []string{"campaignId"},
			flags: append([]flagSpec{str("name", "name", "campaign name")}, campaignFields...)},
		{use: "status <id> <state>", short: "Change campaign status", tool: "update_campaign_status", argKeys: []string{"campaignId", "status"}},
	})
}

func newArtifactCmd() *cobra.Command {
	return group("artifact", "Artifacts (Draft → Review → Approved → Live → Archived)", []string{"artifacts"}, []toolSpec{
		{use: "ls", aliases: []string{"list"}, short: "List artifacts", tool: "list_artifacts",
			flags: []flagSpec{
				str("campaign", "campaignId", "filter by campaign"),
				str("status", "status", "filter by status (draft, review, approved, live, archived)"),
				limitFlag,
			}},
		{use: "get <id>", short: "Show an artifact", tool: "get_artifact", argKeys: []string{"artifactId"}},
		{use: "save", short: "Save a new artifact", tool: "save_artifact",
			example: `  quiver artifact save --campaign q3-launch --title "Launch email" --type email -f email.md`,
			flags: []flagSpec{
				str("title", "title", "artifact title").req(),
				file("content", "file with the artifact body").req(),
				str("campaign", "campaignId", "campaign to attach to"),
				str("type", "type", "artifact type"),
			}},
		{use: "update <id>", short: "Update an artifact, or move it to another campaign", tool: "update_artifact", argKeys: []string{"artifactId"},
			flags: []flagSpec{
				str("title", "title", "artifact title"),
				file("content", "file with the new artifact body"),
				str("campaign", "campaignId", "move to this campaign"),
				str("type", "type", "artifact type"),
			}},
		{use: "status <id> <state>", short: "Change artifact status (draft, review, approved, live, archived)", tool: "update_artifact_status",
			argKeys: []string{"artifactId", "status"}},
	})
}

func newContentCmd() *cobra.Command {
	return group("content", "Content library", nil, []toolSpec{
		{use: "ls", aliases: []string{"list"}, short: "List content", tool: "list_content",
			flags: []flagSpec{
				str("status", "status", "filter by status"),
				str("type", "contentType", "filter by content type (e.g. changelog)"),
				str("campaign", "campaignId", "filter by campaign"),
				limitFlag,
			}},
		{use: "calendar", short: "Show the content calendar", tool: "get_content_calendar",
			flags: []flagSpec{
				str("from", "from", "start date (YYYY-MM-DD)"),
				str("to", "to", "end date (YYYY-MM-DD)"),
			}},
		{use: "get <slug>", short: "Show a content item", tool: "get_content", argKeys: []string{"slug"}},
		{use: "metrics <slug>", short: "Show metric snapshots for a content item", tool: "get_content_metrics", argKeys: []string{"slug"}},
		{use: "log-metrics <slug>", short: "Record a metric snapshot for a content item", tool: "log_content_metrics", argKeys: []string{"slug"},
			example: `  quiver content log-metrics launch-post -m views=1200 -m clicks=85`,
			flags: []flagSpec{
				withShort(mapf("metric", "metrics", "metric value, e.g. views=1200"), "m").req(),
				str("date", "date", "snapshot date (YYYY-MM-DD), default today"),
			}},
		{use: "distribute <slug>", short: "Record where a content item was distributed", tool: "add_distribution", argKeys: []string{"slug"},
			example: `  quiver content distribute launch-post --channel linkedin --url https://linkedin.com/posts/...`,
			flags: []flagSpec{
				str("channel", "channel", "distribution channel (e.g. linkedin, newsletter)").req(),
				str("url", "url", "URL of the distributed post"),
				str("date", "date", "distribution date (YYYY-MM-DD)"),
			}},
	}, newContentPullCmd(), newContentPushCmd(), newContentCheckCmd())
}

func newResearchCmd() *cobra.Command {
	return group("research", "Research entries and quotes", nil, []toolSpec{
		{use: "ls", aliases: []string{"list"}, short: "List research entries", tool: "list_research_entries",
			flags: []flagSpec{str("source", "source", "filter by source (e.g. call)"), limitFlag}},
		{use: "get <id>", short: "Show a research entry", tool: "get_research_entry", argKeys: []string{"entryId"}},
		{use: "add", short: "Save a research entry", tool: "save_research_entry",
			example: `  quiver research add --source call --title "Acme discovery call" -f notes.md`,
			flags: []flagSpec{
				str("source", "source", "source type (e.g. call, interview, survey)").req(),
				file("content", "file with the notes").req(),
				str("title", "title", "entry title"),
				str("url", "url", "source URL"),
			}},
		{use: "quotes", short: "List quotes", tool: "list_quotes",
			flags: []flagSpec{boolf("starred", "starred", "only starred quotes"), limitFlag}},
		{use: "linear <entry-id>", short: "Print a Linear issue payload for piping", tool: "get_linear_payload", argKeys: []string{"entryId"},
			long: "Print the Linear payload for a research entry. Use --json for a machine-readable payload."},
	})
}

func newPerfCmd() *cobra.Command {
	return group("perf", "Performance log and close-the-loop proposals", nil, []toolSpec{
		{use: "log", short: "Log a performance data point", tool: "log_performance",
			example: `  quiver perf log --campaign q3-launch --metric signups --value 42`,
			flags: []flagSpec{
				str("campaign", "campaignId", "campaign").req(),
				str("metric", "metric", "metric name").req(),
				num("value", "value", "numeric value").req(),
				str("date", "date", "date (YYYY-MM-DD), default today"),
				str("note", "note", "context for this data point"),
			}},
		{use: "ls", aliases: []string{"list"}, short: "Show the performance log", tool: "get_performance_log",
			flags: []flagSpec{str("campaign", "campaignId", "filter by campaign"), limitFlag}},
		{use: "queue", short: "Show the close-the-loop queue", tool: "get_close_the_loop_queue"},
		{use: "proposals", short: "List proposals", tool: "list_proposals",
			flags: []flagSpec{str("status", "status", "filter by status (e.g. pending)"), limitFlag}},
	}, newPerfProposalCmd())
}

// newPerfProposalCmd needs exactly one of --approve/--reject, which the spec
// table does not express.
func newPerfProposalCmd() *cobra.Command {
	var approve, reject bool
	var note string
	cmd := &cobra.Command{
		Use:     "proposal <id> --approve|--reject",
		Short:   "Approve or reject a proposal (action_proposal)",
		Example: `  quiver perf proposal prop_123 --approve --note "matches Q3 goals"`,
		Args:    exactArgs(1, "<id>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if approve == reject {
				return apperr.Usage("pass exactly one of --approve or --reject")
			}
			action := "approve"
			if reject {
				action = "reject"
			}
			toolArgs := map[string]any{"proposalId": args[0], "action": action}
			setIf(cmd, toolArgs, "note", "note", note)
			return callTool(cmd, "action_proposal", toolArgs)
		},
	}
	cmd.Flags().BoolVar(&approve, "approve", false, "approve the proposal")
	cmd.Flags().BoolVar(&reject, "reject", false, "reject the proposal")
	cmd.Flags().StringVar(&note, "note", "", "reason, recorded with the decision")
	return cmd
}

// taskFields are the editable task fields shared by add and update.
var taskFields = []flagSpec{
	str("due", "dueDate", "due date (YYYY-MM-DD)"),
	str("assignee", "assignee", "assignee"),
	str("campaign", "campaignId", "campaign"),
	str("notes", "notes", "notes"),
}

func newTaskCmd() *cobra.Command {
	return group("task", "Tasks (hosted workspaces only)", []string{"tasks"}, []toolSpec{
		{use: "ls", aliases: []string{"list"}, short: "List tasks", tool: "list_tasks",
			flags: []flagSpec{
				boolf("mine", "mine", "only tasks assigned to me"),
				boolf("overdue", "overdue", "only overdue tasks"),
				boolf("today", "dueToday", "only tasks due today"),
				str("campaign", "campaignId", "filter by campaign"),
				limitFlag,
			}},
		{use: "add", short: "Create a task", tool: "create_task",
			example: `  quiver task add --title "Review launch email" --due 2026-10-02 --campaign q3-launch`,
			flags:   append([]flagSpec{str("title", "title", "task title").req()}, taskFields...)},
		{use: "update <id>", short: "Update a task", tool: "update_task", argKeys: []string{"taskId"},
			flags: append([]flagSpec{str("title", "title", "task title"), str("status", "status", "task status")}, taskFields...)},
		{use: "done <id>", short: "Mark a task complete", tool: "complete_task", argKeys: []string{"taskId"}},
	})
}

func newSessionCmd() *cobra.Command {
	return group("session", "Agent sessions", []string{"sessions"}, []toolSpec{
		{use: "ls", aliases: []string{"list"}, short: "List sessions", tool: "list_sessions", flags: []flagSpec{limitFlag}},
		{use: "get <id>", short: "Show a session", tool: "get_session", argKeys: []string{"sessionId"}},
	})
}

func newCompetitorCmd() *cobra.Command {
	return group("competitor", "Competitors and competitive intel", []string{"competitors"}, []toolSpec{
		{use: "ls", aliases: []string{"list"}, short: "List competitors", tool: "list_competitors"},
		{use: "get <id>", short: "Show a competitor", tool: "get_competitor", argKeys: []string{"competitorId"}},
		{use: "intel", short: "Show competitive intel", tool: "get_competitive_intel"},
	})
}

func withShort(f flagSpec, short string) flagSpec { f.short = short; return f }
