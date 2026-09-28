package content

import (
	"strings"
	"testing"
)

// Shape taken from GET /api/public/content/<slug>, plus MCP-only state.
const serverJSON = `{
  "id": "cnt_1", "status": "published",
  "slug": "mastra-web-agent-tabstack",
  "title": "Build a web-connected Mastra agent with Tabstack",
  "contentType": "blog_post",
  "body": "Mastra gives you a clean ` + "`Agent`" + ` abstraction.\n\n## Install\n",
  "excerpt": "A step by step tutorial.",
  "tags": ["Integrate with your model stack"],
  "author": "Steve McDougall",
  "meta": {"title": null, "description": "Meta description.", "canonicalUrl": null},
  "og": {"title": null, "description": "OG description.", "imageUrl": null, "twitterCardType": "summary_large_image"},
  "publishedAt": "2026-08-03T05:00:00.000Z",
  "updatedAt": "2026-09-22T02:36:56.856Z",
  "distributions": [{"channel": "linkedin"}]
}`

func TestRoundTrip(t *testing.T) {
	remote, err := FromServer([]byte(serverJSON))
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
	if *local.Quiver != *remote.Quiver || local.Quiver.UpdatedAt != "2026-09-22T02:36:56.856Z" {
		t.Errorf("state not preserved: %+v", local.Quiver)
	}
	b2, _ := Marshal(local)
	if string(b) != string(b2) {
		t.Errorf("output not stable:\n%s\n---\n%s", b, b2)
	}
	if !strings.HasPrefix(string(b), "---\nslug: mastra-web-agent-tabstack\ntitle: ") {
		t.Errorf("unexpected key order:\n%s", b)
	}
	if !strings.Contains(string(b), "---\nMastra gives you") {
		t.Errorf("body not after frontmatter:\n%s", b)
	}
}

func TestFromServerEnvelope(t *testing.T) {
	d, err := FromServer([]byte(`{"content": ` + serverJSON + `}`))
	if err != nil || d.Slug != "mastra-web-agent-tabstack" {
		t.Fatalf("d=%v err=%v", d, err)
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
	b.Title = "T2"
	b.Meta.Description = new("d")
	if d := Diff(a, b); strings.Join(d, ",") != "meta,title" {
		t.Errorf("diff=%v", d)
	}
}

func TestCheck(t *testing.T) {
	d, _ := FromServer([]byte(serverJSON))
	issues := Check(d)
	if HasErrors(issues) {
		t.Fatalf("valid doc has errors: %v", issues)
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
	if got := fields(issues, SevWarning); got != "og.imageUrl" {
		t.Errorf("warnings=%s", got)
	}

	d.Slug = "Bad Slug"
	d.Meta.Description = nil
	d.OG.ImageURL = new("not-a-url")
	d.OG.TwitterCardType = new("huge")
	if got := fields(Check(d), SevError); got != "slug,meta.description,og.imageUrl,og.twitterCardType" {
		t.Errorf("errors=%s", got)
	}
}
