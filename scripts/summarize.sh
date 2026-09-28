#!/usr/bin/env bash
# Render quiver --json output for GitHub Actions: a job summary table plus
# ::error/::warning annotations on the affected files.
#
# Usage: summarize.sh <args> <stdout.json> <stderr.json> <exit-code>
set -uo pipefail

args=$1 out=$2 err=$3 code=$4
summary=${GITHUB_STEP_SUMMARY:-/dev/null}

# jq helper: escape a string for a workflow command message or property.
esc='def esc: gsub("%"; "%25") | gsub("\r"; "%0D") | gsub("\n"; "%0A");
     def prop: esc | gsub(":"; "%3A") | gsub(","; "%2C");'
# jq helper: make a string safe inside a markdown table cell.
cell='def cell: gsub("\\|"; "\\|") | gsub("\n"; " ");'

{
  echo "### \`quiver $args\`"
  echo
} >> "$summary"

# Command-level error (stderr is {"error": {...}} under --json).
if [ -s "$err" ] && jq -e '.error' "$err" > /dev/null 2>&1; then
  jq -r "$esc"' "::error title=quiver (\(.error.kind))::\(.error.message | esc)"' "$err"
  jq -r '"**\(.error.kind)** (exit \(.error.code)): \(.error.message)\n"' "$err" >> "$summary"
  if [ "$(jq -r .error.code "$err")" = 7 ]; then
    printf '%s\n\n' "Content changed in Quiver since these files were pulled. Run \`quiver content pull\` locally, merge, and push again." >> "$summary"
  fi
elif [ -s "$err" ]; then
  cat "$err" >&2
fi

[ -s "$out" ] || exit 0

# align tab-separated columns when column(1) is available.
align() { if command -v column > /dev/null; then column -t -s $'\t'; else cat; fi; }

case "$args" in
content\ push*)
  jq -r "$esc"'.[] | .path as $p |
    (select(.action == "error") | "::error file=\($p | prop),title=quiver push::\(.error | esc)"),
    (.issues // [] | .[] | select(.severity == "error") | "::error file=\($p | prop),title=\(.field | prop)::\(.message | esc)")
  ' "$out"
  jq -r '.[] | "\(.action | ascii_upcase)\t\(.path)\t\(.changed // [] | join(", "))\t\(.error // "")"' "$out" | align
  {
    echo "| File | Result | Changed |"
    echo "|---|---|---|"
    jq -r "$cell"'.[] | "| `\(.path)` | \(if .action == "error" then "❌ \(.error | cell)" else .action end) | \(.changed // [] | join(", ")) |"' "$out"
    echo
    jq -r 'group_by(.action) | map("\(length) \(.[0].action)") | join(" · ")' "$out"
  } >> "$summary"
  ;;
content\ check*)
  jq -r "$esc"'.[] | .path as $p | .issues[] |
    "::\(if .severity == "error" then "error" else "warning" end) file=\($p | prop),title=\(.field | prop)::\(.message | esc)"' "$out"
  jq -r '.[] | "\(if .ok then "ok  " else "FAIL" end)  \(.path)" , (.issues[] | "      \(.severity)  \(.field): \(.message)")' "$out"
  {
    echo "| File | Result | Issues |"
    echo "|---|---|---|"
    jq -r "$cell"'.[] | "| `\(.path)` | \(if .ok then "✅" else "❌" end) | \(.issues | map("\(.severity): `\(.field)` \(.message | cell)") | join("<br>")) |"' "$out"
    echo
  } >> "$summary"
  ;;
*)
  cat "$out"
  {
    echo '```json'
    head -c 60000 "$out"
    echo
    echo '```'
  } >> "$summary"
  ;;
esac

exit 0
