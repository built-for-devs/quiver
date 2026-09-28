package content

import (
	_ "embed"
	"fmt"
	"strings"
	"testing"
)

// testdata/get_content.json is a real get_content response with IDs and
// text anonymized.
//
//go:embed testdata/get_content.json
var serverJSON []byte

func TestRoundTrip(t *testing.T) {
	remote, err := FromServer(serverJSON)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Marshal(remote)
	if err != nil {
		t.Fatal(err)
	}
	local, err := Parse(b)
	if err != nil {
		t.Fatalf("parse own output: %v\n%s", err, b)
	}
	if d := Diff(local, remote); len(d) != 0 {
		t.Errorf("round trip changed fields %v\n%s", d, b)
	}
	want := State{
		ID:              "00000000-0000-4000-8000-000000000001",
		Status:          "draft",
		TwitterCardType: "summary_large_image",
		UpdatedAt:       "2026-09-01T00:00:00.000Z",
	}
	if *local.Quiver != want {
		t.Errorf("state = %+v, want %+v", *local.Quiver, want)
	}
	b2, _ := Marshal(local)
	if string(b) != string(b2) {
		t.Errorf("output not stable:\n%s\n---\n%s", b, b2)
	}
	for _, s := range []string{
		"---\nslug: launch-post\ntitle: Launch post\ncontentType: blog_post\n",
		"meta:\n  title: Launch meta title\n  description: Launch meta description.\n  canonicalUrl: https://example.com/launch\n  targetKeyword: launch\n  secondaryKeywords:\n    - a\n",
		"og:\n  title: Launch OG title\n",
		"---\n# Launch\n\nBody text.\n",
	} {
		if !strings.Contains(string(b), s) {
			t.Errorf("output missing %q:\n%s", s, b)
		}
	}
}

func TestArgs(t *testing.T) {
	d, _ := FromServer(serverJSON)
	create := CreateArgs(d)
	for _, k := range []string{"slug", "title", "body", "content_type", "meta_title", "meta_description", "canonical_url",
		"target_keyword", "secondary_keywords", "og_title", "og_description", "og_image_url", "tags", "author", "excerpt"} {
		if _, ok := create[k]; !ok {
			t.Errorf("CreateArgs missing %s", k)
		}
	}
	if len(create) != 15 {
		t.Errorf("CreateArgs has %d keys: %v", len(create), create)
	}

	// Removing a field locally clears it on the server with "".
	local, _ := FromServer(serverJSON)
	local.Title = "New"
	local.OG.ImageURL = nil
	local.Meta.SecondaryKeywords = nil
	changed := Diff(local, d)
	if got := strings.Join(changed, ","); got != "title,meta.secondaryKeywords,og.imageUrl" {
		t.Fatalf("diff = %s", got)
	}
	if got := fmt.Sprint(UpdateArgs(local, changed)); got != "map[og_image_url: secondary_keywords:[] title:New]" {
		t.Errorf("UpdateArgs = %s", got)
	}
}

func TestParseRejectsUnknownKeys(t *testing.T) {
	_, err := Parse([]byte("---\nslug: x\nmeta:\n  descripton: typo\n---\nbody\n"))
	if err == nil || !strings.Contains(err.Error(), "descripton") {
		t.Fatalf("err=%v", err)
	}
}

func TestParseEdgeCases(t *testing.T) {
	if _, err := Parse([]byte("no frontmatter")); err != ErrNoFrontmatter {
		t.Errorf("err=%v", err)
	}
	if _, err := Parse([]byte("---\nslug: x\n")); err == nil {
		t.Error("unterminated frontmatter accepted")
	}
	d, err := Parse([]byte("---\r\nslug: x\r\ntitle: T\r\n---\r\nhello\r\n"))
	if err != nil || d.Slug != "x" || d.Body != "hello" {
		t.Errorf("CRLF: d=%+v err=%v", d, err)
	}
	d, err = Parse([]byte("---\nslug: x\n---"))
	if err != nil || d.Slug != "x" || d.Body != "" {
		t.Errorf("no body: d=%+v err=%v", d, err)
	}
	// A --- inside the body is content, not a delimiter.
	d, _ = Parse([]byte("---\nslug: x\n---\nabove\n---\nbelow\n"))
	if d.Body != "above\n---\nbelow" {
		t.Errorf("body=%q", d.Body)
	}
}

func TestDiffIgnoresEquivalentValues(t *testing.T) {
	a, _ := Parse([]byte("---\nslug: x\ntitle: T\nexcerpt: \"\"\n---\nbody\n\n\n"))
	b, _ := Parse([]byte("---\nslug: x\ntitle: T\ntags: []\n---\nbody"))
	if d := Diff(a, b); len(d) != 0 {
		t.Errorf("diff=%v", d)
	}
	// The server stores cleared fields as "", which equals unset.
	c, _ := FromServer([]byte(`{"slug":"x","title":"T","body":"body","excerpt":"","ogImageUrl":""}`))
	if d := Diff(a, c); len(d) != 0 {
		t.Errorf("diff vs cleared server fields=%v", d)
	}
}

func TestCheck(t *testing.T) {
	d, _ := FromServer(serverJSON)
	issues := Check(d)
	if len(issues) != 0 {
		t.Fatalf("complete doc has issues: %v", issues)
	}
	fields := func(issues []Issue, sev Severity) string {
		var f []string
		for _, i := range issues {
			if i.Severity == sev {
				f = append(f, i.Field)
			}
		}
		return strings.Join(f, ",")
	}

	d.Slug = "Bad Slug"
	d.Meta.Description = nil
	d.OG.ImageURL = new("not-a-url")
	d.Excerpt = nil
	issues = Check(d)
	if got := fields(issues, SevError); got != "slug,meta.description,og.imageUrl" {
		t.Errorf("errors=%s", got)
	}
	if got := fields(issues, SevWarning); got != "excerpt" {
		t.Errorf("warnings=%s", got)
	}
}
