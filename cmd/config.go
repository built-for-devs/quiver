package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/built-for-devs/quiver/internal/apperr"
	"github.com/built-for-devs/quiver/internal/config"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read and write CLI settings",
	}
	cmd.AddCommand(newConfigSetCmd(), newConfigGetCmd(), newConfigListCmd(), newConfigPathCmd())
	return cmd
}

func settableKey(key string) (func(*config.Config) *string, error) {
	field, ok := config.SettableKeys[key]
	if !ok {
		return nil, apperr.Usage("unknown key %q (valid: %s)", key, strings.Join(config.KeyNames(), ", "))
	}
	return field, nil
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value (" + strings.Join(config.KeyNames(), ", ") + ")",
		Args:  exactArgs(2, "<key>", "<value>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			field, err := settableKey(args[0])
			if err != nil {
				return err
			}
			s, err := loadSettings()
			if err != nil {
				return err
			}
			*field(s.cfg) = args[1]
			if err := s.cfg.Save(); err != nil {
				return err
			}
			return emit(cmd.OutOrStdout(), map[string]any{args[0]: args[1]}, fmt.Sprintf("%s = %s", args[0], args[1]))
		},
	}
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Print the effective value of a config key",
		Args:  exactArgs(1, "<key>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := settableKey(args[0]); err != nil {
				return err
			}
			s, err := loadSettings()
			if err != nil {
				return err
			}
			v := effective(s)[args[0]]
			return emit(cmd.OutOrStdout(), map[string]any{args[0]: v.Value, "source": v.Source}, v.Value)
		},
	}
}

func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "Show all effective settings and where they come from",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := loadSettings()
			if err != nil {
				return err
			}
			eff := effective(s)
			eff["token"] = config.Setting{Value: config.MaskToken(s.token.Value), Source: s.token.Source}
			keys := append(config.KeyNames(), "token")
			out := map[string]any{}
			var b strings.Builder
			for i, k := range keys {
				out[k] = map[string]any{"value": eff[k].Value, "source": eff[k].Source}
				if i > 0 {
					b.WriteByte('\n')
				}
				src := ""
				if eff[k].Source != config.SourceNone {
					src = "  (" + string(eff[k].Source) + ")"
				}
				fmt.Fprintf(&b, "%-10s %s%s", k, eff[k].Value, src)
			}
			return emit(cmd.OutOrStdout(), out, b.String())
		},
	}
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the config file path",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := config.Path()
			if err != nil {
				return err
			}
			return emit(cmd.OutOrStdout(), map[string]any{"path": p}, p)
		},
	}
}

func effective(s *settings) map[string]config.Setting {
	return map[string]config.Setting{
		"api_url":   s.apiURL,
		"workspace": s.workspace,
	}
}
