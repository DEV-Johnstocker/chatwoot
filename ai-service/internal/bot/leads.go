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

// ChatLinker records which Messenger customer a chat is with
// (referral.Postgres).
type ChatLinker interface {
	LinkChat(ctx context.Context, accountID, conversationID, inboxID int64, psid string) error
}

// linkChat keeps the customer's Page-scoped id for a Messenger chat, which
// Chatwoot's webhook carries, so the Leads and Sales pages can show the ad
// or m.me link the customer came from. Only customer messages on Facebook
// count, whether or not the bot answers them (bot off, staff on the chat).
// A linked chat whose customer tapped an ad is then marked in Chatwoot
// (markAd). Best effort: a failure is logged (without the id) and the reply
// goes on.
func (b *Bot) linkChat(ctx context.Context, log *slog.Logger, ev webhook.Event) {
	psid := ev.Conversation.ContactInbox.SourceID
	if b.opts.ChatLinks == nil || ev.Conversation.Channel != "Channel::FacebookPage" || psid == "" {
		return
	}
	if ev.MessageType != "incoming" || (ev.Sender.Type != "" && ev.Sender.Type != "contact") {
		return
	}
	if err := b.opts.ChatLinks.LinkChat(ctx, ev.Account.ID, ev.ConversationID(), ev.Inbox.ID, psid); err != nil {
		log.Warn("chat link not recorded", "error", err)
		return
	}
	b.markAd(ctx, log, ev.Account.ID, ev.ConversationID())
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
