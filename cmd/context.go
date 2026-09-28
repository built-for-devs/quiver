package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/built-for-devs/quiver/internal/apperr"
)

func newContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Workspace context: show, history, propose, apply, restore",
		Long: `Workspace context.

Changes follow the same rule as the MCP server: propose is the default,
immediate mutation is opt-in. 'apply' and 'restore' require --yes when not
running in an interactive terminal.`,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Show current context (get_context)",
			Args:  noArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return callTool(cmd, "get_context", nil)
			},
		},
		newContextHistoryCmd(),
		newContextProposeCmd(),
		newContextApplyCmd(),
		newContextRestoreCmd(),
	)
	return cmd
}

func newContextHistoryCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "history",
		Short: "List context versions (get_context_history)",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			args := map[string]any{}
			setIf(cmd, args, "limit", "limit", limit)
			return callTool(cmd, "get_context_history", args)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "max versions to return")
	return cmd
}

func newContextProposeCmd() *cobra.Command {
	var file, summary string
	cmd := &cobra.Command{
		Use:   "propose -f <file>",
		Short: "Propose a context update for review (propose_context_update)",
		Example: `  quiver context propose -f notes.md -m "Q3 positioning"
  cat notes.md | quiver context propose -f -`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if file == "" {
				return apperr.Usage("-f/--file is required (use - for stdin)")
			}
			content, err := readInput(cmd, file)
			if err != nil {
				return err
			}
			if strings.TrimSpace(content) == "" {
				return apperr.Validation("proposal content is empty")
			}
			args := map[string]any{"content": content}
			setIf(cmd, args, "message", "summary", summary)
			return callTool(cmd, "propose_context_update", args)
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "markdown file with the proposed context, or - for stdin")
	cmd.Flags().StringVarP(&summary, "message", "m", "", "short summary of the change")
	return cmd
}

func newContextApplyCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "apply <proposal-id>",
		Short: "Apply a proposal immediately (apply_context_update, guarded)",
		Args:  exactArgs(1, "<proposal-id>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirm(cmd, yes, "apply context proposal "+args[0]); err != nil {
				return err
			}
			return callTool(cmd, "apply_context_update", map[string]any{"proposal_id": args[0]})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "apply without prompting (required in scripts)")
	return cmd
}

func newContextRestoreCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "restore <version>",
		Short: "Restore a previous context version (restore_context_version, guarded)",
		Args:  exactArgs(1, "<version>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirm(cmd, yes, "restore context to version "+args[0]); err != nil {
				return err
			}
			return callTool(cmd, "restore_context_version", map[string]any{"version": args[0]})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "restore without prompting (required in scripts)")
	return cmd
}
