# quiver

Command-line client for [Quiver](https://quivergtm.dev). A thin, deterministic
wrapper over the same tools the Quiver MCP server exposes: noun-verb
subcommands, `--json` on everything, and exit codes you can rely on in CI.

## Install

```sh
brew tap built-for-devs/quiver https://github.com/built-for-devs/quiver
brew install --cask built-for-devs/quiver/quiver
```

Or download a binary for macOS, Linux, or Windows from
[Releases](https://github.com/built-for-devs/quiver/releases), or build from
source with `go install github.com/built-for-devs/quiver@latest`.

## Setup

The CLI reuses workspace API tokens with the `mcp` scope (Settings → API
tokens). The workspace slug selects the endpoint,
`https://<workspace>.quivergtm.dev/api/mcp`.

```sh
quiver config set workspace tabstack
pbpaste | quiver auth login --token -   # reads from stdin, stays out of shell history
quiver auth whoami
```

In CI, skip login and set environment variables instead:

| Variable           | Purpose                                  |
|--------------------|------------------------------------------|
| `QUIVER_TOKEN`     | `qvr_` API token                         |
| `QUIVER_WORKSPACE` | workspace slug                           |
| `QUIVER_API_URL`   | override the endpoint (e.g. local dev)   |
| `QUIVER_CONFIG`    | config file path (default `~/.quiver/config`) |

Precedence is flag, then environment, then config file.

## Commands

```
quiver dashboard
quiver context    show [--field] | history | propose <field> | apply <field> | restore <version>
quiver campaign   ls | get | create | update | status <id> <state> | delete
quiver artifact   ls | get | save | update | status <id> <state> | archive | delete
quiver content    ls | calendar | get | metrics | log-metrics | distribute | archive | delete
                  pull | push | check
quiver research   ls | get | add | update | delete | quotes | linear <entry-id>
                  quote update | quote delete
quiver perf       log | ls | queue | proposals | proposal <id> --approve|--reject
quiver task       ls | add | update | done <id>
quiver session    ls | get | delete
quiver competitor ls | get | intel | add | update | remove | scan | cadence <off|monthly|weekly>
quiver tools      ls | describe <tool> | call <tool>
```

`quiver <command> --help` shows flags and examples. `quiver tools call` reaches
any tool without a dedicated command.

Arguments that name a record accept an ID or a name: `quiver campaign get
"Q3 launch"` and `--campaign "Q3 launch"` work as well as UUIDs. Enum flags
(`--status`, `--type`, `--channel`, ...) are checked locally and tab-complete
once shell completion is installed (`quiver completion --help`).

## Editing context

Context changes are made one field at a time and replace the whole field.
Export the field, edit it, and propose the complete new value for review in
the Quiver UI:

```sh
quiver context show --field messagingPillars > pillars.yaml   # lists and objects as YAML
$EDITOR pillars.yaml
quiver context propose messagingPillars -f pillars.yaml -r "Acme call feedback"
```

Text fields (e.g. `positioningStatement`) are exported and proposed as plain text.

## Content as files

`pull` writes content as markdown with SEO and Open Graph metadata in YAML
frontmatter. `push` syncs edits back. Quiver stays the source of truth.

```sh
quiver content pull --all --dir posts
$EDITOR posts/launch-post.md
quiver content check posts/          # offline validation, for CI
quiver content push posts/ --dry-run
quiver content push posts/
```

- Keys are written in a fixed order, so repeated pulls produce identical files.
- The `quiver:` block records server state at pull time. Don't edit it.
- `push` sends only changed fields and skips unchanged files, so it is safe to
  run on every merge.
- If an item changed on the server since you pulled it, `push` stops with exit
  code 7. Pull again, or pass `--force` to overwrite.
- Files remember the piece they were pulled from (`quiver.id`), so push always
  updates that piece. Slugs can't be renamed from the CLI.
- Removing a field in the file clears it on the server.
- New slugs are created as drafts. The CLI cannot publish; that happens in the
  Quiver UI.

## Guardrails

`context propose` is the default way to change workspace context.

These commands prompt on a terminal and refuse to run in scripts without
`--yes`:

- Changing the context immediately: `context apply`, `context restore`,
  `perf proposal --approve`
- Permanent deletes: `campaign delete`, `artifact delete`, `content delete`,
  `research delete`, `research quote delete`, `session delete`,
  `competitor remove`
- Taking content off the public site: `content archive`
- Spending model tokens: `competitor scan`, and `competitor cadence` with any
  value other than `off`

## Output and exit codes

`--json` output is the stable contract; human output may change. With
`--json`, errors are written to stderr as
`{"error": {"code": 4, "kind": "not_found", "message": "..."}}`.

| Code | Kind          | Meaning                                         |
|------|---------------|-------------------------------------------------|
| 0    | ok            | success                                         |
| 1    | error         | unexpected failure                              |
| 2    | usage         | bad flags or arguments, missing config, refused guarded action |
| 3    | auth          | missing, invalid, or under-scoped token         |
| 4    | not_found     | resource does not exist                         |
| 5    | validation    | server rejected input, or `content check` failed |
| 6    | unavailable   | network error, timeout, or server 5xx           |
| 7    | conflict      | remote changed since the local copy was pulled  |

## GitHub Actions

This repo is also a GitHub Action. It installs a checksum-verified release
binary and runs any quiver command, with results in the job summary and
errors annotated on the affected files.

```yaml
- uses: built-for-devs/quiver@v1
  with:
    args: content push posts
    workspace: your-workspace
    token: ${{ secrets.QUIVER_TOKEN }}
    commit-state: "true"
```

| Input            | Default   | Description |
|------------------|-----------|-------------|
| `args`           | required  | quiver arguments; `--json` is added |
| `workspace`      |           | workspace slug |
| `token`          |           | API token with the `mcp` scope (from a secret) |
| `api-url`        |           | override the endpoint |
| `version`        | `latest`  | release tag, `latest`, or `source` to build from the action checkout |
| `fail-on-error`  | `true`    | fail the step on a non-zero exit; `"false"` to branch on `exit-code` instead |
| `commit-state`   | `false`   | commit files the command modified back to the branch |
| `commit-message` | `Sync Quiver state [skip ci]` | message for that commit |

Outputs: `exit-code`, `json` (path to the `--json` output), `committed`.
The exit code is also exported as `QUIVER_EXIT_CODE` for later steps. To act
on a specific code (e.g. 7 for a conflict) without failing the job, set
`fail-on-error: "false"` and branch on `exit-code`.

[`examples/quiver-content.yml`](examples/quiver-content.yml) is a complete
workflow for a content repo: pull requests run `content check` and a
`push --dry-run` preview; merges to `main` run `content push`.

Use `commit-state: "true"` on the push job. `push` records each post's new
server version in its file; without committing that back, the next merge of
the same post fails with a false conflict. The push job needs
`permissions: contents: write`, and branch protection must allow
`github-actions[bot]` to push.

## Releasing

Tag a version and push the tag:

```sh
git tag v1.0.0 && git push origin v1.0.0
```

The release workflow builds archives for macOS, Linux, and Windows (amd64,
arm64) with GoReleaser, publishes them with checksums, and moves the `v1` tag
so `built-for-devs/quiver@v1` picks up the release. It also commits the
updated Homebrew cask to `Casks/quiver.rb` on `main`; this repo is the tap.
Branch protection on `main`, if enabled, must allow `github-actions[bot]` to
push. Tags with a suffix (`v1.1.0-rc.1`) are marked as prereleases and don't
move `v1` or the cask.

`make snapshot` builds the same archives locally into `dist/`.

## Development

```sh
make test
make lint           # gofmt + go vet
make lint-actions   # actionlint on workflows
make build
```

Tool argument keys and enums live in `cmd/commands.go` (and the content field
table in `internal/content/doc.go`). Tests check them against a snapshot of
the live schemas in `cmd/testdata/tools.json`, and the test server rejects
calls the real server would reject. When the server's tools change, refresh
the snapshot and fix whatever fails:

```sh
quiver tools ls --json | jq '[.[] | {name, inputSchema}] | sort_by(.name)' > cmd/testdata/tools.json
make test
```
