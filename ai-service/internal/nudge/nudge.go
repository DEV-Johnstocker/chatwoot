// Package nudge sends one follow-up message to a customer who went quiet
// after the bot gave a price: the bot still owns the chat, staff never
// wrote in it, the bot's priced answer was the last message, and the
// customer's last message is about 20 hours old, so the follow-up lands
// inside Messenger's 24-hour reply window.
//
// The conversation gets the label "nudged" before the message is sent, so
// a chat is followed up at most once, even if sending fails. Auto-resolve
// (internal/autoresolve) counts quiet time from the bot's last message; the
// nudge is that last message, so a nudged chat resolves a full
// BOT_RESOLVE_AFTER later instead of being closed under it.
package nudge

import (
	"context"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/schedule"
)

// Label marks a conversation that already got its follow-up.
const Label = "nudged"

// handoffLabel is bot.HandoffLabel (not imported: bot depends on much more).
const handoffLabel = "bot-handoff"

// window is the latest moment after the customer's last message a nudge
// may go out: Messenger allows replies for 24 hours, and sweeps run every
// 15 minutes, so an hour of margin stays.
const window = 23 * time.Hour

// Conversation is the part of a listed Chatwoot conversation the decision needs.
type Conversation struct {
	ID           int64
	Status       string
	Muted        bool
	Labels       []string
	LastActivity time.Time
}

// price matches a baht amount: "9,900 บาท", "4900บาท", "฿1,290", "1,500 THB",
// "300 baht".
var price = regexp.MustCompile(`(?i)\d[\d,]*(\.\d+)?\s*(บาท|฿|thb\b|baht\b)|฿\s*\d`)

// HasPrice reports whether text quotes a baht amount.
func HasPrice(text string) bool {
	return price.MatchString(text)
}

// Due reports whether c should get its follow-up at now. msgs are the
// conversation's latest messages, oldest first; after is how long after the
// customer's last message the nudge goes out.
func Due(c Conversation, msgs []gaps.Message, now time.Time, after time.Duration) bool {
	if c.Status != "pending" || c.Muted || slices.Contains(c.Labels, Label) || slices.Contains(c.Labels, handoffLabel) {
		return false
	}
	last, ok := lastPublic(msgs)
	if !ok || !last.Outgoing || !last.FromBot {
		return false
	}
	asked, ok := lastIncoming(msgs)
	if !ok {
		return false
	}
	quiet := now.Sub(asked)
	if quiet < after || quiet >= window {
		return false
	}
	return !staffReplied(msgs) && botGavePrice(msgs)
}

// lastPublic is the newest message the customer could see or wrote.
func lastPublic(msgs []gaps.Message) (gaps.Message, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if m := msgs[i]; !m.Private && (m.Incoming || m.Outgoing) {
			return m, true
		}
	}
	return gaps.Message{}, false
}

func lastIncoming(msgs []gaps.Message) (time.Time, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Incoming {
			return msgs[i].At, true
		}
	}
	return time.Time{}, false
}

// staffReplied reports a reply or private note from a person, not the bot:
// staff stepped into the chat at some point.
func staffReplied(msgs []gaps.Message) bool {
	for _, m := range msgs {
		if m.Outgoing && !m.FromBot {
			return true
		}
	}
	return false
}

func botGavePrice(msgs []gaps.Message) bool {
	for _, m := range msgs {
		if m.Outgoing && !m.Private && m.FromBot && HasPrice(m.Content) {
			return true
		}
	}
	return false
}

// Lister finds the accounts and their bot-owned conversations.
type Lister interface {
	Accounts(ctx context.Context) ([]int64, error)
	// PendingBetween returns pending conversations whose last activity lies
	// in [from, to].
	PendingBetween(ctx context.Context, accountID int64, from, to time.Time) ([]Conversation, error)
}

