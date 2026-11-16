package deliver

import (
	"context"

	"github.com/avery20024/herdr-async-message-relay/internal/sqlc"
)

// QueueBlock 是投遞閘門的快照，不改變訊息的持久狀態或宣稱座位已可接收。
type QueueBlock struct {
	Recipient string `json:"recipient"`
	Reason    string `json:"reason"`
	BatchID   string `json:"batch_id"`
	Detail    string `json:"detail"`
}

func queueBlocks(ctx context.Context, c Config, q *sqlc.Queries) ([]QueueBlock, error) {
	blocks := []QueueBlock{}
	// legacy worker 會退避重試，不冒稱 uncertain 一定阻塞後續投遞。
	if !c.IsCore() {
		return blocks, nil
	}
	rows, err := q.UncertainQueueBlocks(ctx)
	if err != nil {
		return nil, err
	}
	for _, b := range rows {
		blocks = append(blocks, QueueBlock{Recipient: b.Recipient, Reason: "uncertain_requires_resolution", BatchID: b.ID, Detail: b.Detail})
	}
	return blocks, nil
}
