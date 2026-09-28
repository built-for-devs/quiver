package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/built-for-devs/quiver/internal/apperr"
)

// contentStore backs get/save/update_content in the fake server.
type contentStore struct {
	items   map[string]map[string]any
	version int
}

func (s *contentStore) bump(item map[string]any) {
	s.version++
	item["updatedAt"] = "2026-09-0" + string(rune('0'+s.version)) + "T00:00:00Z"
}

func newContentServer(t *testing.T) (*fakeMCP, *contentStore, map[string]string) {
	f, env := server(t, false)
	store := &contentStore{items: map[string]map[string]any{}}
	store.items["launch-post"] = map[string]any{
		"id": "cnt_1", "status": "published", "slug": "launch-post",
		"title": "Launch", "contentType": "blog_post", "body": "Hello.\n",
		"excerpt": "Short.", "tags": []any{"launch"}, "author": "Sam",
		"meta": map[string]any{"title": nil, "description": "Meta desc.", "canonicalUrl": nil},
		"og":   map[string]any{"title": nil, "description": "OG desc.", "imageUrl": "https://x.dev/og.png", "twitterCardType": "summary_large_image"},
	}
	store.bump(store.items["launch-post"])

	asResult := func(v any) map[string]any {
		b, _ := json.Marshal(v)
		return textResult(string(b))
	}
	notFound := map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "Content not found"}}}
	f.handlers = map[string]func(map[string]any) map[string]any{
		"get_content": func(a map[string]any) map[string]any {
			if it, ok := store.items[a["slug"].(string)]; ok {
				return asResult(it)
			}
			return notFound
		},
		"list_content": func(map[string]any) map[string]any {
			var list []any
			for _, it := range store.items {
				list = append(list, map[string]any{"slug": it["slug"]})
			}
			return asResult(map[string]any{"items": list})
		},
		"save_content": func(args map[string]any) map[string]any {
			a := map[string]any{}
			for k, v := range args {
				a[k] = v
			}
			a["id"], a["status"] = "cnt_new", "draft"
			store.bump(a)
			store.items[a["slug"].(string)] = a
			return asResult(a)
		},
		"update_content": func(a map[string]any) map[string]any {
			it, ok := store.items[a["slug"].(string)]
			if !ok {
				return notFound
			}
			for k, v := range a {
				it[k] = v
			}
			store.bump(it)
			return asResult(it)
		},
	}
	return f, store, env
}

func callNames(f *fakeMCP) string {
	var n []string
	for _, c := range f.calls {
		n = append(n, c["name"].(string))
	}
	return strings.Join(n, ",")
}

func TestPullPushRoundTrip(t *testing.T) {
	f, store, env := newContentServer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "launch-post.md")

	if _, stderr, code := run(t, env, "content", "pull", "launch-post", "--dir", dir); code != 0 {
		t.Fatalf("pull: code=%d stderr=%s", code, stderr)
	}
	pulled, _ := os.ReadFile(path)
	if !strings.Contains(string(pulled), "updatedAt: \"2026-09-01T00:00:00Z\"") {
		t.Fatalf("pulled file missing base:\n%s", pulled)
	}

	// Unchanged: no write.
	f.calls = nil
	out, stderr, code := run(t, env, "content", "push", path)
	if code != 0 || !strings.Contains(out, "unchanged") || strings.Contains(callNames(f), "update_content") {
		t.Fatalf("unchanged push: code=%d out=%s stderr=%s calls=%s", code, out, stderr, callNames(f))
	}

	// Edit title: only the title is sent, and the base is fast-forwarded.
	os.WriteFile(path, []byte(strings.Replace(string(pulled), "title: Launch", "title: Launch day", 1)), 0o644)
	f.calls = nil
	out, stderr, code = run(t, env, "content", "push", path)
	if code != 0 || !strings.Contains(out, "updated") {
		t.Fatalf("push: code=%d out=%s stderr=%s", code, out, stderr)
	}
	var update map[string]any
	for _, c := range f.calls {
		if c["name"] == "update_content" {
			update = c["arguments"].(map[string]any)
		}
	}
	if len(update) != 2 || update["title"] != "Launch day" || update["slug"] != "launch-post" {
		t.Errorf("update args = %v, want only slug+title", update)
	}
	if store.items["launch-post"]["title"] != "Launch day" {
		t.Error("server not updated")
	}
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "2026-09-02T00:00:00Z") {
		t.Errorf("base not refreshed:\n%s", after)
	}

	// Second push of the same file is a no-op.
	if out, _, _ := run(t, env, "content", "push", path); !strings.Contains(out, "unchanged") {
		t.Errorf("repeat push: %s", out)
	}
}

