package cmd

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/built-for-devs/quiver/internal/apperr"
)

// Commands that map 1:1 to an MCP tool. Argument keys and enums match the
// live tool schemas (inspect with `quiver tools describe <tool>`); the server
// rejects unknown keys, so keep them in sync.

var (
	campaignStatuses = []string{"planning", "active", "paused", "complete", "archived"}
	artifactStatuses = []string{"draft", "review", "approved", "live", "archived"}
	artifactTypes    = []string{"copywriting", "email_sequence", "cold_email", "social_content", "launch_strategy",
		"content_strategy", "positioning", "messaging", "ad_creative", "competitor_analysis", "seo", "cro",
		"ab_test", "landing_page", "one_pager", "pitch_deck", "other"}
	contentStatuses = []string{"draft", "review", "approved", "published", "archived"}
	channels        = []string{"website", "dev_to", "hashnode", "medium", "newsletter", "linkedin", "twitter", "youtube", "other"}
	priorities      = []string{"high", "medium", "low"}
	researchSources = []string{"call", "interview", "survey", "review", "forum", "support_ticket", "social", "common_room", "other"}
	researchStages  = []string{"prospect", "customer", "churned", "never_converted"}
	researchThemes  = []string{"pricing", "onboarding", "competitor_mention", "feature_gap", "messaging", "icp_fit", "other"}
	sessionModes    = []string{"strategy", "create", "feedback", "analyze", "optimize"}
	taskStatuses    = []string{"todo", "in_progress", "done", "canceled"}
)

// Shared --campaign / --artifact flags: accept an ID or a partial name.
var (
	campaignRef = ref("campaign", "campaign_id", "campaign_name", "campaign")
	artifactRef = ref("artifact", "artifact_id", "artifact_title", "artifact")
)

func newDashboardCmd() *cobra.Command {
	return toolSpec{verb: "dashboard", short: "Workspace summary", tool: "get_dashboard_summary"}.command()
}

// campaignFields are the editable campaign fields shared by create and update.
var campaignFields = []flagSpec{
	str("description", "description", "campaign description"),
	str("goal", "goal", "campaign goal"),
	strs("channel", "channels", "channel"),
	oneOf("priority", "priority", "priority", priorities...),
	str("start", "start_date", "start date (YYYY-MM-DD)"),
	str("end", "end_date", "end date (YYYY-MM-DD)"),
}

func newCampaignCmd() *cobra.Command {
	return group("campaign", "Campaigns", []string{"campaigns"}, []toolSpec{
		{verb: "ls", aliases: []string{"list"}, short: "List campaigns", tool: "list_campaigns",
			flags: []flagSpec{oneOf("status", "status", "filter by status", append(campaignStatuses, "all")...)}},
		{verb: "get", args: []argSpec{idOr("<id|name>", "campaign_id", "name")}, short: "Show a campaign", tool: "get_campaign"},
		{verb: "create", short: "Create a campaign", tool: "create_campaign",
			example: `  quiver campaign create --name "Q3 launch" --goal "500 signups" --channel linkedin,newsletter --start 2026-10-01`,
			flags:   append([]flagSpec{str("name", "name", "campaign name").req()}, campaignFields...)},
		{verb: "update", args: []argSpec{id("<id>", "campaign_id")}, short: "Update campaign fields", tool: "update_campaign",
			flags: append([]flagSpec{str("name", "name", "campaign name")}, campaignFields...)},
		{verb: "status", args: []argSpec{id("<id>", "campaign_id"), {name: "<state>", key: "status", enum: campaignStatuses}},
			short: "Change campaign status", tool: "update_campaign_status"},
		{verb: "delete", args: []argSpec{id("<id>", "campaign_id")}, short: "Permanently delete a campaign", tool: "delete_campaign",
			guard: "permanently delete campaign %s"},
	})
}

