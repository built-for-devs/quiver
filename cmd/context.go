package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	"github.com/built-for-devs/quiver/internal/apperr"
	"github.com/built-for-devs/quiver/internal/output"
)

// contextFields are the editable product marketing context fields, as
// accepted by propose_context_update and apply_context_update.
var contextFields = []string{
	"positioningStatement", "productCategory", "businessModel", "icpDefinition", "coreProblem",
	"messagingPillars", "keyDifferentiators", "competitiveLandscape", "whyAlternativesFallShort",
	"customerLanguage", "proofPoints", "activeHypotheses", "brandVoice", "wordsToUse", "wordsToAvoid",
}

func newContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Product marketing context: show, history, propose, apply, restore",
		Long: `Product marketing context: positioning, ICP, messaging pillars, brand voice, etc.

Changes are made one field at a time and replace the whole field. Edit a
field by exporting it, changing it, and proposing the complete new value:

  quiver context show --field messagingPillars > pillars.yaml
  $EDITOR pillars.yaml
  quiver context propose messagingPillars -f pillars.yaml -r "Acme call feedback"

propose is the default and goes to human review in the Quiver UI. apply
changes the context immediately, so it prompts on a terminal and requires
--yes in scripts.`,
	}
	cmd.AddCommand(
		newContextShowCmd(),
		toolSpec{verb: "history", short: "List context versions", tool: "get_context_history",
			flags: []flagSpec{limitFlag}}.command(),
		newContextProposeCmd(),
		newContextApplyCmd(),
		newContextRestoreCmd(),
	)
	return cmd
}