func TestPushConflict(t *testing.T) {
	f, store, env := newContentServer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "launch-post.md")
	run(t, env, "content", "pull", "launch-post", "-o", path)

	// Someone edits in the Quiver UI after our pull.
	store.items["launch-post"]["body"] = "Edited in UI.\n"
	store.bump(store.items["launch-post"])

	b, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(string(b), "title: Launch", "title: Mine", 1)), 0o644)

	f.calls = nil
	_, stderr, code := run(t, env, "content", "push", path)
	if code != apperr.CodeConflict || strings.Contains(callNames(f), "update_content") {
		t.Fatalf("code=%d stderr=%s calls=%s", code, stderr, callNames(f))
	}
	if _, stderr, code := run(t, env, "content", "push", path, "--force"); code != 0 {
		t.Fatalf("--force: code=%d stderr=%s", code, stderr)
	}
	if store.items["launch-post"]["title"] != "Mine" {
		t.Error("force push did not update")
	}
}

func TestPushCreatesDraft(t *testing.T) {
	f, store, env := newContentServer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "new-post.md")
	os.WriteFile(path, []byte(`---
slug: new-post
title: New post
contentType: blog_post
excerpt: Short.
meta:
  description: Meta desc.
og:
  description: OG desc.
  imageUrl: https://x.dev/og.png
---
Body text.
`), 0o644)

	// Dry run: nothing written.
	out, _, code := run(t, env, "content", "push", dir, "--dry-run")
	if code != 0 || !strings.Contains(out, "would create") || strings.Contains(callNames(f), "save_content") {
		t.Fatalf("dry run: code=%d out=%s calls=%s", code, out, callNames(f))
	}

	out, stderr, code := run(t, env, "content", "push", dir, "--json")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	var results []pushResult
	json.Unmarshal([]byte(out), &results)
	if len(results) != 1 || results[0].Action != "created" {
		t.Fatalf("results=%s", out)
	}
	if store.items["new-post"]["status"] != "draft" {
		t.Error("not created as draft")
	}
	if _, hasStatus := f.calls[len(f.calls)-2]["arguments"].(map[string]any)["status"]; hasStatus {
		t.Error("push must never send status")
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "id: cnt_new") {
		t.Errorf("base not recorded after create:\n%s", b)
	}
}

func TestPushExistingWithoutBaseConflicts(t *testing.T) {
	_, _, env := newContentServer(t)
	path := filepath.Join(t.TempDir(), "launch-post.md")
	os.WriteFile(path, []byte("---\nslug: launch-post\ntitle: Other\ncontentType: blog_post\nmeta:\n  description: d\nog:\n  description: d\n---\nBody\n"), 0o644)
	if _, _, code := run(t, env, "content", "push", path); code != apperr.CodeConflict {
		t.Fatalf("code=%d", code)
	}
}

func TestPushInvalidBlocksWrite(t *testing.T) {
	f, _, env := newContentServer(t)
	path := filepath.Join(t.TempDir(), "x.md")
	os.WriteFile(path, []byte("---\nslug: x\ntitle: X\n---\nBody\n"), 0o644)
	out, _, code := run(t, env, "content", "push", path)
	if code != apperr.CodeValidation || len(f.calls) != 0 || !strings.Contains(out, "meta.description") {
		t.Fatalf("code=%d calls=%s out=%s", code, callNames(f), out)
	}
}

func TestPullAll(t *testing.T) {
	_, store, env := newContentServer(t)
	store.items["second"] = map[string]any{"slug": "second", "title": "Second", "contentType": "changelog", "body": "b"}
	dir := t.TempDir()
	if _, stderr, code := run(t, env, "content", "pull", "--all", "--dir", dir); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	for _, name := range []string{"launch-post.md", "second.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Error(err)
		}
	}
}

func TestCheckCommand(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.md")
	os.WriteFile(good, []byte("---\nslug: good\ntitle: Good\ncontentType: blog_post\nmeta:\n  description: d\nog:\n  description: d\n---\nBody\n"), 0o644)

	if _, _, code := run(t, nil, "content", "check", good); code != 0 {
		t.Errorf("good: code=%d", code)
	}
	// Warnings (no excerpt, no og.imageUrl) only fail with --strict.
	if _, _, code := run(t, nil, "content", "check", good, "--strict"); code != apperr.CodeValidation {
		t.Errorf("strict: code=%d", code)
	}
	os.WriteFile(filepath.Join(dir, "bad.md"), []byte("---\nslug: bad\nunknown: 1\n---\n"), 0o644)
	out, _, code := run(t, nil, "content", "check", dir, "--json")
	if code != apperr.CodeValidation || !strings.Contains(out, `"ok": false`) || !strings.Contains(out, "unknown") {
		t.Errorf("dir: code=%d out=%s", code, out)
	}
}
