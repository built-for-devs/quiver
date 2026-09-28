package cmd

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/built-for-devs/quiver/internal/apperr"
)

// toolSpec declares a command that maps 1:1 to an MCP tool call: positional
// arguments plus flags, each forwarded under a tool argument key. Flags are
// only sent when explicitly set, so server defaults apply otherwise.
type toolSpec struct {
	verb    string // e.g. "status"; positional placeholders are appended
	short   string
	long    string
	example string
	aliases []string
	tool    string

	args  []argSpec
	flags []flagSpec

	// prepare runs after arguments are mapped and before the call. It can
	// read CLI-only flags (key ""), add computed arguments, or refuse.
	prepare func(cmd *cobra.Command, toolArgs map[string]any) error

	// guard marks an irreversible, externally visible, or costly action. It
	// adds --yes and describes the action in the confirmation prompt; "%s" is
	// replaced with the first positional argument.
	guard string
}

// argSpec is a positional argument.
type argSpec struct {
	name   string   // placeholder shown in usage, e.g. "<id>"
	key    string   // tool argument key
	altKey string   // if set, UUIDs go to key and anything else to altKey
	enum   []string // allowed values, if restricted
}

type flagKind int

const (
	flagString  flagKind = iota
	flagBool             // sent as true/false
	flagInt              // sent as a number
	flagFloat            // parsed at run time; sent as a number
	flagStrings          // repeatable or comma-separated; sent as an array
	flagFile             // path (or - for stdin) whose contents are sent
	flagMap              // repeatable key=value; sent as an object
	flagNull             // boolean switch that sends key: null (clears a field)
	flagPairs            // repeatable a=b; sent as [{pair[0]: a, pair[1]: b}, ...]
)

