package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/built-for-devs/quiver/internal/apperr"
)

// contentStore backs get/save/update_content in the fake server using the
// real protocol: snake_case arguments in, flat camelCase pieces out.
type contentStore struct {
	items   map[string]map[string]any // by slug
	version int
}

func (s *contentStore) bump(item map[string]any) {
	s.version++
	item["updatedAt"] = fmt.Sprintf("2026-09-%02dT00:00:00Z", s.version)
}

func (s *contentStore) find(a map[string]any) map[string]any {
	if id, ok := a["content_id"].(string); ok {
		for _, it := range s.items {
			if it["id"] == id {
				return it
			}
		}
		return nil
	}
	slug, _ := a["slug"].(string)
	return s.items[slug]
}

// apply copies snake_case tool arguments onto a camelCase piece.
func (s *contentStore) apply(item, args map[string]any) {
	for k, v := range args {
		if k == "content_id" || (k == "slug" && item["slug"] != nil) {
			continue // lookup keys
		}
		item[camelKey(k)] = v
	}
}

// camelKey maps a tool argument (meta_title) to its response key (metaTitle).
func camelKey(k string) string {
	parts := strings.Split(k, "_")
	for i := 1; i < len(parts); i++ {
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

func newContentServer(t *testing.T) (*fakeMCP, *contentStore, map[string]string) {
	f, env := server(t, false)
	store := &contentStore{items: map[string]map[string]any{}}
	var sample map[string]any
	b, _ := os.ReadFile("../internal/content/testdata/get_content.json")
	json.Unmarshal(b, &sample)
	store.items["launch-post"] = sample
	store.bump(sample)

	asResult := func(v any) map[string]any {
		b, _ := json.Marshal(v)
		return textResult(string(b))
	}
	notFound := map[string]any{"isError": true, "content": []any{map[string]any{"type": "text",
		"text": "Content piece not found. Provide content_id, slug, or title."}}}
	f.handlers = map[string]func(map[string]any) map[string]any{
		"get_content": func(a map[string]any) map[string]any {
			if it := store.find(a); it != nil {
				return asResult(it)
			}
			return notFound
		},
		"list_content": func(map[string]any) map[string]any {
			var list []any
			for _, it := range store.items {
				list = append(list, map[string]any{"id": it["id"], "slug": it["slug"], "title": it["title"]})
			}
			return asResult(list)
		},
		"save_content": func(a map[string]any) map[string]any {
			it := map[string]any{"id": fmt.Sprintf("new-%d", len(store.items)), "status": "draft"}
			store.apply(it, a)
			store.bump(it)
			store.items[it["slug"].(string)] = it
			return asResult(it)
		},
		"update_content": func(a map[string]any) map[string]any {
			it := store.find(a)
			if it == nil {
				return notFound
			}
			store.apply(it, a)
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
	if !strings.Contains(string(pulled), "updatedAt: \"2026-09-01T00:00:00Z\"") || !strings.Contains(string(pulled), "id: 00000000-0000-4000-8000-000000000001") {
		t.Fatalf("pulled file missing base:\n%s", pulled)
	}

	// Unchanged: no write.
	f.calls = nil
	out, stderr, code := run(t, env, "content", "push", path)
	if code != 0 || !strings.Contains(out, "unchanged") || strings.Contains(callNames(f), "update_content") {
		t.Fatalf("unchanged push: code=%d out=%s stderr=%s calls=%s", code, out, stderr, callNames(f))
	}

	// Edit title: only the title is sent, and the base is fast-forwarded.
	os.WriteFile(path, []byte(strings.Replace(string(pulled), "title: Launch post", "title: Launch day", 1)), 0o644)
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
	if len(update) != 2 || update["title"] != "Launch day" || update["content_id"] != "00000000-0000-4000-8000-000000000001" {
		t.Errorf("update args = %v, want only content_id+title", update)
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
	os.WriteFile(path, []byte(strings.Replace(string(b), "title: Launch post", "title: Mine", 1)), 0o644)

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
  targetKeyword: new post
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
	var save map[string]any
	for _, c := range f.calls {
		if c["name"] == "save_content" {
			save = c["arguments"].(map[string]any)
		}
	}
	if got := fmt.Sprint(save); got != "map[body:Body text. content_type:blog_post excerpt:Short. meta_description:Meta desc. og_description:OG desc. og_image_url:https://x.dev/og.png slug:new-post target_keyword:new post title:New post]" {
		t.Errorf("save args = %s", got)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "id: new-1") || strings.Count(callNames(f), "get_content") != 2 {
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
	store.items["second"] = map[string]any{"id": "id-2", "slug": "second", "title": "Second", "contentType": "changelog", "body": "b"}
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

func TestPushRejectsSlugRename(t *testing.T) {
	f, _, env := newContentServer(t)
	path := filepath.Join(t.TempDir(), "p.md")
	run(t, env, "content", "pull", "launch-post", "-o", path)
	b, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(string(b), "slug: launch-post", "slug: renamed", 1)), 0o644)
	f.calls = nil
	out, _, code := run(t, env, "content", "push", path)
	if code != apperr.CodeValidation || !strings.Contains(out, "renamed") || strings.Contains(callNames(f), "update_content") {
		t.Fatalf("code=%d out=%s calls=%s", code, out, callNames(f))
	}
}

func TestPushDeletedRemoteConflicts(t *testing.T) {
	_, store, env := newContentServer(t)
	path := filepath.Join(t.TempDir(), "p.md")
	run(t, env, "content", "pull", "launch-post", "-o", path)
	delete(store.items, "launch-post")
	b, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(string(b), "title: Launch post", "title: X", 1)), 0o644)
	if _, _, code := run(t, env, "content", "push", path); code != apperr.CodeConflict {
		t.Fatalf("code=%d", code)
	}
}
