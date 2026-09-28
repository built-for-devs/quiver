package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/built-for-devs/quiver/internal/apperr"
	"github.com/built-for-devs/quiver/internal/client"
	"github.com/built-for-devs/quiver/internal/content"
	"github.com/built-for-devs/quiver/internal/output"
)

func newContentPullCmd() *cobra.Command {
	var outFile, dir string
	var all bool
	cmd := &cobra.Command{
		Use:   "pull [slug...]",
		Short: "Write content as markdown with frontmatter (get_content)",
		Long: `Write content items as markdown files with SEO/OG metadata in YAML frontmatter.

With one slug and no --dir or -o, the file is written to stdout.
The quiver: block records server state and is used by push to detect
conflicting edits. Do not edit it.`,
		Example: `  quiver content pull launch-post > posts/launch-post.md
  quiver content pull launch-post changelog-42 --dir posts
  quiver content pull --all --dir posts`,
		RunE: func(cmd *cobra.Command, slugs []string) error {
			switch {
			case all && len(slugs) > 0:
				return apperr.Usage("pass slugs or --all, not both")
			case all && dir == "":
				return apperr.Usage("--all requires --dir")
			case !all && len(slugs) == 0:
				return apperr.Usage("expected at least one slug (or --all)")
			case outFile != "" && (len(slugs) != 1 || dir != ""):
				return apperr.Usage("-o takes exactly one slug and cannot be combined with --dir")
			}
			s, err := newSession(cmd)
			if err != nil {
				return err
			}
			if all {
				if slugs, err = listSlugs(s); err != nil {
					return err
				}
			}

			// Single item to stdout.
			if outFile == "" && dir == "" {
				if len(slugs) != 1 {
					return apperr.Usage("multiple slugs need --dir")
				}
				d, err := fetchContent(s, map[string]any{"slug": slugs[0]})
				if err != nil {
					return err
				}
				if g.json {
					return output.JSON(cmd.OutOrStdout(), d.JSON())
				}
				b, err := content.Marshal(d)
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(b)
				return err
			}

			type pulled struct {
				Slug string `json:"slug"`
				Path string `json:"path"`
			}
			var done []pulled
			for _, slug := range slugs {
				path := outFile
				if path == "" {
					if strings.ContainsAny(slug, `/\`) || slug == ".." {
						return apperr.Validation("refusing unsafe slug %q", slug)
					}
					path = filepath.Join(dir, slug+".md")
				}
				d, err := fetchContent(s, map[string]any{"slug": slug})
				if err != nil {
					return err
				}
				if err := writeDoc(path, d); err != nil {
					return err
				}
				done = append(done, pulled{slug, path})
				if !g.json {
					fmt.Fprintf(cmd.OutOrStdout(), "pulled  %s\n", path)
				}
			}
			if g.json {
				return output.JSON(cmd.OutOrStdout(), done)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&outFile, "output", "o", "", "write to this file instead of stdout")
	cmd.Flags().StringVar(&dir, "dir", "", "write each item to <dir>/<slug>.md")
	cmd.Flags().BoolVar(&all, "all", false, "pull every item from list_content (requires --dir)")
	return cmd
}

// pushResult is one file's outcome. JSON field names are part of the stable
// output contract.
type pushResult struct {
	Path    string          `json:"path"`
	Slug    string          `json:"slug,omitempty"`
	Action  string          `json:"action"` // created, updated, unchanged, would_create, would_update, error
	Changed []string        `json:"changed,omitempty"`
	Issues  []content.Issue `json:"issues,omitempty"`
	Error   string          `json:"error,omitempty"`
	code    int
}

func newContentPushCmd() *cobra.Command {
	var force, dryRun bool
	cmd := &cobra.Command{
		Use:   "push <path>...",
		Short: "Sync local markdown files to Quiver (save_content / update_content)",
		Long: `Sync markdown files (or directories of .md files) back to Quiver.

For each file push validates the frontmatter, fetches the current server copy,
and sends only the fields that changed. Files that match the server are left
alone, so push is safe to run repeatedly.

New slugs are created with save_content, which always creates a draft.
Publishing only happens from the Quiver UI.

If the item changed on the server since it was pulled, push stops with exit
code 7 (conflict). Pull again to merge, or pass --force to overwrite.`,
		Example: `  quiver content push posts/launch-post.md
  quiver content push posts/ --dry-run`,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return apperr.Usage("expected at least one path")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := markdownFiles(args)
			if err != nil {
				return err
			}
			s, err := newSession(cmd)
			if err != nil {
				return err
			}
			var results []pushResult
			for _, p := range paths {
				r := pushOne(s, p, force, dryRun)
				results = append(results, r)
				if !g.json {
					printPushResult(cmd.OutOrStdout(), r)
				}
			}
			if g.json {
				if err := output.JSON(cmd.OutOrStdout(), results); err != nil {
					return err
				}
			}
			return firstFailure(results, "push")
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite remote changes made since pull")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing")
	return cmd
}

func pushOne(s *session, path string, force, dryRun bool) pushResult {
	r := pushResult{Path: path}
	fail := func(err error) pushResult {
		r.Action, r.Error, r.code = "error", err.Error(), apperr.CodeOf(err)
		return r
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return fail(apperr.Wrap(apperr.CodeUsage, err, "read"))
	}
	local, err := content.Parse(b)
	if err != nil {
		return fail(apperr.Validation("%v", err))
	}
	r.Slug = local.Slug
	if issues := content.Check(local); content.HasErrors(issues) {
		r.Issues = issues
		return fail(apperr.Validation("invalid frontmatter (run `quiver content check %s`)", path))
	}

	// Look up by the pulled ID when there is one, so a slug edit in the file
	// can't silently target a different piece.
	var id string
	lookup := map[string]any{"slug": local.Slug}
	if local.Quiver != nil && local.Quiver.ID != "" {
		id = local.Quiver.ID
		lookup = map[string]any{"content_id": id}
	}
	remote, err := fetchContent(s, lookup)
	notFound := apperr.CodeOf(err) == apperr.CodeNotFound
	if err != nil && !notFound {
		return fail(err)
	}

	if notFound {
		if id != "" && !force {
			return fail(apperr.Conflict("content %s, which this file was pulled from, no longer exists: deleted? use --force to create a new draft", id))
		}
		if dryRun {
			r.Action = "would_create"
			return r
		}
		res, err := s.call("save_content", content.CreateArgs(local))
		if err != nil {
			return fail(err)
		}
		r.Action = "created"
		return adoptBase(s, path, local, res, r)
	}

	if remote.Slug != local.Slug {
		return fail(apperr.Validation("slug changed from %q to %q: slugs can't be renamed from the CLI; rename it in Quiver and pull again", remote.Slug, local.Slug))
	}
	r.Changed = content.Diff(local, remote)
	if len(r.Changed) == 0 {
		r.Action = "unchanged"
		if dryRun {
			return r
		}
		return fastForward(path, local, remote, r)
	}

	if !force {
		switch {
		case local.Quiver == nil || local.Quiver.UpdatedAt == "":
			return fail(apperr.Conflict("%q already exists on server and this file has no quiver: base: pull it first, or use --force to overwrite", local.Slug))
		case remote.Quiver != nil && remote.Quiver.UpdatedAt != local.Quiver.UpdatedAt:
			return fail(apperr.Conflict("%q changed on server since pull (pulled %s, server %s): pull again, or use --force to overwrite", local.Slug, local.Quiver.UpdatedAt, remote.Quiver.UpdatedAt))
		}
	}
	if dryRun {
		r.Action = "would_update"
		return r
	}
	args := content.UpdateArgs(local, r.Changed)
	if remote.Quiver != nil && remote.Quiver.ID != "" {
		args["content_id"] = remote.Quiver.ID
	} else {
		args["slug"] = remote.Slug
	}
	res, err := s.call("update_content", args)
	if err != nil {
		return fail(err)
	}
	r.Action = "updated"
	return adoptBase(s, path, local, res, r)
}

// adoptBase records the post-write server state in the file so the next push
// compares against it. save_content and update_content return the full
// piece; fall back to fetching it if the response can't be decoded.
func adoptBase(s *session, path string, local *content.Doc, res *client.ToolResult, r pushResult) pushResult {
	var remote *content.Doc
	if data, ok := res.Data(); ok {
		remote, _ = content.FromServer(data)
	}
	if remote == nil || remote.Quiver == nil {
		var err error
		if remote, err = fetchContent(s, map[string]any{"slug": local.Slug}); err != nil {
			r.Error = "pushed, but could not refresh quiver: block: " + err.Error()
			return r
		}
	}
	return fastForward(path, local, remote, r)
}

func fastForward(path string, local, remote *content.Doc, r pushResult) pushResult {
	if remote.Quiver == nil || (local.Quiver != nil && *local.Quiver == *remote.Quiver) {
		return r
	}
	local.Quiver = remote.Quiver
	if err := writeDoc(path, local); err != nil {
		r.Error = "pushed, but could not update file: " + err.Error()
	}
	return r
}

func printPushResult(w io.Writer, r pushResult) {
	switch r.Action {
	case "error":
		fmt.Fprintf(w, "%-12s %s: %s\n", "failed", r.Path, r.Error)
		for _, i := range r.Issues {
			if i.Severity == content.SevError {
				fmt.Fprintf(w, "             %s: %s\n", i.Field, i.Message)
			}
		}
	case "updated", "would_update":
		fmt.Fprintf(w, "%-12s %s  (%s)\n", strings.ReplaceAll(r.Action, "_", " "), r.Path, strings.Join(r.Changed, ", "))
	case "created", "would_create":
		fmt.Fprintf(w, "%-12s %s  (draft)\n", strings.ReplaceAll(r.Action, "_", " "), r.Path)
	default:
		fmt.Fprintf(w, "%-12s %s\n", r.Action, r.Path)
	}
	if r.Action != "error" && r.Error != "" {
		fmt.Fprintf(w, "             warning: %s\n", r.Error)
	}
}

func newContentCheckCmd() *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:   "check <path>...",
		Short: "Validate frontmatter and SEO/OG metadata offline",
		Long: `Validate markdown files without contacting the server. Exits 5 if any file
has errors, or warnings with --strict. Use in CI to fail builds on incomplete
metadata.

Errors: missing slug, title, contentType, body, meta.description,
og.description; malformed slug, URLs, or twitterCardType; unknown keys.
Warnings: missing excerpt or og.imageUrl; titles/descriptions over
recommended lengths.`,
		Example: `  quiver content check posts/
  quiver content check posts/launch-post.md --strict --json`,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return apperr.Usage("expected at least one path")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := markdownFiles(args)
			if err != nil {
				return err
			}
			type fileIssues struct {
				Path   string          `json:"path"`
				OK     bool            `json:"ok"`
				Issues []content.Issue `json:"issues"`
			}
			var all []fileIssues
			failed := 0
			for _, p := range paths {
				fi := fileIssues{Path: p, Issues: []content.Issue{}}
				b, err := os.ReadFile(p)
				if err != nil {
					return apperr.Wrap(apperr.CodeUsage, err, "read")
				}
				if d, err := content.Parse(b); err != nil {
					fi.Issues = append(fi.Issues, content.Issue{Field: "frontmatter", Severity: content.SevError, Message: err.Error()})
				} else {
					fi.Issues = append(fi.Issues, content.Check(d)...)
				}
				fi.OK = !content.HasErrors(fi.Issues) && !(strict && len(fi.Issues) > 0)
				if !fi.OK {
					failed++
				}
				all = append(all, fi)
			}

			w := cmd.OutOrStdout()
			if g.json {
				if err := output.JSON(w, all); err != nil {
					return err
				}
			} else {
				for _, fi := range all {
					status := "ok"
					if !fi.OK {
						status = "FAIL"
					}
					fmt.Fprintf(w, "%-5s %s\n", status, fi.Path)
					for _, i := range fi.Issues {
						fmt.Fprintf(w, "      %-7s %s: %s\n", i.Severity, i.Field, i.Message)
					}
				}
			}
			if failed > 0 {
				return apperr.Validation("%d of %d file(s) failed validation", failed, len(all))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "treat warnings as failures")
	return cmd
}

// fetchContent gets one piece by content_id or slug and decodes it.
func fetchContent(s *session, lookup map[string]any) (*content.Doc, error) {
	res, err := s.call("get_content", lookup)
	if err != nil {
		return nil, err
	}
	data, ok := res.Data()
	if !ok {
		return nil, apperr.New(apperr.CodeGeneral, "get_content returned non-JSON output")
	}
	d, err := content.FromServer(data)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeGeneral, err, "")
	}
	return d, nil
}

// listSlugs returns every slug from list_content. Accepts a bare array or a
// single-key envelope like {"items": [...]}.
func listSlugs(s *session) ([]string, error) {
	res, err := s.call("list_content", nil)
	if err != nil {
		return nil, err
	}
	data, ok := res.Data()
	if !ok {
		return nil, apperr.New(apperr.CodeGeneral, "list_content returned non-JSON output")
	}
	var items []struct {
		Slug string `json:"slug"`
	}
	if json.Unmarshal(data, &items) != nil {
		var env map[string]json.RawMessage
		if json.Unmarshal(data, &env) != nil || len(env) != 1 {
			return nil, apperr.New(apperr.CodeGeneral, "unexpected list_content response shape")
		}
		for _, inner := range env {
			if err := json.Unmarshal(inner, &items); err != nil {
				return nil, apperr.New(apperr.CodeGeneral, "unexpected list_content response shape")
			}
		}
	}
	slugs := make([]string, 0, len(items))
	for _, it := range items {
		if it.Slug != "" {
			slugs = append(slugs, it.Slug)
		}
	}
	return slugs, nil
}

// markdownFiles expands args: files are taken as-is, directories are walked
// for *.md files (skipping hidden directories).
func markdownFiles(args []string) ([]string, error) {
	var out []string
	for _, a := range args {
		fi, err := os.Stat(a)
		if err != nil {
			return nil, apperr.Wrap(apperr.CodeUsage, err, "")
		}
		if !fi.IsDir() {
			out = append(out, a)
			continue
		}
		err = filepath.WalkDir(a, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && p != a && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(p, ".md") {
				out = append(out, p)
			}
			return nil
		})
		if err != nil {
			return nil, apperr.Wrap(apperr.CodeUsage, err, "")
		}
	}
	if len(out) == 0 {
		return nil, apperr.Usage("no .md files found in %s", strings.Join(args, ", "))
	}
	return out, nil
}

func writeDoc(path string, d *content.Doc) error {
	b, err := content.Marshal(d)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return apperr.Wrap(apperr.CodeGeneral, err, "create directory")
		}
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return apperr.Wrap(apperr.CodeGeneral, err, "write file")
	}
	return nil
}

// firstFailure returns an error carrying the exit code of the first failed
// result, or nil if all succeeded.
func firstFailure(results []pushResult, verb string) error {
	failed, code := 0, 0
	for _, r := range results {
		if r.Action == "error" {
			if failed == 0 {
				code = r.code
			}
			failed++
		}
	}
	if failed == 0 {
		return nil
	}
	return apperr.New(code, fmt.Sprintf("%s failed for %d of %d file(s)", verb, failed, len(results)))
}
