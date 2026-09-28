# quiver

Command-line client for [Quiver](https://quivergtm.dev). A thin, deterministic
wrapper over the same tools the Quiver MCP server exposes: noun-verb
subcommands, `--json` on everything, and exit codes you can rely on in CI.

## Install

```sh
make install        # or: go install github.com/built-for-devs/quiver@latest
```

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
quiver campaign   ls | get | create | update | status <id> <state>
quiver artifact   ls | get | save | update | status <id> <state>
quiver content    ls | calendar | get | metrics | log-metrics | distribute
                  pull | push | check
quiver research   ls | get | add | quotes | linear <entry-id>
quiver perf       log | ls | queue | proposals | proposal <id> --approve|--reject
quiver task       ls | add | update | done <id>
quiver session    ls | get
quiver competitor ls | get | intel
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

`context propose` is the default way to change workspace context. Commands
that change the context immediately (`context apply`, `context restore`, and
`perf proposal --approve`) prompt on a terminal and refuse to run in scripts
without `--yes`.

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

## Development

```sh
make test
make lint
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
