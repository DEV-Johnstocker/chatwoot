package bot

import (
	"context"
	"log/slog"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/leads"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/webhook"
)

// LeadRecorder keeps the owner's lead list (leads.Postgres).
type LeadRecorder interface {
	Customer(ctx context.Context, c leads.Contact, asked bool) error
	Replied(ctx context.Context, accountID, conversationID int64, interest string, quoted bool) error
}

// noteCustomer records a customer message for the lead list: a question
// about price, buying or a promotion makes the chat a lead, and any later
// message updates the lead's last contact. Best effort: a failure is logged
// (without the name or text) and the reply goes on.
func (b *Bot) noteCustomer(ctx context.Context, log *slog.Logger, ev webhook.Event, text string) {
	if b.opts.Leads == nil {
		return
	}
	c := leads.Contact{AccountID: ev.Account.ID, ConversationID: ev.ConversationID(), InboxID: ev.Inbox.ID}
	if ev.Sender.Type == "" || ev.Sender.Type == "contact" {
		c.ContactID, c.Name = ev.Sender.ID, ev.Sender.Name
	}
	if err := b.opts.Leads.Customer(ctx, c, leads.AsksPrice(text)); err != nil {
		log.Warn("lead not recorded", "error", err)
	}
}

// noteReply records a bot reply in a lead's chat: what it was about (the
// best-matching knowledge document) and whether it gave a price or link.
// Only a turn about buying (the customer asked, or the reply quoted) updates
// the interest, so a later "thank you" does not overwrite it.
func (b *Bot) noteReply(ctx context.Context, log *slog.Logger, acct, conv int64, history []memory.Turn, hits []knowledge.Hit, reply string) {
	if b.opts.Leads == nil {
		return
	}
	quoted := leads.GivesPrice(reply)
	if !quoted && !leads.AsksPrice(lastCustomerTurn(history)) {
		return
	}
	interest := ""
	if len(hits) > 0 {
		interest = hits[0].Title
	}
	if err := b.opts.Leads.Replied(ctx, acct, conv, interest, quoted); err != nil {
		log.Warn("lead reply not recorded", "error", err)
	}
}

// lastCustomerTurn is the newest customer message in history.
func lastCustomerTurn(history []memory.Turn) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" {
			return history[i].Content
		}
	}
	return ""
}