func newArtifactCmd() *cobra.Command {
	return group("artifact", "Artifacts (draft → review → approved → live → archived)", []string{"artifacts"}, []toolSpec{
		{verb: "ls", aliases: []string{"list"}, short: "List artifacts", tool: "list_artifacts",
			flags: []flagSpec{
				campaignRef,
				oneOf("type", "type", "filter by type", artifactTypes...),
				oneOf("status", "status", "filter by status", artifactStatuses...),
				limitFlag,
			}},
		{verb: "get", args: []argSpec{idOr("<id|title>", "artifact_id", "title")}, short: "Show an artifact", tool: "get_artifact"},
		{verb: "save", short: "Save a new artifact", tool: "save_artifact",
			example: `  quiver artifact save --title "Launch email" --type email_sequence --campaign "Q3 launch" -f email.md`,
			flags: []flagSpec{
				str("title", "title", "artifact title").req(),
				oneOf("type", "type", "artifact type", artifactTypes...).req(),
				file("content", "file with the artifact body").req(),
				campaignRef,
				strs("tag", "tags", "tag"),
				str("skill", "skill_used", "skill used to produce it"),
			}},
		{verb: "update", args: []argSpec{id("<id>", "artifact_id")}, short: "Update an artifact, or move it to another campaign", tool: "update_artifact",
			flags: []flagSpec{
				str("title", "title", "artifact title"),
				file("content", "file with the new artifact body"),
				strs("tag", "tags", "tag, replaces the list"),
				ref("campaign", "campaign_id", "campaign_name", "move to this campaign"),
			}},
		{verb: "status", args: []argSpec{id("<id>", "artifact_id"), {name: "<state>", key: "status", enum: artifactStatuses}},
			short: "Change artifact status", tool: "update_artifact_status"},
		{verb: "archive", args: []argSpec{id("<id>", "artifact_id")}, short: "Archive an artifact (undo with `artifact status <id> draft`)", tool: "archive_artifact"},
		{verb: "delete", args: []argSpec{id("<id>", "artifact_id")}, short: "Permanently delete an artifact", tool: "delete_artifact",
			guard: "permanently delete artifact %s"},
	})
}

// contentMetrics are the snapshot fields log_content_metrics accepts.
var contentMetrics = []flagSpec{
	num("pageviews", "pageviews", "pageviews"),
	num("unique-visitors", "unique_visitors", "unique visitors"),
	num("avg-time-on-page", "avg_time_on_page", "average time on page, seconds"),
	num("bounce-rate", "bounce_rate", "bounce rate, 0-100"),
	num("organic-clicks", "organic_clicks", "organic clicks"),
	num("impressions", "impressions", "search impressions"),
	num("avg-position", "avg_position", "average search position"),
	num("ctr", "ctr", "click-through rate, 0-100"),
	num("social-shares", "social_shares", "social shares"),
	num("backlinks", "backlinks", "backlinks"),
	num("comments", "comments", "comments"),
	num("signups", "signups", "attributed signups"),
	num("conversion-rate", "conversion_rate", "conversion rate, 0-100"),
	str("notes", "notes", "notes about this snapshot"),
}

// contentRef is the positional content identifier: a UUID or a slug.
var contentRef = idOr("<id|slug>", "content_id", "slug")

