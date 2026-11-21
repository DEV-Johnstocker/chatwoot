package chatwoot

import (
	"context"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/nudge"
)

// PendingBetween lists the account's pending (bot-owned) conversations whose
// last activity lies in [from, to], newest first. Walking newest first keeps
// a backlog of old pending chats from using up the page budget.
func (r *Reader) PendingBetween(ctx context.Context, accountID int64, from, to time.Time) ([]nudge.Conversation, error) {
	var out []nudge.Conversation
	err := r.walkPending(ctx, accountID, "last_activity_at_desc", func(l listedConversation) bool {
		at := time.Unix(l.LastActivityAt, 0)
		if at.Before(from) {
			return false // sorted newest first: the rest are older
		}
		if !at.After(to) {
			out = append(out, nudge.Conversation{
				ID: l.ID, Status: l.Status, Muted: l.Muted, Assigned: l.assigned(),
				Labels: l.Labels, LastActivity: at,
				InboxID: l.InboxID, ContactID: l.Meta.Sender.ID, Channel: l.Meta.Channel, HmacVerified: l.Meta.HmacVerified,
			})
		}
		return true
	})
	return out, err
}