// Chat reads a conversation and sends the follow-up.
type Chat interface {
	LatestMessages(ctx context.Context, accountID, conversationID int64) ([]gaps.Message, error)
	ConversationStatus(ctx context.Context, accountID, conversationID int64) (string, error)
	AddLabel(ctx context.Context, accountID, conversationID int64, label string) error
	SendMessage(ctx context.Context, accountID, conversationID int64, content string) error
}

// Hours is the bot's on/off switch per account; nil means always on.
type Hours interface {
	Get(ctx context.Context, accountID int64) (schedule.Schedule, error)
}

// Sweeper sends due follow-ups on every Sweep.
type Sweeper struct {
	List     Lister
	Chat     Chat
	Hours    Hours
	Accounts map[int64]bool // accounts with nudges on; empty = every account
	After    time.Duration  // time after the customer's last message; 0 = off
	Text     string         // the owner-approved follow-up; empty = off
	Now      func() time.Time
	Logger   *slog.Logger
}

// Sweep sends every due follow-up once and returns how many it sent.
// Errors are logged; the next sweep tries again.
func (s *Sweeper) Sweep(ctx context.Context) int {
	if s.After <= 0 || strings.TrimSpace(s.Text) == "" {
		return 0
	}
	now := s.Now()
	accounts, err := s.List.Accounts(ctx)
	if err != nil {
		s.Logger.Warn("nudge: list accounts", "error", err)
		return 0
	}
	total := 0
	for _, acct := range accounts {
		if len(s.Accounts) > 0 && !s.Accounts[acct] {
			continue
		}
		if !s.botOn(ctx, acct, now) {
			continue
		}
		total += s.sweepAccount(ctx, acct, now)
	}
	if total > 0 {
		s.Logger.Info("nudge: sent follow-ups", "count", total)
	}
	return total
}

func (s *Sweeper) botOn(ctx context.Context, acct int64, now time.Time) bool {
	if s.Hours == nil {
		return true
	}
	sc, err := s.Hours.Get(ctx, acct)
	if err != nil {
		s.Logger.Warn("nudge: read bot hours", "account_id", acct, "error", err)
		return false
	}
	return sc.Active(now)
}

// sweepAccount lists chats whose last activity (the bot's answer, seconds
// after the customer's message) falls in the nudge window.
func (s *Sweeper) sweepAccount(ctx context.Context, acct int64, now time.Time) int {
	convs, err := s.List.PendingBetween(ctx, acct, now.Add(-window), now.Add(-s.After))
	if err != nil {
		s.Logger.Warn("nudge: list conversations", "account_id", acct, "error", err)
	}
	n := 0
	for _, c := range convs {
		if ctx.Err() != nil {
			return n
		}
		if s.nudge(ctx, acct, c, now) {
			n++
		}
	}
	return n
}

func (s *Sweeper) nudge(ctx context.Context, acct int64, c Conversation, now time.Time) bool {
	log := s.Logger.With("account_id", acct, "conversation_id", c.ID)
	msgs, err := s.Chat.LatestMessages(ctx, acct, c.ID)
	if err != nil {
		log.Warn("nudge: read messages", "error", err)
		return false
	}
	if !Due(c, msgs, now, s.After) {
		return false
	}
	// Staff may have taken the chat since it was listed.
	if status, err := s.Chat.ConversationStatus(ctx, acct, c.ID); err != nil || status != "pending" {
		if err != nil {
			log.Warn("nudge: read status", "error", err)
		}
		return false
	}
	// Label first: a chat is nudged at most once, even if sending fails.
	if err := s.Chat.AddLabel(ctx, acct, c.ID, Label); err != nil {
		log.Warn("nudge: add label", "error", err)
		return false
	}
	if err := s.Chat.SendMessage(ctx, acct, c.ID, s.Text); err != nil {
		log.Warn("nudge: send", "error", err)
		return false
	}
	return true
}

// Run sweeps every interval until ctx ends.
func (s *Sweeper) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sweep(ctx)
		}
	}
}