func newContentCmd() *cobra.Command {
	return group("content", "Content library", nil, []toolSpec{
		{verb: "ls", aliases: []string{"list"}, short: "List content", tool: "list_content",
			flags: []flagSpec{
				oneOf("status", "status", "filter by status", contentStatuses...),
				str("type", "content_type", "filter by content type (e.g. blog_post, changelog)"),
				campaignRef,
				limitFlag,
			}},
		{verb: "calendar", short: "Show the content calendar", tool: "get_content_calendar",
			flags: []flagSpec{str("from", "from", "start date (YYYY-MM-DD)"), str("to", "to", "end date (YYYY-MM-DD)")}},
		{verb: "get", args: []argSpec{contentRef}, short: "Show a content item", tool: "get_content"},
		{verb: "metrics", args: []argSpec{contentRef}, short: "Show metric snapshots for a content item", tool: "get_content_metrics",
			flags: []flagSpec{limitFlag}},
		{verb: "log-metrics", args: []argSpec{contentRef}, short: "Record a metric snapshot for a content item", tool: "log_content_metrics",
			example: `  quiver content log-metrics launch-post --pageviews 1200 --organic-clicks 85 --ctr 3.1`,
			flags:   contentMetrics},
		{verb: "distribute", args: []argSpec{contentRef}, short: "Record where a content item was distributed", tool: "add_distribution",
			example: `  quiver content distribute launch-post --channel linkedin --url https://linkedin.com/posts/...`,
			flags: []flagSpec{
				oneOf("channel", "channel", "distribution channel", channels...).req(),
				str("url", "url", "URL where the content went live").req(),
				str("status", "status", "distribution status"),
				str("notes", "notes", "notes"),
			}},
		{verb: "archive", args: []argSpec{contentRef}, short: "Archive a content piece", tool: "archive_content",
			long:  "Archive a content piece. If it is published, this takes it down from the public site.",
			guard: "archive content %s (a published piece is taken down)"},
		{verb: "delete", args: []argSpec{contentRef}, short: "Permanently delete a content piece", tool: "delete_content",
			guard: "permanently delete content %s"},
	}, newContentPullCmd(), newContentPushCmd(), newContentCheckCmd())
}

func newResearchCmd() *cobra.Command {
	return group("research", "Research entries and quotes", nil, []toolSpec{
		{verb: "ls", aliases: []string{"list"}, short: "List research entries", tool: "list_research_entries",
			flags: []flagSpec{
				oneOf("source", "source_type", "filter by source", researchSources...),
				str("segment", "segment", "filter by contact segment"),
				oneOf("stage", "stage", "filter by contact stage", researchStages...),
				oneOf("theme", "theme", "filter by theme", researchThemes...),
				campaignRef,
				boolf("product-signal", "product_signal", "only entries with product signals"),
				limitFlag,
			}},
		{verb: "get", args: []argSpec{idOr("<id|title>", "id", "title")}, short: "Show a research entry", tool: "get_research_entry"},
		{verb: "add", short: "Save a research entry", tool: "save_research_entry",
			example: `  quiver research add --source call --title "Acme discovery call" --company Acme -f notes.md`,
			flags: []flagSpec{
				str("title", "title", "entry title").req(),
				oneOf("source", "source_type", "source type", researchSources...).req(),
				file("raw_notes", "file with the raw notes").req(),
				str("contact", "contact_name", "contact name"),
				str("company", "contact_company", "contact company"),
				str("segment", "contact_segment", "contact segment"),
				oneOf("stage", "contact_stage", "contact stage", researchStages...),
				str("date", "research_date", "research date (YYYY-MM-DD)"),
				campaignRef,
				boolf("product-signal", "product_signal", "flag as a product signal"),
				str("product-note", "product_note", "product signal note"),
			}},
		{verb: "quotes", short: "List quotes", tool: "list_quotes",
			flags: []flagSpec{
				oneOf("theme", "theme", "filter by theme", researchThemes...),
				str("segment", "segment", "filter by segment"),
				boolf("featured", "featured_only", "only featured quotes"),
				limitFlag,
			}},
		{verb: "update", args: []argSpec{id("<id>", "id")}, short: "Update a research entry", tool: "update_research_entry",
			flags: []flagSpec{
				str("title", "title", "entry title"),
				oneOf("source", "source_type", "source type", researchSources...),
				file("raw_notes", "file with the replacement raw notes"),
				str("contact", "contact_name", "contact name"),
				str("company", "contact_company", "contact company"),
				str("segment", "contact_segment", "contact segment"),
				oneOf("stage", "contact_stage", "contact stage", researchStages...),
				str("sentiment", "sentiment", "sentiment"),
				str("date", "research_date", "research date (YYYY-MM-DD)"),
				boolf("product-signal", "product_signal", "flag as a product signal (--product-signal=false to unflag)"),
				str("product-note", "product_note", "product signal note"),
			}},
		{verb: "delete", args: []argSpec{id("<id>", "id")}, short: "Permanently delete a research entry and its quotes", tool: "delete_research_entry",
			guard: "permanently delete research entry %s"},
		{verb: "linear", args: []argSpec{idOr("<id|title>", "entry_id", "entry_title")}, short: "Print a Linear issue payload for piping", tool: "get_linear_payload",
			long: "Generate a Linear issue payload (title, description, entry URL) from a research entry with a product signal.\nDoes not call the Linear API."},
	}, group("quote", "Update or delete a quote (list them with `research quotes`)", nil, []toolSpec{
		{verb: "update", args: []argSpec{id("<quote-id>", "quote_id")}, short: "Feature or re-theme a quote", tool: "update_quote",
			example: `  quiver research quote update 5b1e... --featured
  quiver research quote update 5b1e... --featured=false --theme pricing`,
			flags: []flagSpec{
				boolf("featured", "featured", "feature the quote in AI session context (--featured=false to unfeature)"),
				oneOf("theme", "theme", "theme", researchThemes...),
			}},
		{verb: "delete", args: []argSpec{id("<quote-id>", "quote_id")}, short: "Permanently delete a quote", tool: "delete_quote",
			guard: "permanently delete quote %s"},
	}))
}

