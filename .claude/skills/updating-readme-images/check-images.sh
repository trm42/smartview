#!/usr/bin/env bash
# Cross-check docs/images against README.md image references.
# Exits non-zero if any reference dangles or any image is unreferenced.
set -uo pipefail

cd "$(git rev-parse --show-toplevel)"

status=0

# Every docs/images path the README links to.
grep -o 'docs/images/[A-Za-z0-9._-]*' README.md | sort -u > /tmp/sv-refs.txt
ls docs/images/*.png | sort > /tmp/sv-files.txt

while read -r ref; do
  [ -f "$ref" ] || { echo "DANGLING  README links $ref, which does not exist"; status=1; }
done < /tmp/sv-refs.txt

while read -r f; do
  grep -qx "$f" /tmp/sv-refs.txt || { echo "ORPHAN    $f is not referenced by README.md"; status=1; }
done < /tmp/sv-files.txt

# The set is only consistent if every image shares one geometry.
sizes=$(magick identify -format '%wx%h\n' docs/images/*.png | sort -u)
if [ "$(echo "$sizes" | wc -l)" -gt 1 ]; then
  echo "MIXED     images have differing sizes:"; echo "$sizes" | sed 's/^/          /'
  status=1
fi

[ $status -eq 0 ] && echo "OK        $(wc -l < /tmp/sv-files.txt) images, all referenced, all $sizes"
rm -f /tmp/sv-refs.txt /tmp/sv-files.txt
exit $status
