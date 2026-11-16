package queueui

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// 只採終端回報的 Ms 能力，不讀剪貼簿、不僅憑 TERM 名稱猜測支援。
func supportsOSC52(capability string) bool {
	value, ok := strings.CutPrefix(capability, "Ms=")
	if !ok {
		_, value, ok = strings.Cut(capability, ";Ms=")
	}
	return ok && (strings.Contains(value, "\x1b]52;") || strings.Contains(value, `\E]52;`) || strings.Contains(value, `\033]52;`))
}

// 原文僅經 stdin 交給 pbcopy，不經 shell、不回顯輸入或子程序輸出。
func writePBCopy(ctx context.Context, value string) error {
	cmd := exec.CommandContext(ctx, "pbcopy")
	cmd.WaitDelay = 250 * time.Millisecond
	cmd.Stdin = strings.NewReader(value)
	return cmd.Run()
}
