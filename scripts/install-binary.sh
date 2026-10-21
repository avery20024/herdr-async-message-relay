#!/bin/sh
# 安裝固定版本的預編譯成品；不需要 Go，也不自動啟動服務。
set -eu
fail() { printf 'hrelay installer: %s\n' "$*" >&2; exit 1; }
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
VERSION=$(tr -d '\r\n' < "$ROOT/VERSION")
printf '%s\n' "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$' || fail 'invalid VERSION'
case "$(uname -s)" in
    Darwin) OS=darwin ;;
    Linux) OS=linux ;;
    *) fail 'supported systems: macOS and Linux' ;;
esac
case "$(uname -m)" in
    arm64|aarch64) ARCH=arm64 ;;
    x86_64|amd64) ARCH=amd64 ;;
    *) fail 'supported architectures: arm64 and amd64' ;;
esac
for tool in curl tar awk mktemp; do
    command -v "$tool" >/dev/null 2>&1 || fail "missing dependency: $tool"
done
if command -v sha256sum >/dev/null 2>&1; then HASH=sha256sum
elif command -v shasum >/dev/null 2>&1; then HASH=shasum
else fail 'requires sha256sum or shasum'; fi
ASSET="hrelay_${OS}_${ARCH}.tar.gz"
BASE="https://github.com/avery20024/herdr-async-message-relay/releases/download/$VERSION"
WORK=$(mktemp -d "${TMPDIR:-/tmp}/hrelay-install.XXXXXX")
STAGED=''
cleanup() { rm -rf -- "$WORK"; if [ -n "$STAGED" ]; then rm -f -- "$STAGED"; fi; }
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
fetch() {
    curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
        --tlsv1.2 --connect-timeout 10 --max-time 120 --retry 2 --output "$2" "$1"
}
fetch "$BASE/checksums.txt" "$WORK/checksums.txt" || fail 'cannot download checksums for pinned version'
fetch "$BASE/$ASSET" "$WORK/$ASSET" || fail 'cannot download binary for this platform'
EXPECTED=$(awk -v name="$ASSET" '$2 == name { print $1; count++ } END { if (count != 1) exit 1 }' "$WORK/checksums.txt") || fail 'checksum entry missing or ambiguous'
printf '%s\n' "$EXPECTED" | grep -Eq '^[0-9a-f]{64}$' || fail 'invalid SHA-256 entry'
if [ "$HASH" = sha256sum ]; then ACTUAL=$(sha256sum "$WORK/$ASSET" | awk '{print $1}')
else ACTUAL=$(shasum -a 256 "$WORK/$ASSET" | awk '{print $1}'); fi
[ "$ACTUAL" = "$EXPECTED" ] || fail 'SHA-256 mismatch; existing binary was not replaced'
# 只接受發布腳本產生的四個普通檔案，拒絕路徑跳脫、重複成員或連結。
tar -tzf "$WORK/$ASSET" > "$WORK/members" || fail 'invalid archive'
awk '($0 != "hrelay" && $0 != "LICENSE" && $0 != "README.md" && $0 != "THIRD_PARTY_NOTICES.txt") || seen[$0]++ { exit 1 } END { if (NR != 4) exit 1 }' "$WORK/members" || fail 'unexpected archive members'
tar -tvzf "$WORK/$ASSET" > "$WORK/types" || fail 'cannot inspect archive'
awk 'substr($0,1,1) != "-" { exit 1 }' "$WORK/types" || fail 'archive contains non-regular files'
mkdir "$WORK/unpacked"
tar -xzf "$WORK/$ASSET" -C "$WORK/unpacked"
[ -f "$WORK/unpacked/hrelay" ] && [ ! -L "$WORK/unpacked/hrelay" ] || fail 'binary is not a regular file'
chmod 755 "$WORK/unpacked/hrelay"
[ "$("$WORK/unpacked/hrelay" version)" = "hrelay $VERSION" ] || fail 'binary version does not match plugin version'
mkdir -p "$ROOT/bin"
STAGED=$(mktemp "$ROOT/bin/.hrelay.XXXXXX")
cp "$WORK/unpacked/hrelay" "$STAGED"
chmod 755 "$STAGED"
cp "$WORK/unpacked/THIRD_PARTY_NOTICES.txt" "$ROOT/bin/THIRD_PARTY_NOTICES.txt"
mv -f "$STAGED" "$ROOT/bin/hrelay"
STAGED=''
printf 'Installed hrelay %s (%s/%s)\n' "$VERSION" "$OS" "$ARCH"
