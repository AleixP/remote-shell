#!/usr/bin/env bash
# Builds the agent for every supported platform and packs each build with the
# license and README, plus a SHA256SUMS file.
#
#   scripts/build-release.sh 1.2.3        # writes dist/
#
# The agent is pure Go (SQLite without cgo), so every target cross-compiles
# from any machine.
set -euo pipefail

VERSION="${1:?usage: scripts/build-release.sh <version>}"
VERSION="${VERSION#v}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/dist"
PKG="github.com/AleixP/remote-shell/agent/internal/server.Version"
TARGETS="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64"

rm -rf "$OUT"
mkdir -p "$OUT"

for target in $TARGETS; do
  os="${target%/*}"
  arch="${target#*/}"
  name="remote-shell_${VERSION}_${os}_${arch}"
  stage="$(mktemp -d)"
  bin="remote-shell"
  [ "$os" = "windows" ] && bin="remote-shell.exe"

  echo "building $name"
  (cd "$ROOT/agent" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags "-s -w -X ${PKG}=${VERSION}" -o "$stage/$bin" ./cmd/remote-shell)
  cp "$ROOT/LICENSE" "$ROOT/README.md" "$stage/"

  if [ "$os" = "windows" ]; then
    (cd "$stage" && zip -q "$OUT/$name.zip" "$bin" LICENSE README.md)
  else
    tar -C "$stage" -czf "$OUT/$name.tar.gz" "$bin" LICENSE README.md
  fi
  rm -rf "$stage"
done

(cd "$OUT" && shasum -a 256 remote-shell_* > SHA256SUMS)
echo "done:"
ls -1 "$OUT"
