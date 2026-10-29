package deliver

import (
	"context"
	"errors"
	"net"
	"syscall"
	"time"
)

// ExitWhenHerdrGoneEnv 設為 1 時，serve 會在 Herdr socket 持續消失後正常結束。
const ExitWhenHerdrGoneEnv = "HERDR_RELAY_EXIT_WHEN_HERDR_GONE"

// WatchHerdr 定期連線 Herdr socket；只有確定沒有 listener（不存在或拒絕連線）才累計，
// 連續 misses 次後呼叫 gone。逾時或權限錯誤不算消失，避免誤停。
func WatchHerdr(ctx context.Context, socket string, interval time.Duration, misses int, gone func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	count := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", socket)
		switch {
		case err == nil:
			conn.Close()
			count = 0
		case errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED):
			if count++; count >= misses {
				gone()
				return
			}
		}
	}
}
