package cmd

import "github.com/spf13/cobra"

// Read-only commands. Each maps to exactly one MCP tool.
//
// Tool names come from the Quiver docs. Argument keys (campaign_id, slug,
// status, ...) are NOT documented there: verify against the live schema with
// `quiver tools describe <tool>` and fix them here.

func newDashboardCmd() *cobra.Command {
	return toolSpec{
		use:   "dashboard",
		short: "Workspace summary",
		tool:  "get_dashboard_summary",
	}.command()
}

func newCampaignCmd() *cobra.Command {
	return group("campaign", "Campaigns", []string{"campaigns"}, []toolSpec{
		{use: "ls", aliases: []string{"list"}, short: "List campaigns", tool: "list_campaigns",
			flags: []flagSpec{str("status", "status", "filter by status (e.g. active)"), limitFlag}},
		{use: "get <id>", short: "Show a campaign", tool: "get_campaign", argKey: "campaign_id"},
	})
}

func newArtifactCmd() *cobra.Command {
	return group("artifact", "Artifacts (Draft → Review → Approved → Live → Archived)", []string{"artifacts"}, []toolSpec{
		{use: "ls", aliases: []string{"list"}, short: "List artifacts", tool: "list_artifacts",
			flags: []flagSpec{
				str("campaign", "campaign_id", "filter by campaign"),
				str("status", "status", "filter by status (draft, review, approved, live, archived)"),
				limitFlag,
			}},
		{use: "get <id>", short: "Show an artifact", tool: "get_artifact", argKey: "artifact_id"},
	})
}

func newContentCmd() *cobra.Command {
	return group("content", "Content library", nil, []toolSpec{
		{use: "ls", aliases: []string{"list"}, short: "List content", tool: "list_content",
			flags: []flagSpec{
				str("status", "status", "filter by status"),
				str("type", "contentType", "filter by content type (e.g. changelog)"),
				str("campaign", "campaign_id", "filter by campaign"),
				limitFlag,
			}},
		{use: "calendar", short: "Show the content calendar", tool: "get_content_calendar",
			flags: []flagSpec{
				str("from", "from", "start date (YYYY-MM-DD)"),
				str("to", "to", "end date (YYYY-MM-DD)"),
			}},
		{use: "get <slug>", short: "Show a content item", tool: "get_content", argKey: "slug"},
		{use: "metrics <slug>", short: "Show metric snapshots for a content item", tool: "get_content_metrics", argKey: "slug"},
	})
}

func newResearchCmd() *cobra.Command {
	return group("research", "Research entries and quotes", nil, []toolSpec{
		{use: "ls", aliases: []string{"list"}, short: "List research entries", tool: "list_research_entries",
			flags: []flagSpec{str("source", "source", "filter by source (e.g. call)"), limitFlag}},
		{use: "get <id>", short: "Show a research entry", tool: "get_research_entry", argKey: "entry_id"},
		{use: "quotes", short: "List quotes", tool: "list_quotes",
			flags: []flagSpec{boolf("starred", "starred", "only starred quotes"), limitFlag}},
		{use: "linear <entry-id>", short: "Print a Linear issue payload for piping", tool: "get_linear_payload", argKey: "entry_id",
			long: "Print the Linear payload for a research entry. Use --json for a machine-readable payload."},
	})
}

func newPerfCmd() *cobra.Command {
	return group("perf", "Performance log and close-the-loop proposals", nil, []toolSpec{
		{use: "ls", aliases: []string{"list"}, short: "Show the performance log", tool: "get_performance_log",
			flags: []flagSpec{str("campaign", "campaign_id", "filter by campaign"), limitFlag}},
		{use: "queue", short: "Show the close-the-loop queue", tool: "get_close_the_loop_queue"},
		{use: "proposals", short: "List proposals", tool: "list_proposals",
			flags: []flagSpec{str("status", "status", "filter by status (e.g. pending)"), limitFlag}},
	})
}

func newTaskCmd() *cobra.Command {
	return group("task", "Tasks (hosted workspaces only)", []string{"tasks"}, []toolSpec{
		{use: "ls", aliases: []string{"list"}, short: "List tasks", tool: "list_tasks",
			flags: []flagSpec{
				boolf("mine", "mine", "only tasks assigned to me"),
				boolf("overdue", "overdue", "only overdue tasks"),
				boolf("today", "due_today", "only tasks due today"),
				str("campaign", "campaign_id", "filter by campaign"),
				limitFlag,
			}},
	})
}

func newSessionCmd() *cobra.Command {
	return group("session", "Agent sessions", []string{"sessions"}, []toolSpec{
		{use: "ls", aliases: []string{"list"}, short: "List sessions", tool: "list_sessions", flags: []flagSpec{limitFlag}},
		{use: "get <id>", short: "Show a session", tool: "get_session", argKey: "session_id"},
	})
}

func newCompetitorCmd() *cobra.Command {
	return group("competitor", "Competitors and competitive intel", []string{"competitors"}, []toolSpec{
		{use: "ls", aliases: []string{"list"}, short: "List competitors", tool: "list_competitors"},
		{use: "get <id>", short: "Show a competitor", tool: "get_competitor", argKey: "competitor_id"},
		{use: "intel", short: "Show competitive intel", tool: "get_competitive_intel"},
	})
}
