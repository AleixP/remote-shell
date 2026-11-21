#!/usr/bin/env bash
# Prints the CHANGELOG.md section of a version, to use as the release notes.
#
#   scripts/release-notes.sh 1.2.3
#
# Fails when the version has no section or it is empty, so a release cannot go
# out with blank notes.
set -euo pipefail

VERSION="${1:?usage: scripts/release-notes.sh <version>}"
VERSION="${VERSION#v}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

notes="$(awk -v v="$VERSION" '
  /^## \[/ {
    if (found) exit
    if (index($0, "## [" v "]") == 1) { found = 1; next }
  }
  # Link reference definitions at the end of the file are not part of the notes.
  found && /^\[[^]]+\]: / { next }
  found { print }
' "$ROOT/CHANGELOG.md")"

# Trim blank lines at both ends.
notes="$(printf '%s\n' "$notes" | sed -e '/./,$!d' | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}')"

if [ -z "$notes" ]; then
  echo "error: CHANGELOG.md has no notes for version $VERSION (expected a '## [$VERSION]' section)" >&2
  exit 1
fi
printf '%s\n' "$notes"
