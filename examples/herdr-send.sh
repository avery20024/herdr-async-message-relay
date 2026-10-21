#!/bin/bash
# 角色通知只負責入列；座位解析、等待與批次投遞由 herdr-async-message-relay daemon 處理。
set -euo pipefail

fail() { printf 'herdr-send: %s\n' "$*" >&2; exit 1; }
[[ $# -eq 2 && -n "$2" ]] || fail '用法：herdr-send.sh <角色> <訊息>'
TARGET_ROLE="$1"
MESSAGE="$2"
[[ "$TARGET_ROLE" =~ ^[a-z][a-z0-9-]*$ ]] || fail '角色僅允許小寫英文字母、數字與連字號'
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
[[ -d "$ROOT/角色/$TARGET_ROLE" ]] || fail "角色目錄不存在：$TARGET_ROLE"

SENDER="${HERDR_SEND_FROM:-}"
if [[ -z "$SENDER" ]]; then
    # 從角色目錄或其子目錄推導；切到根目錄執行時須明訂原角色，不猜測身分。
    CURRENT="$(pwd -P)"
    for ROLE_DIR in "$ROOT"/角色/*; do
        [[ -d "$ROLE_DIR" ]] || continue
        PHYSICAL="$(cd -- "$ROLE_DIR" && pwd -P)"
        if [[ "$CURRENT" == "$PHYSICAL" || "$CURRENT" == "$PHYSICAL/"* ]]; then
            [[ -z "$SENDER" ]] || fail '目前目錄對應多個角色，請設定 HERDR_SEND_FROM'
            SENDER="${ROLE_DIR##*/}"
        fi
    done
fi
[[ "$SENDER" =~ ^[a-z][a-z0-9-]*$ ]] || fail '無法確認發送角色；請設定 HERDR_SEND_FROM=<實際角色>'
[[ -d "$ROOT/角色/$SENDER" ]] || fail "發送角色目錄不存在：$SENDER"

# 只使用 plugin 安裝的原生指令；不探索 checkout，也不退回專案 binary 或舊覆寫變數。
command -v hrelay >/dev/null || fail '找不到 hrelay；請安裝 plugin v0.2+ 並確認座位 PATH，不會退回專案執行檔、msg 或 prompt'
ARGS=(--root "$ROOT" send --from "$SENDER")
if [[ -n "${HERDR_SEND_IDEMPOTENCY_KEY:-}" ]]; then
    ARGS+=(--key "$HERDR_SEND_IDEMPOTENCY_KEY")
fi
# 短語法預設 ensure；旗標必須置於角色與訊息之前，保留原文作為單一參數。
ARGS+=("$TARGET_ROLE" "$MESSAGE")
# accepted 只表示 SQLite 已入列，不代表目標已收到、已讀或驗收。
exec hrelay "${ARGS[@]}"
