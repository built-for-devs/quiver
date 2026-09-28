package cmd

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/built-for-devs/quiver/internal/apperr"
	"github.com/built-for-devs/quiver/internal/client"
	"github.com/built-for-devs/quiver/internal/config"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage credentials (reuses qvr_ tokens with the mcp scope)",
	}
	cmd.AddCommand(newAuthLoginCmd(), newAuthLogoutCmd(), newAuthWhoamiCmd())
	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	var token string
	var noVerify bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store an API token in the config file",
		Long: `Store a qvr_ token (with the mcp scope) in ~/.quiver/config.

Pass --token - to read the token from stdin and keep it out of shell history:
  pbpaste | quiver auth login --token -

In CI, skip login and set QUIVER_TOKEN instead.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if token == "-" || (token == "" && !isTerminal(cmd.InOrStdin())) {
				line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if err != nil && line == "" {
					return apperr.Usage("read token from stdin: %v", err)
				}
				token = line
			}
			token = strings.TrimSpace(token)
			if token == "" {
				return apperr.Usage("--token is required (use --token - to read from stdin)")
			}
			if !strings.HasPrefix(token, config.TokenPrefix) {
				return apperr.Usage("token must start with %q", config.TokenPrefix)
			}

			s, err := loadSettings()
			if err != nil {
				return err
			}
			var info *client.ServerInfo
			if !noVerify {
				if s.apiURL.Value == "" {
					return apperr.Usage("no API URL to verify against: run `quiver config set workspace <slug>` first, or pass --no-verify")
				}
				c := client.New(s.apiURL.Value, token, Version, g.timeout)
				ctx, cancel := context.WithTimeout(cmd.Context(), g.timeout)
				defer cancel()
				if info, err = c.Initialize(ctx); err != nil {
					return fmt.Errorf("token not saved: %w", err)
				}
			}

			s.cfg.Token = token
			if err := s.cfg.Save(); err != nil {
				return err
			}
			path, _ := config.Path()
			human := "Logged in. Token saved to " + path
			if info != nil && info.Name != "" {
				human += fmt.Sprintf(" (server: %s %s)", info.Name, info.Version)
			}
			return emit(cmd.OutOrStdout(), map[string]any{
				"ok":       true,
				"verified": !noVerify,
				"config":   path,
			}, human)
		},
	}
	cmd.Flags().StringVar(&token, "token", "", "qvr_ API token, or - to read from stdin")
	cmd.Flags().BoolVar(&noVerify, "no-verify", false, "save without contacting the server")
	return cmd
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored token",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := loadSettings()
			if err != nil {
				return err
			}
			s.cfg.Token = ""
			if err := s.cfg.Save(); err != nil {
				return err
			}
			return emit(cmd.OutOrStdout(), map[string]any{"ok": true}, "Logged out.")
		},
	}
}

func newAuthWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Verify the token and show the active server and workspace",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			info, err := c.Initialize(ctx)
			if err != nil {
				return err
			}
			out := map[string]any{
				"api_url":          s.apiURL.Value,
				"token":            config.MaskToken(s.token.Value),
				"token_source":     s.token.Source,
				"workspace":        s.workspace.Value,
				"server":           info.Name,
				"server_version":   info.Version,
				"protocol_version": info.ProtocolVersion,
			}
			ws := s.workspace.Value
			if ws == "" {
				ws = "(default)"
			}
			human := fmt.Sprintf("Authenticated to %s\n  server:    %s %s\n  workspace: %s\n  token:     %s (from %s)",
				s.apiURL.Value, info.Name, info.Version, ws, config.MaskToken(s.token.Value), s.token.Source)
			return emit(cmd.OutOrStdout(), out, human)
		},
	}
}
