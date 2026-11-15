package chatwoot

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
)

// The "Rate replies" page (/admin/replies) reads recent chats and their latest
// messages with the reader token. Issue: DEV-Johnstocker/chatwoot#1

// RecentChats returns one page (25) of the account's conversations in any
// status, the most recently active first.
func (r *Reader) RecentChats(ctx context.Context, accountID int64, page int) ([]gaps.Chat, error) {
	if page < 1 {
		page = 1
	}
	q := url.Values{
		"status": {"all"}, "assignee_type": {"all"},
		"sort_by": {"last_activity_at_desc"}, "page": {fmt.Sprint(page)},
	}
	var res struct {
		Data struct {
			Payload []struct {
				ID             int64 `json:"id"`
				InboxID        int64 `json:"inbox_id"`
				LastActivityAt int64 `json:"last_activity_at"`
			} `json:"payload"`
		} `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/accounts/%d/conversations?%s", accountID, q.Encode())
	if err := r.getJSON(ctx, path, nil, &res); err != nil {
		return nil, err
	}
	out := make([]gaps.Chat, 0, len(res.Data.Payload))
	for _, c := range res.Data.Payload {
		out = append(out, gaps.Chat{ID: c.ID, InboxID: c.InboxID, LastActivity: time.Unix(c.LastActivityAt, 0)})
	}
	return out, nil
}

// LatestMessages returns a conversation's newest page of messages (20),
// oldest first.
func (r *Reader) LatestMessages(ctx context.Context, accountID, conversationID int64) ([]gaps.Message, error) {
	var res struct {
		Payload []gapMessage `json:"payload"`
	}
	path := fmt.Sprintf("/api/v1/accounts/%d/conversations/%d/messages", accountID, conversationID)
	if err := r.getJSON(ctx, path, nil, &res); err != nil {
		return nil, err
	}
	out := make([]gaps.Message, 0, len(res.Payload))
	for _, m := range res.Payload {
		out = append(out, m.toMessage())
	}
	return out, nil
}