func newPerfCmd() *cobra.Command {
	return group("perf", "Performance log and close-the-loop proposals", nil, []toolSpec{
		{verb: "log", short: "Log performance results for a campaign or artifact", tool: "log_performance",
			long:    "Log performance results. The server synthesizes the results and may return proposed context updates;\nreview them with `quiver perf proposals`.",
			example: `  quiver perf log --campaign "Q3 launch" -m signups=42 -m opens=1200 --worked "subject line B"`,
			flags: []flagSpec{
				campaignRef,
				artifactRef,
				mapf("metric", "metrics", "metric value, e.g. signups=42").withShort("m"),
				str("notes", "qualitative_notes", "qualitative notes"),
				str("worked", "what_worked", "what worked"),
				str("didnt", "what_didnt", "what didn't work"),
				str("from", "period_start", "period start (YYYY-MM-DD)"),
				str("to", "period_end", "period end (YYYY-MM-DD)"),
			},
			prepare: func(cmd *cobra.Command, _ map[string]any) error {
				if !cmd.Flags().Changed("campaign") && !cmd.Flags().Changed("artifact") {
					return apperr.Usage("pass --campaign or --artifact")
				}
				return nil
			}},
		{verb: "ls", aliases: []string{"list"}, short: "Show the performance log", tool: "get_performance_log",
			flags: []flagSpec{campaignRef, str("artifact", "artifact_id", "filter by artifact ID"), limitFlag}},
		{verb: "queue", short: "Show the close-the-loop queue", tool: "get_close_the_loop_queue",
			flags: []flagSpec{boolf("overdue", "overdue_only", "only reminders already past due")}},
		{verb: "proposals", short: "List context update proposals", tool: "list_proposals",
			flags: []flagSpec{oneOf("status", "status", "filter by status", "pending", "approved", "rejected", "all")}},
		{verb: "proposal", args: []argSpec{id("<log-id>", "log_id")}, short: "Approve or reject a proposal", tool: "action_proposal",
			long: `Approve or reject a pending context update proposal.

Approving applies the change to the context immediately, so --approve
prompts on a terminal and requires --yes in scripts.`,
			example: `  quiver perf proposal 3f2c... --reject
  quiver perf proposal 3f2c... --approve --yes`,
			flags: []flagSpec{
				boolf("approve", "", "approve and apply the proposal"),
				boolf("reject", "", "reject the proposal"),
				boolf("yes", "", "approve without prompting (required in scripts)").withShort("y"),
			},
			prepare: func(cmd *cobra.Command, args map[string]any) error {
				approve, _ := cmd.Flags().GetBool("approve")
				reject, _ := cmd.Flags().GetBool("reject")
				if approve == reject {
					return apperr.Usage("pass exactly one of --approve or --reject")
				}
				if reject {
					args["action"] = "rejected"
					return nil
				}
				yes, _ := cmd.Flags().GetBool("yes")
				if err := confirm(cmd, yes, "approve proposal "+args["log_id"].(string)+" and apply it to the context"); err != nil {
					return err
				}
				args["action"] = "approved"
				return nil
			}},
	})
}

