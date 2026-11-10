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

# The tapes count Down from the top of the drive list, which follows filename
# order, so they run on this pinned set: a contributed fixture cannot shift them.
FIXTURES=/tmp/smartview-capture-fixtures   # the tapes name this path
PINNED=(
  smart-860qvo smart-990pro smart-apple-nvme smart-hfs512 smart-nvme-errors
  smart-nvme smart-sda-errors smart-sda smart-sdb smart-sdc-failing
  smart-sdd-standby smart-sde-nodata smart-seagate-farm-log smart-sn730
  smart-st4000dm004 smart-st4000dm004-farm smart-wuh721414
)
rm -rf "$FIXTURES" && mkdir -p "$FIXTURES"
for f in "${PINNED[@]}"; do cp "internal/smart/testdata/$f.json" "$FIXTURES/"; done

for v in "${VIEWS[@]}"; do
  [ -f "$TAPES/$v.tape" ] || { echo "no tape for view '$v'" >&2; exit 1; }
  printf '%-11s ' "$v"
  vhs "$TAPES/$v.tape" >/dev/null 2>&1
  # A blank or half-drawn frame still writes a valid PNG, so size is necessary
  # but not sufficient — read every image before calling the capture done.
  magick identify -format '%wx%h\n' "docs/images/$v.png"
done

rm -rf /tmp/smartview-capture.gif "$FIXTURES"
echo "Now READ each docs/images/*.png back. Size checks cannot see a blank frame."