type flagSpec struct {
	name     string // CLI flag name
	short    string // optional one-letter shorthand
	key      string // tool argument key; "" for CLI-only flags read by prepare
	altKey   string // if set, UUIDs go to key and anything else to altKey
	kind     flagKind
	usage    string
	enum     []string  // allowed values (for flagPairs: allowed left-hand sides)
	pair     [2]string // object keys for flagPairs
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

func strs(name, key, usage string) flagSpec {
	return flagSpec{name: name, key: key, kind: flagStrings, usage: usage + " (repeatable or comma-separated)"}
}

func mapf(name, key, usage string) flagSpec {
	return flagSpec{name: name, key: key, kind: flagMap, usage: usage + " (repeatable key=value)"}
}

func nullf(name, key, usage string) flagSpec {
	return flagSpec{name: name, key: key, kind: flagNull, usage: usage}
}

// pairs is a repeatable a=b flag sent as a list of two-key objects.
func pairs(name, key, usage string, pair [2]string, enum ...string) flagSpec {
	return flagSpec{name: name, key: key, kind: flagPairs, pair: pair, enum: enum,
		usage: usage + " (repeatable " + pair[0] + "=" + pair[1] + ")"}
}

// file is the conventional -f/--file flag whose contents are sent under key.
func file(key, usage string) flagSpec {
	return flagSpec{name: "file", short: "f", key: key, kind: flagFile, usage: usage + " (- for stdin)"}
}

// ref is a flag that accepts an ID or a name, e.g. --campaign.
func ref(name, idKey, nameKey, usage string) flagSpec {
	return flagSpec{name: name, key: idKey, altKey: nameKey, kind: flagString, usage: usage + " (ID or name)"}
}

func oneOf(name, key, usage string, values ...string) flagSpec {
	return flagSpec{name: name, key: key, kind: flagString, enum: values,
		usage: usage + " (" + strings.Join(values, ", ") + ")"}
}

func (f flagSpec) req() flagSpec { f.required = true; return f }

func (f flagSpec) withShort(s string) flagSpec { f.short = s; return f }

// id is a positional ID argument.
func id(name, key string) argSpec { return argSpec{name: name, key: key} }

// idOr is a positional argument that accepts an ID or a name/slug/title.
func idOr(name, idKey, altKey string) argSpec { return argSpec{name: name, key: idKey, altKey: altKey} }

// limitFlag is shared by list commands.
var limitFlag = intf("limit", "limit", "max results to return")

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// refKey picks idKey for UUIDs and altKey for anything else.
func refKey(v, idKey, altKey string) string {
	if altKey == "" || uuidRe.MatchString(v) {
		return idKey
	}
	return altKey
}

func checkEnum(what, v string, enum []string) error {
	if len(enum) > 0 && !slices.Contains(enum, v) {
		return apperr.Usage("invalid %s %q (valid: %s)", what, v, strings.Join(enum, ", "))
	}
	return nil
}

// specs records every toolSpec built into the current command tree, so tests
// can check them against the live tool schemas.
var specs []toolSpec

func (s toolSpec) command() *cobra.Command {
	specs = append(specs, s)
	vals := map[string]any{} // flag name -> pointer to its value

	use := s.verb
	names := make([]string, len(s.args))
	for i, a := range s.args {
		names[i] = a.name
		use += " " + a.name
	}
	short := s.short
	if s.tool != "" {
		short += " (" + s.tool + ")"
	}
	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Long:    s.long,
		Example: s.example,
		Aliases: s.aliases,
		Args:    exactArgs(len(s.args), names...),
		RunE: func(cmd *cobra.Command, pos []string) error {
			toolArgs := map[string]any{}
			for i, a := range s.args {
				if err := checkEnum(a.name, pos[i], a.enum); err != nil {
					return err
				}
				toolArgs[refKey(pos[i], a.key, a.altKey)] = pos[i]
			}
			for _, f := range s.flags {
				if f.key == "" || !cmd.Flags().Changed(f.name) {
					continue
				}
				v, err := f.value(cmd, vals[f.name])
				if err != nil {
					return err
				}
				key := f.key
				if sv, ok := v.(string); ok {
					if err := checkEnum("--"+f.name, sv, f.enum); err != nil {
						return err
					}
					key = refKey(sv, f.key, f.altKey)
				}
				toolArgs[key] = v
			}
			if s.prepare != nil {
				if err := s.prepare(cmd, toolArgs); err != nil {
					return err
				}
			}
			if s.guard != "" {
				action := s.guard
				if len(pos) > 0 {
					action = strings.ReplaceAll(action, "%s", pos[0])
				}
				yes, _ := cmd.Flags().GetBool("yes")
				if err := confirm(cmd, yes, action); err != nil {
					return err
				}
			}
			return callTool(cmd, s.tool, toolArgs)
		},
	}
	if len(s.args) > 0 && len(s.args[len(s.args)-1].enum) > 0 {
		enum := s.args[len(s.args)-1].enum
		cmd.ValidArgsFunction = func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) == len(s.args)-1 {
				return enum, cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}

	fl := cmd.Flags()
	for _, f := range s.flags {
		switch f.kind {
		case flagString, flagFile, flagFloat:
			vals[f.name] = fl.StringP(f.name, f.short, "", f.usage)
		case flagBool, flagNull:
			vals[f.name] = fl.BoolP(f.name, f.short, false, f.usage)
		case flagInt:
			vals[f.name] = fl.IntP(f.name, f.short, 0, f.usage)
		case flagStrings:
			vals[f.name] = fl.StringSliceP(f.name, f.short, nil, f.usage)
		case flagMap, flagPairs:
			vals[f.name] = fl.StringArrayP(f.name, f.short, nil, f.usage)
		}
		if f.required {
			cmd.MarkFlagRequired(f.name)
		}
		if len(f.enum) > 0 && f.kind != flagPairs {
			enum := f.enum
			cmd.RegisterFlagCompletionFunc(f.name, func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
				return enum, cobra.ShellCompDirectiveNoFileComp
			})
		}
	}
	if s.guard != "" {
		fl.BoolP("yes", "y", false, "skip the confirmation prompt (required in scripts)")
	}

	// Flags that write the same tool key (e.g. --due and --clear-due) cannot
	// be combined.
	byKey := map[string][]string{}
	for _, f := range s.flags {
		if f.key != "" {
			byKey[f.key] = append(byKey[f.key], f.name)
		}
	}
	for _, names := range byKey {
		if len(names) > 1 {
			cmd.MarkFlagsMutuallyExclusive(names...)
		}
	}
	return cmd
}

// value converts a parsed flag into the value sent to the tool.
func (f flagSpec) value(cmd *cobra.Command, p any) (any, error) {
	switch f.kind {
	case flagBool:
		return *p.(*bool), nil
	case flagNull:
		if !*p.(*bool) {
			return nil, apperr.Usage("--%s takes no value", f.name)
		}
		return nil, nil
	case flagInt:
		return *p.(*int), nil
	case flagFloat:
		s := *p.(*string)
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, apperr.Usage("--%s: %q is not a number", f.name, s)
		}
		return n, nil
	case flagStrings:
		return *p.(*[]string), nil
	case flagFile:
		return readInput(cmd, *p.(*string))
	case flagMap:
		return parseArgPairs(*p.(*[]string))
	case flagPairs:
		var out []any
		for _, kv := range *p.(*[]string) {
			a, b, ok := strings.Cut(kv, "=")
			if !ok || a == "" || b == "" {
				return nil, apperr.Usage("--%s %q: want %s=%s", f.name, kv, f.pair[0], f.pair[1])
			}
			if err := checkEnum("--"+f.name+" "+f.pair[0], a, f.enum); err != nil {
				return nil, err
			}
			out = append(out, map[string]any{f.pair[0]: a, f.pair[1]: b})
		}
		return out, nil
	default:
		return *p.(*string), nil
	}
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