// taskLinks are the optional links a task can have to other records.
var taskLinks = []flagSpec{
	str("content", "content_piece_id", "linked content piece ID"),
	str("research", "research_entry_id", "linked research entry ID"),
}

func newTaskCmd() *cobra.Command {
	return group("task", "Tasks (hosted workspaces only)", []string{"tasks"}, []toolSpec{
		{verb: "ls", aliases: []string{"list"}, short: "List tasks (open tasks by default)", tool: "list_tasks",
			flags: append([]flagSpec{
				oneOf("status", "status", "filter by status", taskStatuses...),
				str("assignee", "assignee_id", "filter by assignee user ID"),
				boolf("all", "include_closed", "include done and canceled tasks"),
				str("due-before", "due_before", "due on or before YYYY-MM-DD"),
				boolf("today", "", "due today or earlier"),
				boolf("overdue", "", "due before today"),
				campaignRef,
				artifactRef,
				str("session", "session_id", "linked session ID"),
			}, taskLinks...),
			prepare: func(cmd *cobra.Command, args map[string]any) error {
				today, _ := cmd.Flags().GetBool("today")
				overdue, _ := cmd.Flags().GetBool("overdue")
				n := 0
				for _, f := range []string{"today", "overdue", "due-before"} {
					if cmd.Flags().Changed(f) {
						n++
					}
				}
				if n > 1 {
					return apperr.Usage("use only one of --today, --overdue, --due-before")
				}
				switch {
				case today:
					args["due_before"] = time.Now().Format(time.DateOnly)
				case overdue:
					args["due_before"] = time.Now().AddDate(0, 0, -1).Format(time.DateOnly)
				}
				return nil
			}},
		{verb: "add", short: "Create a task", tool: "create_task",
			example: `  quiver task add --title "Review launch email" --due 2026-10-02 --priority high --campaign "Q3 launch"`,
			flags: append([]flagSpec{
				str("title", "title", "task title").req(),
				str("description", "description", "description"),
				oneOf("priority", "priority", "priority", priorities...),
				str("due", "due_date", "due date (YYYY-MM-DD)"),
				str("assignee", "assignee_id", "assignee user ID"),
				campaignRef,
				artifactRef,
				str("session", "session_id", "session this task came from"),
			}, taskLinks...)},
		{verb: "update", args: []argSpec{idOr("<id|title>", "task_id", "task_title")}, short: "Update a task (only passed fields change)", tool: "update_task",
			flags: append([]flagSpec{
				str("title", "title", "new title"),
				str("description", "description", "new description"),
				nullf("clear-description", "description", "remove the description"),
				oneOf("status", "status", "status", taskStatuses...),
				oneOf("priority", "priority", "priority", priorities...),
				str("due", "due_date", "due date (YYYY-MM-DD)"),
				nullf("clear-due", "due_date", "remove the due date"),
				str("assignee", "assignee_id", "assignee user ID"),
				nullf("unassign", "assignee_id", "remove the assignee"),
				str("campaign", "campaign_id", "link to campaign ID"),
				str("artifact", "artifact_id", "link to artifact ID"),
			}, taskLinks...)},
		{verb: "done", args: []argSpec{idOr("<id|title>", "task_id", "task_title")}, short: "Mark a task complete", tool: "complete_task"},
	})
}