func newContextShowCmd() *cobra.Command {
	var field string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the active context (get_context)",
		Long: `Show the active context.

With --field, print one field's value in an editable form: text fields as
raw text, lists and objects as YAML. Use --json for the raw JSON value.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if field == "" {
				return callTool(cmd, "get_context", nil)
			}
			if err := checkContextField(field); err != nil {
				return err
			}
			s, err := newSession(cmd)
			if err != nil {
				return err
			}
			ctx, err := fetchContext(s)
			if err != nil {
				return err
			}
			v := ctx[field]
			w := cmd.OutOrStdout()
			if g.json {
				return output.JSON(w, v)
			}
			switch t := v.(type) {
			case nil:
				return nil
			case string:
				_, err := fmt.Fprintln(w, strings.TrimRight(t, "\n"))
				return err
			default:
				b, err := yaml.Marshal(t)
				if err != nil {
					return err
				}
				_, err = w.Write(b)
				return err
			}
		},
	}
	cmd.Flags().StringVar(&field, "field", "", "print a single field ("+strings.Join(contextFields, ", ")+")")
	cmd.RegisterFlagCompletionFunc("field", completeContextFields)
	return cmd
}

func newContextProposeCmd() *cobra.Command {
	var file, value, rationale, sourceNote string
	cmd := &cobra.Command{
		Use:   "propose <field>",
		Short: "Propose a new value for a context field, for human review (propose_context_update)",
		Long: `Propose a complete new value for one context field. The proposal is reviewed
in the Quiver UI; approving it REPLACES the field with this value, so pass
the full final contents, not a fragment.

Text fields take the file contents as-is. List and object fields
(e.g. messagingPillars, wordsToUse) take YAML or JSON, as printed by
` + "`quiver context show --field <field>`" + `.`,
		Example: `  quiver context propose positioningStatement -f positioning.md -r "Tighter ICP after Q3 calls"
  quiver context propose wordsToAvoid -f words.yaml -r "Legal review" --source "legal 2026-09-20"`,
		Args:              exactArgs(1, "<field>"),
		ValidArgsFunction: completeContextFieldArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			field := args[0]
			if err := checkContextField(field); err != nil {
				return err
			}
			if strings.TrimSpace(rationale) == "" {
				return apperr.Usage("--rationale is required: tell the reviewer why")
			}
			s, err := newSession(cmd)
			if err != nil {
				return err
			}
			ctx, err := fetchContext(s)
			if err != nil {
				return err
			}
			proposed, err := contextValue(cmd, file, value, ctx[field])
			if err != nil {
				return err
			}
			toolArgs := map[string]any{"proposals": []any{map[string]any{
				"field":     field,
				"current":   stringify(ctx[field]),
				"proposed":  proposed,
				"rationale": rationale,
			}}}
			setIf(cmd, toolArgs, "source", "source_note", sourceNote)
			res, err := s.call("propose_context_update", toolArgs)
			if err != nil {
				return err
			}
			return render(cmd.OutOrStdout(), res)
		},
	}
	valueFlags(cmd, &file, &value)
	cmd.Flags().StringVarP(&rationale, "rationale", "r", "", "why this change (shown to the reviewer; required)")
	cmd.Flags().StringVar(&sourceNote, "source", "", "what prompted it, e.g. \"customer call with Acme 2026-04-11\"")
	return cmd
}

func newContextApplyCmd() *cobra.Command {
	var file, value, summary string
	var yes bool
	cmd := &cobra.Command{
		Use:   "apply <field>",
		Short: "Set a context field immediately, creating a new version (apply_context_update, guarded)",
		Long: `Replace a context field immediately, with no review step. Prefer
` + "`quiver context propose`" + ` unless you are certain of the change.

Prompts on a terminal; requires --yes in scripts.`,
		Example:           `  quiver context apply productCategory --value "API performance consulting" -m "Rename category" --yes`,
		Args:              exactArgs(1, "<field>"),
		ValidArgsFunction: completeContextFieldArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			field := args[0]
			if err := checkContextField(field); err != nil {
				return err
			}
			if strings.TrimSpace(summary) == "" {
				return apperr.Usage("-m/--message is required: it is recorded in version history")
			}
			s, err := newSession(cmd)
			if err != nil {
				return err
			}
			ctx, err := fetchContext(s)
			if err != nil {
				return err
			}
			v, err := contextValue(cmd, file, value, ctx[field])
			if err != nil {
				return err
			}
			if err := confirm(cmd, yes, "replace context field "+field+" immediately"); err != nil {
				return err
			}
			res, err := s.call("apply_context_update", map[string]any{
				"updates":        map[string]any{field: v},
				"change_summary": summary,
			})
			if err != nil {
				return err
			}
			return render(cmd.OutOrStdout(), res)
		},
	}
	valueFlags(cmd, &file, &value)
	cmd.Flags().StringVarP(&summary, "message", "m", "", "what changed and why (recorded in version history; required)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "apply without prompting (required in scripts)")
	return cmd
}

var digitsRe = regexp.MustCompile(`^[0-9]+$`)

func newContextRestoreCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "restore <version>",
		Short: "Restore a previous context version (restore_context_version, guarded)",
		Long: `Restore a previous context version by number (as shown in
` + "`quiver context history`" + `) or by version ID. Creates a new version with the
old content. Prompts on a terminal; requires --yes in scripts.`,
		Args: exactArgs(1, "<version>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := newSession(cmd)
			if err != nil {
				return err
			}
			versionID := args[0]
			if digitsRe.MatchString(versionID) {
				if versionID, err = resolveContextVersion(s, args[0]); err != nil {
					return err
				}
			}
			if err := confirm(cmd, yes, "restore context to version "+args[0]); err != nil {
				return err
			}
			res, err := s.call("restore_context_version", map[string]any{"version_id": versionID})
			if err != nil {
				return err
			}
			return render(cmd.OutOrStdout(), res)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "restore without prompting (required in scripts)")
	return cmd
}

// resolveContextVersion maps a version number to its ID via the history.
func resolveContextVersion(s *session, number string) (string, error) {
	res, err := s.call("get_context_history", nil)
	if err != nil {
		return "", err
	}
	data, ok := res.Data()
	if !ok {
		return "", apperr.New(apperr.CodeGeneral, "get_context_history returned non-JSON output")
	}
	var versions []struct {
		ID      string      `json:"id"`
		Version json.Number `json:"version"`
	}
	if err := json.Unmarshal(data, &versions); err != nil {
		return "", apperr.Wrap(apperr.CodeGeneral, err, "decode context history")
	}
	for _, v := range versions {
		if v.Version.String() == number {
			return v.ID, nil
		}
	}
	return "", apperr.NotFound("no context version %s in history", number)
}

func fetchContext(s *session) (map[string]any, error) {
	res, err := s.call("get_context", nil)
	if err != nil {
		return nil, err
	}
	data, ok := res.Data()
	if !ok {
		return nil, apperr.New(apperr.CodeGeneral, "get_context returned non-JSON output")
	}
	var ctx map[string]any
	if err := json.Unmarshal(data, &ctx); err != nil {
		return nil, apperr.Wrap(apperr.CodeGeneral, err, "decode context")
	}
	return ctx, nil
}

func valueFlags(cmd *cobra.Command, file, value *string) {
	cmd.Flags().StringVarP(file, "file", "f", "", "file with the complete new value (- for stdin)")
	cmd.Flags().StringVar(value, "value", "", "complete new value, for short text fields")
	cmd.MarkFlagsMutuallyExclusive("file", "value")
	cmd.MarkFlagsOneRequired("file", "value")
}

// contextValue reads the new value for a field. Text fields (and fields that
// are currently empty, unless the file is .json/.yaml/.yml) are sent as text;
// list and object fields are parsed as YAML, which also accepts JSON.
func contextValue(cmd *cobra.Command, file, value string, current any) (any, error) {
	raw := value
	if file != "" {
		var err error
		if raw, err = readInput(cmd, file); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(raw) == "" {
		return nil, apperr.Validation("new value is empty")
	}
	ext := strings.ToLower(filepath.Ext(file))
	structured := ext == ".json" || ext == ".yaml" || ext == ".yml"
	switch current.(type) {
	case []any, map[string]any:
		structured = true
	}
	if !structured {
		return strings.TrimRight(raw, "\n"), nil
	}
	var v any
	if err := yaml.Unmarshal([]byte(raw), &v); err != nil {
		return nil, apperr.Validation("parse value as YAML/JSON: %v", err)
	}
	switch current.(type) {
	case []any:
		if _, ok := v.([]any); !ok {
			return nil, apperr.Validation("field is a list; the new value must be a YAML or JSON list")
		}
	case map[string]any:
		if _, ok := v.(map[string]any); !ok {
			return nil, apperr.Validation("field is an object; the new value must be a YAML or JSON mapping")
		}
	}
	return v, nil
}

// stringify renders a current value for the reviewer.
func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func checkContextField(field string) error {
	return checkEnum("context field", field, contextFields)
}

func completeContextFields(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return contextFields, cobra.ShellCompDirectiveNoFileComp
}

func completeContextFieldArg(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return contextFields, cobra.ShellCompDirectiveNoFileComp
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}
