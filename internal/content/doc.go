// Package content converts Quiver content pieces to and from markdown files
// with YAML frontmatter, and diffs local files against the server copy.
//
// File layout:
//
//	---
//	slug: launch-post
//	title: ...
//	...editable fields...
//	# Managed by Quiver. Do not edit; used to detect conflicting edits.
//	quiver:
//	  id: ...
//	  status: draft
//	  updatedAt: ...
//	---
//	markdown body
//
// Field order is fixed so repeated pulls produce identical files. The server
// uses flat fields (metaTitle in responses, meta_title in tool arguments);
// the file groups them under meta: and og: for readability. The fields table
// is the single mapping between the three.
package content

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Doc is one content piece as stored in a local file.
type Doc struct {
	Slug        string   `yaml:"slug"`
	Title       string   `yaml:"title"`
	ContentType string   `yaml:"contentType"`
	Excerpt     *string  `yaml:"excerpt"`
	Author      *string  `yaml:"author"`
	Tags        []string `yaml:"tags"`
	Meta        Meta     `yaml:"meta"`
	OG          OG       `yaml:"og"`

	// Quiver is read-only server state recorded at pull time. It is never
	// sent on push.
	Quiver *State `yaml:"quiver,omitempty"`

	Body string `yaml:"-"`
}

// Meta is SEO metadata.
type Meta struct {
	Title             *string  `yaml:"title"`
	Description       *string  `yaml:"description"`
	CanonicalURL      *string  `yaml:"canonicalUrl"`
	TargetKeyword     *string  `yaml:"targetKeyword"`
	SecondaryKeywords []string `yaml:"secondaryKeywords"`
}

// OG is Open Graph / social card metadata.
type OG struct {
	Title       *string `yaml:"title"`
	Description *string `yaml:"description"`
	ImageURL    *string `yaml:"imageUrl"`
}

// State is read-only server state.
type State struct {
	ID              string `yaml:"id,omitempty"`
	Status          string `yaml:"status,omitempty"`
	TwitterCardType string `yaml:"twitterCardType,omitempty"`
	PublishedAt     string `yaml:"publishedAt,omitempty"`
	UpdatedAt       string `yaml:"updatedAt,omitempty"`
}

// field maps one editable value between the file, the server response, and
// tool arguments.
type field struct {
	path string          // name in the file, e.g. "meta.title"; used in diffs and issues
	arg  string          // tool argument key, e.g. "meta_title"
	get  func(*Doc) any  // normalized value: string or []string
	set  func(*Doc, any) // from a decoded server value
}

func strField(path, arg string, p func(*Doc) **string) field {
	return field{path: path, arg: arg,
		get: func(d *Doc) any { return deref(*p(d)) },
		set: func(d *Doc, v any) {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				*p(d) = &s
			}
		}}
}

func listField(path, arg string, p func(*Doc) *[]string) field {
	return field{path: path, arg: arg,
		get: func(d *Doc) any { return nonNil(*p(d)) },
		set: func(d *Doc, v any) {
			var out []string
			if arr, ok := v.([]any); ok {
				for _, e := range arr {
					if s, ok := e.(string); ok {
						out = append(out, s)
					}
				}
			}
			*p(d) = nonNil(out)
		}}
}

func plainField(path, arg string, p func(*Doc) *string) field {
	return field{path: path, arg: arg,
		get: func(d *Doc) any { return *p(d) },
		set: func(d *Doc, v any) {
			if s, ok := v.(string); ok {
				*p(d) = s
			}
		}}
}

// fields lists every editable field. Keys of the server response are the
// camelCase form of arg (meta_title -> metaTitle).
var fields = []field{
	plainField("title", "title", func(d *Doc) *string { return &d.Title }),
	plainField("contentType", "content_type", func(d *Doc) *string { return &d.ContentType }),
	strField("excerpt", "excerpt", func(d *Doc) **string { return &d.Excerpt }),
	strField("author", "author", func(d *Doc) **string { return &d.Author }),
	listField("tags", "tags", func(d *Doc) *[]string { return &d.Tags }),
	strField("meta.title", "meta_title", func(d *Doc) **string { return &d.Meta.Title }),
	strField("meta.description", "meta_description", func(d *Doc) **string { return &d.Meta.Description }),
	strField("meta.canonicalUrl", "canonical_url", func(d *Doc) **string { return &d.Meta.CanonicalURL }),
	strField("meta.targetKeyword", "target_keyword", func(d *Doc) **string { return &d.Meta.TargetKeyword }),
	listField("meta.secondaryKeywords", "secondary_keywords", func(d *Doc) *[]string { return &d.Meta.SecondaryKeywords }),
	strField("og.title", "og_title", func(d *Doc) **string { return &d.OG.Title }),
	strField("og.description", "og_description", func(d *Doc) **string { return &d.OG.Description }),
	strField("og.imageUrl", "og_image_url", func(d *Doc) **string { return &d.OG.ImageURL }),
	plainField("body", "body", func(d *Doc) *string { return &d.Body }),
}

const stateComment = "# Managed by Quiver. Do not edit; used to detect conflicting edits.\n"