func newSessionCmd() *cobra.Command {
	return group("session", "Agent sessions", []string{"sessions"}, []toolSpec{
		{verb: "ls", aliases: []string{"list"}, short: "List sessions", tool: "list_sessions",
			flags: []flagSpec{
				oneOf("mode", "mode", "filter by mode", sessionModes...),
				str("campaign", "campaign_id", "filter by campaign ID"),
				limitFlag,
			}},
		{verb: "get", args: []argSpec{id("<id>", "session_id")}, short: "Show a session", tool: "get_session"},
		{verb: "delete", args: []argSpec{id("<id>", "session_id")}, short: "Permanently delete a session", tool: "delete_session",
			guard: "permanently delete session %s"},
	})
}

var competitorSurfaces = []string{"homepage", "pricing", "product", "changelog", "blog", "reviews", "careers",
	"profile", "docs", "github", "social"}

// competitorPages is the --page flag: surface=url pairs to track.
var competitorPages = pairs("page", "pages", "page to track", [2]string{"surface", "url"}, competitorSurfaces...)

func newCompetitorCmd() *cobra.Command {
	return group("competitor", "Competitors and competitive intel", []string{"competitors"}, []toolSpec{
		{verb: "ls", aliases: []string{"list"}, short: "List competitors", tool: "list_competitors"},
		{verb: "get", args: []argSpec{id("<id>", "competitor_id")}, short: "Show a competitor", tool: "get_competitor"},
		{verb: "intel", short: "Show competitive intel", tool: "get_competitive_intel"},
		{verb: "add", short: "Track a competitor", tool: "add_competitor",
			example: `  quiver competitor add --name Acme --homepage https://acme.dev --page pricing=https://acme.dev/pricing`,
			flags: []flagSpec{
				str("name", "name", "competitor name").req(),
				str("homepage", "homepage_url", "public https homepage URL").req(),
				competitorPages,
			}},
		{verb: "update", args: []argSpec{id("<id>", "competitor_id")}, short: "Update a competitor", tool: "update_competitor",
			flags: []flagSpec{
				str("name", "name", "new name"),
				str("homepage", "homepage_url", "corrected homepage URL"),
				boolf("active", "is_active", "resume scanning (--active=false pauses it)"),
				competitorPages,
			}},
		{verb: "remove", args: []argSpec{id("<id>", "competitor_id")}, short: "Stop tracking a competitor", tool: "remove_competitor",
			guard: "remove competitor %s"},
		{verb: "scan", short: "Scan competitors that are due (spends model tokens)", tool: "run_competitor_scan",
			flags: []flagSpec{str("competitor", "competitor_id", "scan only this competitor ID")},
			guard: "run a competitor scan, which spends model tokens"},
		{verb: "cadence", args: []argSpec{{name: "<off|monthly|weekly>", key: "cadence", enum: []string{"off", "monthly", "weekly"}}},
			short: "Set how often competitors are scanned automatically", tool: "set_competitive_intel",
			long:  "Set the automatic scan cadence. Enabling scans commits model tokens on a schedule, so\nanything but off prompts on a terminal and requires --yes in scripts.",
			flags: []flagSpec{boolf("yes", "", "skip the confirmation prompt (required in scripts)").withShort("y")},
			prepare: func(cmd *cobra.Command, args map[string]any) error {
				cadence := args["cadence"].(string)
				if cadence == "off" {
					return nil
				}
				yes, _ := cmd.Flags().GetBool("yes")
				return confirm(cmd, yes, "scan competitors "+cadence+", which spends model tokens on a schedule")
			}},
	})
}
