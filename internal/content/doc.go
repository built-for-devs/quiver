// Package content converts Quiver content items to and from markdown files
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
// Field order is fixed so repeated pulls produce identical files.
package content

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Doc is one content item as stored in a local file.
type Doc struct {
	Slug        string   `yaml:"slug" json:"slug"`
	Title       string   `yaml:"title" json:"title"`
	ContentType string   `yaml:"contentType" json:"contentType"`
	Excerpt     *string  `yaml:"excerpt" json:"excerpt"`
	Author      *string  `yaml:"author" json:"author"`
	Tags        []string `yaml:"tags" json:"tags"`
	Meta        Meta     `yaml:"meta" json:"meta"`
	OG          OG       `yaml:"og" json:"og"`

	// Quiver is server-managed state recorded at pull time. It is never sent
	// on push.
	Quiver *State `yaml:"quiver,omitempty" json:"-"`

	Body string `yaml:"-" json:"body"`
}

// Meta is SEO metadata.
type Meta struct {
	Title        *string `yaml:"title" json:"title"`
	Description  *string `yaml:"description" json:"description"`
	CanonicalURL *string `yaml:"canonicalUrl" json:"canonicalUrl"`
}

// OG is Open Graph / social card metadata.
type OG struct {
	Title           *string `yaml:"title" json:"title"`
	Description     *string `yaml:"description" json:"description"`
	ImageURL        *string `yaml:"imageUrl" json:"imageUrl"`
	TwitterCardType *string `yaml:"twitterCardType" json:"twitterCardType"`
}

// State is read-only server state.
type State struct {
	ID          string `yaml:"id,omitempty" json:"id,omitempty"`
	Status      string `yaml:"status,omitempty" json:"status,omitempty"`
	PublishedAt string `yaml:"publishedAt,omitempty" json:"publishedAt,omitempty"`
	UpdatedAt   string `yaml:"updatedAt,omitempty" json:"updatedAt,omitempty"`
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
	if strings.HasPrefix(rest, "---\n") || rest == "---" {
		front, body = "", strings.TrimPrefix(strings.TrimPrefix(rest, "---"), "\n")
	} else {
		i := strings.Index(rest, "\n---\n")
		if i < 0 {
			if !strings.HasSuffix(rest, "\n---") {
				return nil, errors.New("unterminated frontmatter: missing closing ---")
			}
			i = len(rest) - len("\n---")
			front, body = rest[:i], ""
		} else {
			front, body = rest[:i], rest[i+len("\n---\n"):]
		}
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

// FromServer decodes a get_content result. It accepts the item directly or
// wrapped in a single-key envelope such as {"content": {...}}.
func FromServer(raw json.RawMessage) (*Doc, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("decode content: %w", err)
	}
	if _, ok := probe["slug"]; !ok && len(probe) == 1 {
		for _, inner := range probe {
			raw = inner
		}
	}
	var item struct {
		Doc
		State
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, fmt.Errorf("decode content: %w", err)
	}
	if item.Doc.Slug == "" {
		return nil, errors.New("decode content: response has no slug")
	}
	d := item.Doc
	st := item.State
	if st != (State{}) {
		d.Quiver = &st
	}
	d.normalize()
	return &d, nil
}

// normalize makes equivalent documents compare equal: trailing newlines on
// the body are not significant, empty strings mean unset, and nil tags are
// the empty list.
func (d *Doc) normalize() {
	d.Body = strings.TrimRight(d.Body, "\n")
	if d.Tags == nil {
		d.Tags = []string{}
	}
	for _, p := range []**string{
		&d.Excerpt, &d.Author,
		&d.Meta.Title, &d.Meta.Description, &d.Meta.CanonicalURL,
		&d.OG.Title, &d.OG.Description, &d.OG.ImageURL, &d.OG.TwitterCardType,
	} {
		if *p != nil && strings.TrimSpace(**p) == "" {
			*p = nil
		}
	}
}

// Fields returns the editable fields as tool arguments, keyed by the API's
// field names. meta and og are sent as whole objects.
func (d *Doc) Fields() map[string]any {
	b, _ := json.Marshal(d)
	var m map[string]any
	json.Unmarshal(b, &m)
	return m
}

// Diff returns the sorted names of top-level fields whose values differ.
func Diff(local, remote *Doc) []string {
	l, r := local.Fields(), remote.Fields()
	var changed []string
	for k, lv := range l {
		lb, _ := json.Marshal(lv)
		rb, _ := json.Marshal(r[k])
		if !bytes.Equal(lb, rb) {
			changed = append(changed, k)
		}
	}
	slices.Sort(changed)
	return changed
}
