package cmd

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"testing"
)

// testdata/tools.json is a snapshot of the live tools/list input schemas.
// Refresh it with:
//
//	quiver tools ls --json | jq '[.[] | {name, inputSchema}] | sort_by(.name)' > cmd/testdata/tools.json
//
//go:embed testdata/tools.json
var toolsSnapshot []byte

type schema struct {
	Type                 any                `json:"type"`
	Properties           map[string]*schema `json:"properties"`
	Required             []string           `json:"required"`
	Enum                 []any              `json:"enum"`
	Items                *schema            `json:"items"`
	AnyOf                []*schema          `json:"anyOf"`
	AdditionalProperties any                `json:"additionalProperties"`
	PropertyNames        *schema            `json:"propertyNames"`
}

func loadSchemas(t testing.TB) map[string]*schema {
	t.Helper()
	var tools []struct {
		Name        string  `json:"name"`
		InputSchema *schema `json:"inputSchema"`
	}
	if err := json.Unmarshal(toolsSnapshot, &tools); err != nil {
		t.Fatal(err)
	}
	out := map[string]*schema{}
	for _, tl := range tools {
		out[tl.Name] = tl.InputSchema
	}
	return out
}

// validate checks v against s the way the server does: unknown keys when
// additionalProperties is false, required keys, enums, and basic types.
func validate(s *schema, v any, path string) error {
	if s == nil {
		return nil
	}
	if len(s.AnyOf) > 0 {
		for _, alt := range s.AnyOf {
			if validate(alt, v, path) == nil {
				return nil
			}
		}
		return fmt.Errorf("%s: matches no anyOf branch", path)
	}
	if len(s.Enum) > 0 && !slices.Contains(s.Enum, v) {
		return fmt.Errorf("%s: %v not in enum %v", path, v, s.Enum)
	}
	if s.Type != nil && !typeOK(s.Type, v) {
		return fmt.Errorf("%s: %T does not match type %v", path, v, s.Type)
	}
	switch t := v.(type) {
	case map[string]any:
		for _, r := range s.Required {
			if _, ok := t[r]; !ok {
				return fmt.Errorf("%s: missing required %q", path, r)
			}
		}
		for k, val := range t {
			prop, known := s.Properties[k]
			if !known {
				if s.AdditionalProperties == false {
					return fmt.Errorf("%s: unknown key %q", path, k)
				}
				if s.PropertyNames != nil {
					if err := validate(s.PropertyNames, k, path+" key"); err != nil {
						return err
					}
				}
				continue
			}
			if err := validate(prop, val, path+"."+k); err != nil {
				return err
			}
		}
	case []any:
		for i, e := range t {
			if err := validate(s.Items, e, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func typeOK(want any, v any) bool {
	var types []string
	switch w := want.(type) {
	case string:
		types = []string{w}
	case []any:
		for _, x := range w {
			types = append(types, x.(string))
		}
	}
	for _, ty := range types {
		switch ty {
		case "string":
			if _, ok := v.(string); ok {
				return true
			}
		case "number", "integer":
			if _, ok := v.(float64); ok {
				return true
			}
		case "boolean":
			if _, ok := v.(bool); ok {
				return true
			}
		case "array":
			if _, ok := v.([]any); ok {
				return true
			}
		case "object":
			if _, ok := v.(map[string]any); ok {
				return true
			}
		case "null":
			if v == nil {
				return true
			}
		}
	}
	return false
}

// TestSpecsMatchSchemas checks every key every spec command can send,
// including flags no other test exercises.
func TestSpecsMatchSchemas(t *testing.T) {
	schemas := loadSchemas(t)
	NewRoot()
	if len(specs) == 0 {
		t.Fatal("no specs registered")
	}
	for _, s := range specs {
		sch, ok := schemas[s.tool]
		if !ok {
			t.Errorf("%s: tool %q not in schema snapshot", s.verb, s.tool)
			continue
		}
		name := s.tool + " (" + s.verb + ")"
		provided := map[string]bool{}
		check := func(what, key string, enum []string) {
			if key == "" {
				return
			}
			prop, ok := sch.Properties[key]
			if !ok {
				t.Errorf("%s: %s sends unknown key %q", name, what, key)
				return
			}
			if len(enum) > 0 && len(prop.Enum) > 0 {
				var want []string
				for _, e := range prop.Enum {
					want = append(want, e.(string))
				}
				got := slices.Clone(enum)
				sort.Strings(got)
				sort.Strings(want)
				if !slices.Equal(got, want) {
					t.Errorf("%s: %s enum %v, schema has %v", name, what, got, want)
				}
			}
		}
		for _, a := range s.args {
			check("arg "+a.name, a.key, a.enum)
			check("arg "+a.name, a.altKey, nil)
			provided[a.key] = true
			if a.altKey != "" {
				provided[a.altKey] = true
			}
		}
		for _, f := range s.flags {
			check("--"+f.name, f.key, f.enum)
			check("--"+f.name, f.altKey, nil)
			if f.required {
				provided[f.key] = true
			}
		}
		for _, r := range sch.Required {
			// action_proposal's action is set by prepare from --approve/--reject.
			if !provided[r] && !(s.tool == "action_proposal" && r == "action") {
				t.Errorf("%s: required key %q is not a positional or required flag", name, r)
			}
		}
	}
}