// ErrNoFrontmatter is returned when a file does not start with a --- block.
var ErrNoFrontmatter = errors.New("missing frontmatter: file must start with ---")

// Parse reads a markdown file with YAML frontmatter. Unknown frontmatter keys
// are rejected so typos (e.g. "descripton") fail loudly instead of silently
// dropping data.
func Parse(b []byte) (*Doc, error) {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return nil, ErrNoFrontmatter
	}
	rest := s[len("---\n"):]
	var front, body string
	switch i := strings.Index(rest, "\n---\n"); {
	case strings.HasPrefix(rest, "---\n") || rest == "---":
		body = strings.TrimPrefix(strings.TrimPrefix(rest, "---"), "\n")
	case i >= 0:
		front, body = rest[:i], rest[i+len("\n---\n"):]
	case strings.HasSuffix(rest, "\n---"):
		front = rest[:len(rest)-len("\n---")]
	default:
		return nil, errors.New("unterminated frontmatter: missing closing ---")
	}

	var d Doc
	dec := yaml.NewDecoder(strings.NewReader(front))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	d.Body = body
	d.normalize()
	return &d, nil
}

// Marshal renders d as a markdown file.
func Marshal(d *Doc) ([]byte, error) {
	d.normalize()
	editable := *d
	editable.Quiver = nil

	var buf bytes.Buffer
	buf.WriteString("---\n")
	if err := encode(&buf, &editable); err != nil {
		return nil, err
	}
	if d.Quiver != nil && *d.Quiver != (State{}) {
		buf.WriteString(stateComment)
		if err := encode(&buf, struct {
			Quiver *State `yaml:"quiver"`
		}{d.Quiver}); err != nil {
			return nil, err
		}
	}
	buf.WriteString("---\n")
	buf.WriteString(d.Body)
	if d.Body != "" {
		buf.WriteString("\n")
	}
	return buf.Bytes(), nil
}

func encode(w io.Writer, v any) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return err
	}
	return enc.Close()
}

// FromServer decodes a content piece as returned by get_content,
// save_content, and update_content (flat camelCase keys).
func FromServer(raw json.RawMessage) (*Doc, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("decode content: %w", err)
	}
	slug, _ := m["slug"].(string)
	if slug == "" {
		return nil, errors.New("decode content: response has no slug")
	}
	d := &Doc{Slug: slug}
	for _, f := range fields {
		f.set(d, m[camel(f.arg)])
	}
	str := func(k string) string { s, _ := m[k].(string); return s }
	st := State{
		ID:              str("id"),
		Status:          str("status"),
		TwitterCardType: str("twitterCardType"),
		PublishedAt:     str("publishedAt"),
		UpdatedAt:       str("updatedAt"),
	}
	if st != (State{}) {
		d.Quiver = &st
	}
	d.normalize()
	return d, nil
}

// camel converts a snake_case argument key to the camelCase response key.
func camel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// normalize makes equivalent documents compare equal: trailing newlines on
// the body are not significant, blank strings mean unset, and nil lists are
// empty lists.
func (d *Doc) normalize() {
	d.Body = strings.TrimRight(d.Body, "\n")
	d.Tags = nonNil(d.Tags)
	d.Meta.SecondaryKeywords = nonNil(d.Meta.SecondaryKeywords)
	for _, p := range []**string{
		&d.Excerpt, &d.Author,
		&d.Meta.Title, &d.Meta.Description, &d.Meta.CanonicalURL, &d.Meta.TargetKeyword,
		&d.OG.Title, &d.OG.Description, &d.OG.ImageURL,
	} {
		if *p != nil && strings.TrimSpace(**p) == "" {
			*p = nil
		}
	}
}

// Diff returns the file paths (e.g. "meta.title") of fields whose values
// differ, in file order.
func Diff(local, remote *Doc) []string {
	var changed []string
	for _, f := range fields {
		if !reflect.DeepEqual(f.get(local), f.get(remote)) {
			changed = append(changed, f.path)
		}
	}
	return changed
}

// UpdateArgs returns tool arguments for the given changed paths. A field that
// was removed locally is sent as "" (or []), which clears it on the server.
func UpdateArgs(d *Doc, changed []string) map[string]any {
	args := map[string]any{}
	for _, f := range fields {
		for _, c := range changed {
			if c == f.path {
				args[f.arg] = f.get(d)
			}
		}
	}
	return args
}

// CreateArgs returns tool arguments for save_content: slug plus every
// non-empty field.
func CreateArgs(d *Doc) map[string]any {
	args := map[string]any{"slug": d.Slug}
	for _, f := range fields {
		switch v := f.get(d).(type) {
		case string:
			if v != "" {
				args[f.arg] = v
			}
		case []string:
			if len(v) > 0 {
				args[f.arg] = v
			}
		}
	}
	return args
}

// JSON returns the document as a JSON-friendly map keyed by file paths, for
// `content pull --json`.
func (d *Doc) JSON() map[string]any {
	out := map[string]any{"slug": d.Slug}
	for _, f := range fields {
		out[f.path] = f.get(d)
	}
	if d.Quiver != nil {
		out["quiver"] = d.Quiver
	}
	return out
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
