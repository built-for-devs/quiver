package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/built-for-devs/quiver/internal/apperr"
	"github.com/built-for-devs/quiver/internal/client"
	"github.com/built-for-devs/quiver/internal/output"
)

// newToolsCmd exposes the raw MCP tool surface. It is the escape hatch for
// tools without a dedicated command, and the way to check argument schemas.
func newToolsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "Inspect and call raw MCP tools",
	}
	cmd.AddCommand(newToolsListCmd(), newToolsDescribeCmd(), newToolsCallCmd())
	return cmd
}

func listTools(cmd *cobra.Command) ([]client.Tool, error) {
	s, err := loadSettings()
	if err != nil {
		return nil, err
	}
	c, err := newClient(s)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), g.timeout)
	defer cancel()
	return c.ListTools(ctx)
}

func newToolsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List tools the server exposes",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			tools, err := listTools(cmd)
			if err != nil {
				return err
			}
			if g.json {
				return output.JSON(cmd.OutOrStdout(), tools)
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			for _, t := range tools {
				desc, _, _ := strings.Cut(t.Description, "\n")
				fmt.Fprintf(tw, "%s\t%s\n", t.Name, desc)
			}
			return tw.Flush()
		},
	}
}

func newToolsDescribeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "describe <tool>",
		Short: "Show a tool's description and input schema",
		Args:  exactArgs(1, "<tool>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			tools, err := listTools(cmd)
			if err != nil {
				return err
			}
			for _, t := range tools {
				if t.Name != args[0] {
					continue
				}
				if g.json {
					return output.JSON(cmd.OutOrStdout(), t)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n\n%s\n\nInput schema:\n", t.Name, strings.TrimSpace(t.Description))
				if len(t.InputSchema) == 0 {
					_, err := fmt.Fprintln(cmd.OutOrStdout(), "  (none)")
					return err
				}
				return output.JSON(cmd.OutOrStdout(), t.InputSchema)
			}
			return apperr.NotFound("no tool named %q", args[0])
		},
	}
}

func newToolsCallCmd() *cobra.Command {
	var pairs []string
	var rawJSON string
	cmd := &cobra.Command{
		Use:   "call <tool>",
		Short: "Call any tool with raw arguments",
		Example: `  quiver tools call list_tasks -a mine=true -a limit=20
  quiver tools call get_campaign --args '{"campaign_id":"q3-launch"}'`,
		Args: exactArgs(1, "<tool>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			toolArgs := map[string]any{}
			if rawJSON != "" {
				if err := json.Unmarshal([]byte(rawJSON), &toolArgs); err != nil {
					return apperr.Usage("--args must be a JSON object: %v", err)
				}
			}
			kv, err := parseArgPairs(pairs)
			if err != nil {
				return err
			}
			for k, v := range kv {
				toolArgs[k] = v
			}
			return callTool(cmd, args[0], toolArgs)
		},
	}
	cmd.Flags().StringArrayVarP(&pairs, "arg", "a", nil, "argument as key=value (repeatable; JSON values decoded)")
	cmd.Flags().StringVar(&rawJSON, "args", "", "arguments as a JSON object")
	return cmd
}
