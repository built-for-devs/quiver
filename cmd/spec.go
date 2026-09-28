package cmd

import (
	"github.com/spf13/cobra"
)

// toolSpec declares a command that maps 1:1 to an MCP tool call: an optional
// positional argument plus flags, each forwarded under a tool argument key.
// Flags are only sent when explicitly set, so server defaults apply otherwise.
type toolSpec struct {
	use     string // e.g. "get <id>"
	short   string
	long    string
	aliases []string
	tool    string

	argKey string // tool argument key for the positional arg; "" means no positional
	flags  []flagSpec
}

type flagKind int

const (
	flagString flagKind = iota
	flagBool
	flagInt
)

type flagSpec struct {
	name  string // CLI flag name
	key   string // tool argument key
	kind  flagKind
	usage string
}

func str(name, key, usage string) flagSpec   { return flagSpec{name, key, flagString, usage} }
func boolf(name, key, usage string) flagSpec { return flagSpec{name, key, flagBool, usage} }
func intf(name, key, usage string) flagSpec  { return flagSpec{name, key, flagInt, usage} }

// limitFlag is shared by list commands.
var limitFlag = intf("limit", "limit", "max results to return")

func (s toolSpec) command() *cobra.Command {
	strs := map[string]*string{}
	bools := map[string]*bool{}
	ints := map[string]*int{}

	args := noArgs
	if s.argKey != "" {
		args = exactArgs(1, s.use)
	}
	cmd := &cobra.Command{
		Use:     s.use,
		Short:   s.short + " (" + s.tool + ")",
		Long:    s.long,
		Aliases: s.aliases,
		Args:    args,
		RunE: func(cmd *cobra.Command, pos []string) error {
			toolArgs := map[string]any{}
			if s.argKey != "" {
				toolArgs[s.argKey] = pos[0]
			}
			for _, f := range s.flags {
				switch f.kind {
				case flagString:
					setIf(cmd, toolArgs, f.name, f.key, *strs[f.name])
				case flagBool:
					setIf(cmd, toolArgs, f.name, f.key, *bools[f.name])
				case flagInt:
					setIf(cmd, toolArgs, f.name, f.key, *ints[f.name])
				}
			}
			return callTool(cmd, s.tool, toolArgs)
		},
	}
	for _, f := range s.flags {
		switch f.kind {
		case flagString:
			strs[f.name] = cmd.Flags().String(f.name, "", f.usage)
		case flagBool:
			bools[f.name] = cmd.Flags().Bool(f.name, false, f.usage)
		case flagInt:
			ints[f.name] = cmd.Flags().Int(f.name, 0, f.usage)
		}
	}
	return cmd
}

// group builds a parent command from specs plus any hand-written children.
func group(use, short string, aliases []string, specs []toolSpec, extra ...*cobra.Command) *cobra.Command {
	cmd := &cobra.Command{Use: use, Short: short, Aliases: aliases}
	for _, s := range specs {
		cmd.AddCommand(s.command())
	}
	cmd.AddCommand(extra...)
	return cmd
}
