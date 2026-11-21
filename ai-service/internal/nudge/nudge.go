// Package nudge sends one follow-up message to a customer who went quiet
// after the bot gave a price somewhere in the chat: the bot still owns the
// chat and no staff member is assigned or wrote in it, a bot reply was the
// last message, and the customer's last message is about 20 hours old, so
// the follow-up lands inside Messenger's 24-hour reply window.
//
// The conversation gets the label "nudged" before the message is sent, and
// only the sweep that added the label sends, so a chat is followed up at
// most once, even if sending fails. (Chatwoot has no compare-and-set for
// labels; two sweepers against one Chatwoot in the same second could both
// send. Production runs one ai-service container.) Auto-resolve
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

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/ack"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/schedule"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/spend"
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
	Assigned     bool // a staff member (not the bot) is the assignee
	Labels       []string
	LastActivity time.Time
	// For a follow-up picture: where the chat came in and who the customer
	// is, so their previous chat can be checked for pictures they already got.
	InboxID      int64
	ContactID    int64
	Channel      string // e.g. "Channel::FacebookPage"
	HmacVerified bool   // website widget: the site signed the visitor's identity
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
	if c.Status != "pending" || c.Muted || c.Assigned || slices.Contains(c.Labels, Label) || slices.Contains(c.Labels, handoffLabel) {
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

// lastPublic is the newest message the customer could see or wrote, looking
// past a customer's like, sticker (no text) or thanks: the bot does not
// answer those (internal/ack), so the bot's reply before them counts.
func lastPublic(msgs []gaps.Message) (gaps.Message, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Private || !(m.Incoming || m.Outgoing) {
			continue
		}
		if m.Incoming && (strings.TrimSpace(m.Content) == "" || ack.IsAck(m.Content)) {
			continue
		}
		return m, true
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
	// ChatMessages returns the conversation's latest messages (a few pages),
	// oldest first.
	ChatMessages(ctx context.Context, accountID, conversationID int64) ([]gaps.Message, error)
	ConversationStatus(ctx context.Context, accountID, conversationID int64) (string, error)
	// Assignee returns the staff member the chat is assigned to, or 0.
	Assignee(ctx context.Context, accountID, conversationID int64) (int64, error)
	// AddLabelIfMissing reports false when the chat already had the label.
	AddLabelIfMissing(ctx context.Context, accountID, conversationID int64, label string) (bool, error)
	SendMessage(ctx context.Context, accountID, conversationID int64, content string) error
}

// Pictures sends one review picture after a follow-up's text (bot.Bot): the
// first one ticked "use in follow-ups" that the customer has not had. It
// reports whether one went out.
type Pictures interface {
	FollowUpPicture(ctx context.Context, acct int64, c Conversation) (bool, error)
}

// Hours is the bot's on/off switch per account; nil means always on.
type Hours interface {
	Get(ctx context.Context, accountID int64) (schedule.Schedule, error)
}

// Sweeper sends due follow-ups on every Sweep.
type Sweeper struct {
	List   Lister
	Chat   Chat
	Hours  Hours
	Rules  Rules  // each account's setting, read on every sweep
	Writer Writer // drafts a follow-up when the rule asks; nil = always the fixed text
	// Pictures adds a ticked review picture after the text; nil = text only.
	Pictures Pictures
	Now      func() time.Time
	Logger   *slog.Logger
}

// Rules gives an account's follow-up rule (SettingsStore).
type Rules interface {
	For(acct int64) Rule
}

// Sweep sends every due follow-up once and returns how many it sent.
// Errors are logged; the next sweep tries again.
func (s *Sweeper) Sweep(ctx context.Context) int {
	now := s.Now()
	accounts, err := s.List.Accounts(ctx)
	if err != nil {
		s.Logger.Warn("nudge: list accounts", "error", err)
		return 0
	}
	total := 0
	for _, acct := range accounts {
		rule := s.Rules.For(acct)
		if !rule.Active() || !s.botOn(ctx, acct, now) {
			continue
		}
		total += s.sweepAccount(ctx, acct, rule, now)
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
func (s *Sweeper) sweepAccount(ctx context.Context, acct int64, rule Rule, now time.Time) int {
	ctx = spend.WithAccount(ctx, acct) // AI-written follow-ups count against this business
	convs, err := s.List.PendingBetween(ctx, acct, now.Add(-window), now.Add(-rule.After))
	if err != nil {
		s.Logger.Warn("nudge: list conversations", "account_id", acct, "error", err)
	}
	n := 0
	for _, c := range convs {
		if ctx.Err() != nil {
			return n
		}
		if s.nudge(ctx, acct, c, rule, now) {
			n++
		}
	}
	return n
}

func (s *Sweeper) nudge(ctx context.Context, acct int64, c Conversation, rule Rule, now time.Time) bool {
	log := s.Logger.With("account_id", acct, "conversation_id", c.ID)
	msgs, err := s.Chat.ChatMessages(ctx, acct, c.ID)
	if err != nil {
		log.Warn("nudge: read messages", "error", err)
		return false
	}
	if !Due(c, msgs, now, rule.After) {
		return false
	}
	if !s.stillBots(ctx, acct, c.ID, log) {
		return false
	}
	// Label first: a chat is nudged at most once, even if sending fails, and
	// only by the sweep that added the label.
	added, err := s.Chat.AddLabelIfMissing(ctx, acct, c.ID, Label)
	if err != nil {
		log.Warn("nudge: add label", "error", err)
		return false
	}
	if !added {
		return false
	}
	text, written := s.message(ctx, rule, msgs)
	if err := s.Chat.SendMessage(ctx, acct, c.ID, text); err != nil {
		log.Warn("nudge: send", "error", err)
		return false
	}
	log.Info("nudge: sent", "written", written, "picture", s.picture(ctx, acct, c, log))
	return true
}

// picture sends the follow-up's picture, if any. A failure is only logged:
// the text already went out and counts, and a picture is not retried.
func (s *Sweeper) picture(ctx context.Context, acct int64, c Conversation, log *slog.Logger) bool {
	if s.Pictures == nil {
		return false
	}
	sent, err := s.Pictures.FollowUpPicture(ctx, acct, c)
	if err != nil {
		log.Warn("nudge: follow-up picture failed", "error", err)
		return false
	}
	return sent
}

// stillBots re-reads the chat just before sending: staff may have taken or
// been assigned it since it was listed.
func (s *Sweeper) stillBots(ctx context.Context, acct, conv int64, log *slog.Logger) bool {
	status, err := s.Chat.ConversationStatus(ctx, acct, conv)
	if err != nil {
		log.Warn("nudge: read status", "error", err)
		return false
	}
	if status != "pending" {
		return false
	}
	staff, err := s.Chat.Assignee(ctx, acct, conv)
	if err != nil {
		log.Warn("nudge: read assignee", "error", err)
		return false
	}
	return staff == 0
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
