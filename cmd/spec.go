package cmd

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/built-for-devs/quiver/internal/apperr"
)

// toolSpec declares a command that maps 1:1 to an MCP tool call: positional
// arguments plus flags, each forwarded under a tool argument key. Flags are
// only sent when explicitly set, so server defaults apply otherwise.
type toolSpec struct {
	use     string // e.g. "status <id> <state>"
	short   string
	long    string
	example string
	aliases []string
	tool    string

	argKeys []string // tool argument keys for positional args, in order
	flags   []flagSpec
}

type flagKind int

const (
	flagString flagKind = iota
	flagBool
	flagInt
	flagFloat
	flagFile // value is a path (or - for stdin); its contents are sent
	flagMap  // repeatable key=value, sent as an object
)

type flagSpec struct {
	name     string // CLI flag name
	short    string // optional one-letter shorthand
	key      string // tool argument key
	kind     flagKind
	usage    string
	required bool
}

func str(name, key, usage string) flagSpec {
	return flagSpec{name: name, key: key, kind: flagString, usage: usage}
}
func boolf(name, key, usage string) flagSpec {
	return flagSpec{name: name, key: key, kind: flagBool, usage: usage}
}
func intf(name, key, usage string) flagSpec {
	return flagSpec{name: name, key: key, kind: flagInt, usage: usage}
}
func num(name, key, usage string) flagSpec {
	return flagSpec{name: name, key: key, kind: flagFloat, usage: usage}
}
func mapf(name, key, usage string) flagSpec {
	return flagSpec{name: name, key: key, kind: flagMap, usage: usage}
}

// file is the conventional -f/--file flag whose contents are sent under key.
func file(key, usage string) flagSpec {
	return flagSpec{name: "file", short: "f", key: key, kind: flagFile, usage: usage + " (- for stdin)"}
}

func (f flagSpec) req() flagSpec { f.required = true; return f }

// limitFlag is shared by list commands.
var limitFlag = intf("limit", "limit", "max results to return")

func (s toolSpec) command() *cobra.Command {
	vals := map[string]any{} // flag name -> pointer to its value

	names := positionalNames(s.use)
	cmd := &cobra.Command{
		Use:     s.use,
		Short:   s.short + " (" + s.tool + ")",
		Long:    s.long,
		Example: s.example,
		Aliases: s.aliases,
		Args:    exactArgs(len(s.argKeys), names...),
		RunE: func(cmd *cobra.Command, pos []string) error {
			toolArgs := map[string]any{}
			for i, k := range s.argKeys {
				toolArgs[k] = pos[i]
			}
			for _, f := range s.flags {
				if !cmd.Flags().Changed(f.name) {
					continue
				}
				v, err := f.value(cmd, vals[f.name])
				if err != nil {
					return err
				}
				toolArgs[f.key] = v
			}
			return callTool(cmd, s.tool, toolArgs)
		},
	}
	fl := cmd.Flags()
	for _, f := range s.flags {
		switch f.kind {
		case flagString, flagFile, flagFloat:
			// Floats are parsed at run time so a bad value is a usage error
			// with a clear message.
			vals[f.name] = fl.StringP(f.name, f.short, "", f.usage)
		case flagBool:
			vals[f.name] = fl.BoolP(f.name, f.short, false, f.usage)
		case flagInt:
			vals[f.name] = fl.IntP(f.name, f.short, 0, f.usage)
		case flagMap:
			vals[f.name] = fl.StringArrayP(f.name, f.short, nil, f.usage+" (repeatable key=value)")
		}
		if f.required {
			cmd.MarkFlagRequired(f.name)
		}
	}
	return cmd
}

// value converts a parsed flag into the value sent to the tool.
func (f flagSpec) value(cmd *cobra.Command, p any) (any, error) {
	switch f.kind {
	case flagBool:
		return *p.(*bool), nil
	case flagInt:
		return *p.(*int), nil
	case flagFloat:
		s := *p.(*string)
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, apperr.Usage("--%s: %q is not a number", f.name, s)
		}
		return n, nil
	case flagFile:
		return readInput(cmd, *p.(*string))
	case flagMap:
		return parseArgPairs(*p.(*[]string))
	default:
		return *p.(*string), nil
	}
}

// positionalNames extracts "<id>"-style placeholders from a use string.
func positionalNames(use string) []string {
	var out []string
	for _, w := range strings.Fields(use)[1:] {
		if strings.HasPrefix(w, "<") {
			out = append(out, w)
		}
	}
	return out
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
