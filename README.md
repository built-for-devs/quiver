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
quiver context    show | history | propose -f | apply <id> | restore <version>
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
- New slugs are created as drafts. The CLI cannot publish; that happens in the
  Quiver UI.

## Guardrails

`context propose` is the default way to change workspace context.
`context apply` and `context restore` mutate immediately, so they prompt on a
terminal and refuse to run in scripts without `--yes`.

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

Tool argument names are not yet verified against the live MCP schema. Compare
them with `quiver tools describe <tool>` and fix them in `cmd/commands.go` and
`cmd/content_sync.go`.
