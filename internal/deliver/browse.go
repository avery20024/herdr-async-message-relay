package deliver

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/avery20024/herdr-async-message-relay/internal/sqlc"
)

// BrowseResponse 是唯讀介面快照；批次時間為證據，不宣稱訊息已讀。
type BrowseResponse struct {
	ProjectRoot string                   `json:"project_root"`
	Items       []sqlc.BrowseMessagesRow `json:"items"`
	NextBefore  int64                    `json:"next_before"`
	Senders     []string                 `json:"senders"`
	Recipients  []string                 `json:"recipients"`
}

func (a API) browseMessages(w http.ResponseWriter, r *http.Request) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		apiError(w, 400, "invalid query parameters")
		return
	}
	for key, entries := range values {
		if len(entries) != 1 {
			apiError(w, 400, "duplicate query parameter")
			return
		}
		switch key {
		case "before", "limit", "sender", "recipient", "state":
		default:
			apiError(w, 400, "unknown query parameter")
			return
		}
	}
	before := int64(0)
	if raw, ok := values["before"]; ok {
		before, err = strconv.ParseInt(raw[0], 10, 64)
		if err != nil || before <= 0 {
			apiError(w, 400, "before must be a positive sequence")
			return
		}
	}
	limit := int64(50)
	if raw, ok := values["limit"]; ok {
		limit, err = strconv.ParseInt(raw[0], 10, 64)
		if err != nil || limit < 1 || limit > 100 {
			apiError(w, 400, "limit must be between 1 and 100")
			return
		}
	}
	state := values.Get("state")
	switch state {
	case "", "queued", "dispatching", "submitted", "uncertain":
	default:
		apiError(w, 400, "invalid message state")
		return
	}
	// 同一讀取交易保持頁面與歷史候選一致，不新增或修改任何資料。
	tx, err := a.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		apiError(w, 500, "unable to read messages")
		return
	}
	defer tx.Rollback()
	q := a.Store.Q.WithTx(tx)
	rows, err := q.BrowseMessages(r.Context(), sqlc.BrowseMessagesParams{
		BeforeSeq: before, SenderFilter: values.Get("sender"), RecipientFilter: values.Get("recipient"), StateFilter: state, PageLimit: limit + 1,
	})
	if err != nil {
		apiError(w, 500, "unable to read messages")
		return
	}
	next := int64(0)
	if int64(len(rows)) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1].Seq
	}
	senders, err := q.HistoricalSenders(r.Context())
	if err != nil {
		apiError(w, 500, "unable to read sender choices")
		return
	}
	recipients, err := q.HistoricalRecipients(r.Context())
	if err != nil {
		apiError(w, 500, "unable to read recipient choices")
		return
	}
	if err := tx.Commit(); err != nil {
		apiError(w, 500, "unable to finish message snapshot")
		return
	}
	respond(w, 200, BrowseResponse{ProjectRoot: a.Config.Root, Items: rows, NextBefore: next, Senders: senders, Recipients: recipients})
}
