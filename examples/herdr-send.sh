#!/bin/bash
# 相容入口只負責入列，不以專案目錄名稱推測座位或發送身分。
set -euo pipefail

fail() { printf 'herdr-send: %s\n' "$*" >&2; exit 1; }
[[ $# -eq 2 && -n "$1" && -n "$2" ]] || fail 'Usage: herdr-send.sh <role> <message>'
TARGET_ROLE="$1"
MESSAGE="$2"
# 優先使用明確 root；未設定時，以範例腳本所在 scripts/ 的上一層為 root。
ROOT="${DELIVER_ROOT:-$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)}"
SENDER="${HERDR_SEND_FROM:-${HERDR_RELAY_FROM:-}}"
[[ -n "$SENDER" ]] || fail 'Set HERDR_SEND_FROM or HERDR_RELAY_FROM to the actual sender'
# 名稱由原生 CLI 統一驗證，避免 shell 語系限制多語角色；本文絕不 eval。
command -v hrelay >/dev/null || fail 'hrelay not found; install the plugin and check PATH; no binary or prompt fallback'
ARGS=(--root "$ROOT" send --from "$SENDER")
if [[ -n "${HERDR_SEND_IDEMPOTENCY_KEY:-}" ]]; then
    ARGS+=(--key "$HERDR_SEND_IDEMPOTENCY_KEY")
fi
ARGS+=("$TARGET_ROLE" "$MESSAGE")
# accepted 僅代表持久入列，不代表已讀或任務完成。
exec hrelay "${ARGS[@]}"
