package chatwoot

import (
	"context"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/nudge"
)

// PendingBetween lists the account's pending (bot-owned) conversations whose
// last activity lies in [from, to], oldest first.
func (r *Reader) PendingBetween(ctx context.Context, accountID int64, from, to time.Time) ([]nudge.Conversation, error) {
	var out []nudge.Conversation
	err := r.walkPending(ctx, accountID, func(l listedConversation) bool {
		at := time.Unix(l.LastActivityAt, 0)
		if at.After(to) {
			return false // sorted oldest first: the rest are newer
		}
		if !at.Before(from) {
			out = append(out, nudge.Conversation{
				ID: l.ID, Status: l.Status, Muted: l.Muted, Labels: l.Labels, LastActivity: at,
			})
		}
		return true
	})
	return out, err
}
