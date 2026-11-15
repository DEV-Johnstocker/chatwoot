package chatwoot

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/autoresolve"
)

// maxIdlePages caps one listing (25 conversations a page), so a backlog is
// worked off over several sweeps instead of in one long run.
const maxIdlePages = 40

// Resolve marks a conversation resolved. For a bot-only conversation
// Chatwoot records a "bot resolved" event and sends the inbox's CSAT survey.
func (c *Client) Resolve(ctx context.Context, accountID, conversationID int64) error {
	return c.post(ctx, c.convPath(accountID, conversationID, "/toggle_status"), map[string]any{"status": "resolved"})
}

// Accounts lists the accounts the reader account belongs to.
func (r *Reader) Accounts(ctx context.Context) ([]int64, error) {
	var profile struct {
		Accounts []struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
		} `json:"accounts"`
	}
	if err := r.getJSON(ctx, "/api/v1/profile", nil, &profile); err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(profile.Accounts))
	for _, a := range profile.Accounts {
		if a.Status == "" || a.Status == "active" {
			ids = append(ids, a.ID)
		}
	}
	return ids, nil
}

type listedConversation struct {
	ID             int64    `json:"id"`
	Status         string   `json:"status"`
	Muted          bool     `json:"muted"`
	LastActivityAt int64    `json:"last_activity_at"`
	WaitingSince   int64    `json:"waiting_since"`
	Labels         []string `json:"labels"`
	Meta           struct {
		Assignee     *struct{} `json:"assignee"`
		AssigneeType string    `json:"assignee_type"`
	} `json:"meta"`
	LastNonActivity *struct {
		MessageType int  `json:"message_type"`
		Private     bool `json:"private"`
	} `json:"last_non_activity_message"`
}

// assigned reports a staff member (a user, not the agent bot) as assignee.
func (l listedConversation) assigned() bool {
	return l.Meta.Assignee != nil && l.Meta.AssigneeType == "User"
}

func (l listedConversation) toConversation() autoresolve.Conversation {
	c := autoresolve.Conversation{
		ID: l.ID, Status: l.Status, Muted: l.Muted,
		LastActivity: time.Unix(l.LastActivityAt, 0),
	}
	if l.WaitingSince > 0 {
		c.WaitingSince = time.Unix(l.WaitingSince, 0)
	}
	if m := l.LastNonActivity; m != nil {
		c.LastMessage = &autoresolve.Message{Outgoing: m.MessageType == 1, Private: m.Private}
	}
	return c
}

// IdlePending lists the account's pending (bot-owned) conversations with no
// activity after before, oldest first.
func (r *Reader) IdlePending(ctx context.Context, accountID int64, before time.Time) ([]autoresolve.Conversation, error) {
	var out []autoresolve.Conversation
	err := r.walkPending(ctx, accountID, "last_activity_at_asc", func(l listedConversation) bool {
		if time.Unix(l.LastActivityAt, 0).After(before) {
			return false // sorted oldest first: the rest are newer
		}
		out = append(out, l.toConversation())
		return true
	})
	return out, err
}

// walkPending calls fn for the account's pending conversations in sortBy
// order (last_activity_at_asc or _desc), until fn returns false or the pages
// run out (at most maxIdlePages).
func (r *Reader) walkPending(ctx context.Context, accountID int64, sortBy string, fn func(listedConversation) bool) error {
	for page := 1; page <= maxIdlePages; page++ {
		q := url.Values{
			"status": {"pending"}, "assignee_type": {"all"},
			"sort_by": {sortBy}, "page": {fmt.Sprint(page)},
		}
		var res struct {
			Data struct {
				Payload []listedConversation `json:"payload"`
			} `json:"data"`
		}
		path := fmt.Sprintf("/api/v1/accounts/%d/conversations?%s", accountID, q.Encode())
		if err := r.getJSON(ctx, path, nil, &res); err != nil {
			return err
		}
		for _, l := range res.Data.Payload {
			if !fn(l) {
				return nil
			}
		}
		if len(res.Data.Payload) == 0 {
			return nil
		}
	}
	return nil
}
