// Package cmd defines the quiver command tree.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/built-for-devs/quiver/internal/apperr"
	"github.com/built-for-devs/quiver/internal/client"
	"github.com/built-for-devs/quiver/internal/config"
	"github.com/built-for-devs/quiver/internal/output"
)

// Version is set at build time via -ldflags "-X .../cmd.Version=...".
var Version = "dev"

// globals holds persistent flag values shared by every command.
type globals struct {
	json      bool
	apiURL    string
	workspace string
	timeout   time.Duration
}

var g globals

// NewRoot builds the command tree. Each call returns a fresh tree, which keeps
// tests independent.
func NewRoot() *cobra.Command {
	g = globals{}
	root := &cobra.Command{
		Use:   "quiver",
		Short: "Command-line client for Quiver",
		Long: `quiver is a thin, deterministic client over the Quiver MCP API.

Every read supports --json for piping. Exit codes are stable:
  0 ok   1 error   2 usage   3 auth   4 not found   5 validation   6 unavailable`,
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return apperr.Wrap(apperr.CodeUsage, err, "")
	})

	pf := root.PersistentFlags()
	pf.BoolVar(&g.json, "json", false, "output JSON (stable, for scripts)")
	pf.StringVar(&g.apiURL, "api-url", "", "override MCP endpoint URL (env "+config.EnvAPIURL+"; default derived from workspace)")
	pf.StringVarP(&g.workspace, "workspace", "w", "", "workspace slug, e.g. tabstack (env "+config.EnvWorkspace+")")
	pf.DurationVar(&g.timeout, "timeout", 30*time.Second, "request timeout")

	root.AddCommand(
		newAuthCmd(),
		newConfigCmd(),
		newContextCmd(),
		newDashboardCmd(),
		newCampaignCmd(),
		newArtifactCmd(),
		newContentCmd(),
		newResearchCmd(),
		newPerfCmd(),
		newTaskCmd(),
		newSessionCmd(),
		newCompetitorCmd(),
		newToolsCmd(),
	)
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	root := NewRoot()
	err := root.Execute()
	if err == nil {
		return apperr.CodeOK
	}
	err = classifyCobraErr(err)
	printErr(root.ErrOrStderr(), err)
	return apperr.CodeOf(err)
}

// classifyCobraErr maps cobra's untyped usage errors to exit code 2.
func classifyCobraErr(err error) error {
	if apperr.CodeOf(err) != apperr.CodeGeneral {
		return err
	}
	msg := err.Error()
	for _, p := range []string{"unknown command", "unknown flag", "unknown shorthand", "required flag", "accepts ", "requires at least", "invalid argument"} {
		if strings.HasPrefix(msg, p) {
			return apperr.Wrap(apperr.CodeUsage, err, "")
		}
	}
	return err
}

func printErr(w io.Writer, err error) {
	code := apperr.CodeOf(err)
	if g.json {
		output.JSON(w, map[string]any{"error": map[string]any{
			"code":    code,
			"kind":    apperr.Name(code),
			"message": err.Error(),
		}})
		return
	}
	fmt.Fprintln(w, "error:", err)
}

// settings resolves effective config from flags, env, and the config file.
type settings struct {
	cfg       *config.Config
	token     config.Setting
	apiURL    config.Setting
	workspace config.Setting
}

func loadSettings() (*settings, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeUsage, err, "")
	}
	s := &settings{
		cfg:       cfg,
		token:     config.Resolve("", config.EnvToken, cfg.Token),
		apiURL:    config.Resolve(g.apiURL, config.EnvAPIURL, cfg.APIURL),
		workspace: config.Resolve(g.workspace, config.EnvWorkspace, cfg.Workspace),
	}
	if ws := s.workspace.Value; ws != "" {
		if !config.ValidWorkspace(ws) {
			return nil, apperr.Usage("invalid workspace %q: use the lowercase slug from your Quiver URL", ws)
		}
		// Workspace is scoped by subdomain, so it determines the endpoint
		// unless an explicit API URL overrides it (e.g. local dev).
		if s.apiURL.Value == "" {
			s.apiURL = config.Setting{Value: config.WorkspaceURL(ws), Source: config.SourceWorkspace}
		}
	}
	return s, nil
}

func newClient(s *settings) (*client.Client, error) {
	if s.token.Value == "" {
		return nil, apperr.Auth("not logged in: run `quiver auth login` or set %s", config.EnvToken)
	}
	if s.apiURL.Value == "" {
		return nil, apperr.Usage("no workspace: run `quiver config set workspace <slug>` or set %s", config.EnvWorkspace)
	}
	return client.New(s.apiURL.Value, s.token.Value, Version, g.timeout), nil
}

// callTool is the common path for commands that map 1:1 to an MCP tool:
// resolve settings, call the tool, render the result.
func callTool(cmd *cobra.Command, tool string, args map[string]any) error {
	s, err := loadSettings()
	if err != nil {
		return err
	}
	c, err := newClient(s)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), g.timeout)
	defer cancel()
	res, err := c.CallTool(ctx, tool, args)
	if err != nil {
		return err
	}
	return render(cmd.OutOrStdout(), res)
}

func render(w io.Writer, res *client.ToolResult) error {
	data, ok := res.Data()
	if g.json {
		if ok {
			return output.JSON(w, data)
		}
		return output.JSON(w, map[string]string{"text": res.Text()})
	}
	if ok {
		return output.Human(w, data)
	}
	_, err := fmt.Fprintln(w, res.Text())
	return err
}

// emit writes a CLI-local (non-tool) result: v as JSON under --json,
// otherwise the human string.
func emit(w io.Writer, v any, human string) error {
	if g.json {
		return output.JSON(w, v)
	}
	_, err := fmt.Fprintln(w, human)
	return err
}

// args validators that return usage-coded errors.

func exactArgs(n int, names ...string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != n {
			return apperr.Usage("expected %d argument(s) %s, got %d", n, strings.Join(names, " "), len(args))
		}
		return nil
	}
}

func noArgs(_ *cobra.Command, args []string) error {
	if len(args) > 0 {
		return apperr.Usage("unexpected argument %q", args[0])
	}
	return nil
}

// readInput reads a file path, or stdin when path is "-".
func readInput(cmd *cobra.Command, path string) (string, error) {
	var b []byte
	var err error
	if path == "-" {
		b, err = io.ReadAll(cmd.InOrStdin())
	} else {
		b, err = os.ReadFile(path)
	}
	if err != nil {
		return "", apperr.Wrap(apperr.CodeUsage, err, "read input")
	}
	return string(b), nil
}

// setIf adds k=v to m when the flag was explicitly set by the user.
func setIf(cmd *cobra.Command, m map[string]any, flag, key string, v any) {
	if cmd.Flags().Changed(flag) {
		m[key] = v
	}
}

// isTerminal reports whether f is an interactive terminal.
func isTerminal(f any) bool {
	file, ok := f.(*os.File)
	if !ok {
		return false
	}
	fi, err := file.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// parseArgPairs parses repeated key=value flags. Values that are valid JSON
// (numbers, booleans, objects, arrays, quoted strings) are decoded; anything
// else is a string.
func parseArgPairs(pairs []string) (map[string]any, error) {
	out := map[string]any{}
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, apperr.Usage("invalid --arg %q: want key=value", p)
		}
		var decoded any
		if json.Unmarshal([]byte(v), &decoded) == nil {
			out[k] = decoded
		} else {
			out[k] = v
		}
	}
	return out, nil
}
