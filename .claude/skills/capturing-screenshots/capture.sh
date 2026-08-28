#!/usr/bin/env bash
# Render smartview README screenshots from fixture data via vhs.
# Usage: .claude/skills/capturing-screenshots/capture.sh [view ...]   (default: all)
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"   # tape paths are repo-relative

TAPES=.claude/skills/capturing-screenshots/tapes
ALL=(overview attributes statistics farm tests logs fleet)
VIEWS=("${@:-}")
[ -z "${VIEWS[0]:-}" ] && VIEWS=("${ALL[@]}")

command -v vhs >/dev/null || { echo "vhs not found: brew install vhs" >&2; exit 1; }

# The -tags dev build is required: a release build rejects --fixtures at startup.
go build -tags dev -o smartview .

for v in "${VIEWS[@]}"; do
  [ -f "$TAPES/$v.tape" ] || { echo "no tape for view '$v'" >&2; exit 1; }
  printf '%-11s ' "$v"
  vhs "$TAPES/$v.tape" >/dev/null 2>&1
  # A blank or half-drawn frame still writes a valid PNG, so size is necessary
  # but not sufficient — read every image before calling the capture done.
  magick identify -format '%wx%h\n' "docs/images/$v.png"
done

rm -f /tmp/smartview-capture.gif
echo "Now READ each docs/images/*.png back. Size checks cannot see a blank frame."
