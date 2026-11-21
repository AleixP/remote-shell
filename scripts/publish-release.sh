#!/usr/bin/env bash
# Publishes the archives in dist/ as the GitHub release of a tag.
#
#   scripts/publish-release.sh v1.2.3
#
# It works whether or not the release already exists. Creating the release by
# hand on the web also creates the tag, which starts the release workflow: in
# that case the files are added to the existing release, and the notes are only
# filled in when it has none, so notes typed by hand are never overwritten.
# Versions below 1.0.0, and any with a suffix such as -rc.1, are pre-releases.
set -euo pipefail

TAG="${1:?usage: scripts/publish-release.sh <tag>}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
NOTES="${RELEASE_NOTES:-$ROOT/release-notes.md}"

prerelease=""
case "${TAG#v}" in
  0.* | *-*) prerelease="--prerelease" ;;
esac

if gh release view "$TAG" >/dev/null 2>&1; then
  echo "release $TAG already exists: adding the files to it"
  gh release upload "$TAG" "$ROOT"/dist/* --clobber
  if [ -z "$(gh release view "$TAG" --json body --jq .body)" ]; then
    gh release edit "$TAG" --notes-file "$NOTES"
  fi
  if [ -n "$prerelease" ]; then
    gh release edit "$TAG" --prerelease
  fi
else
  gh release create "$TAG" "$ROOT"/dist/* --title "$TAG" --notes-file "$NOTES" $prerelease
fi
