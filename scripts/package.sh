#!/bin/sh
# 使用固定版本與可重現路徑設定，產出單一平台的 Release 壓縮檔。
set -eu
[ "$#" -eq 2 ] || { echo 'usage: package.sh <darwin|linux> <arm64|amd64>' >&2; exit 1; }
case "$1/$2" in darwin/arm64|darwin/amd64|linux/arm64|linux/amd64) ;; *) echo 'unsupported platform' >&2; exit 1 ;; esac
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
VERSION=$(tr -d '\r\n' < "$ROOT/VERSION")
printf '%s\n' "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$' || { echo 'invalid VERSION' >&2; exit 1; }
WORK=$(mktemp -d "${TMPDIR:-/tmp}/hrelay-package.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
cd "$ROOT"
CGO_ENABLED=0 GOOS="$1" GOARCH="$2" go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$WORK/hrelay" ./cmd/hrelay
cp LICENSE README.md "$WORK/"
GOOS="$1" GOARCH="$2" CGO_ENABLED=0 python3 scripts/notices.py > "$WORK/THIRD_PARTY_NOTICES.txt"
mkdir -p dist
tar -czf "$ROOT/dist/hrelay_$1_$2.tar.gz" -C "$WORK" hrelay LICENSE README.md THIRD_PARTY_NOTICES.txt
printf 'Packaged %s for %s/%s\n' "$VERSION" "$1" "$2"
