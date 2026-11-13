package bot

import (
	"context"
	"log/slog"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/schedule"
)

// Hours reads each account's main switch and weekly hours (schedule.Store).
type Hours interface {
	Get(ctx context.Context, accountID int64) (schedule.Schedule, error)
}

// ReasonBotOff: the owner switched the bot off, or the message came outside
// the bot's hours. The chat goes straight to staff and the bot sends nothing.
const ReasonBotOff = "ai-handoff:bot-off"

// botOn reports whether the bot answers this account now. Without an Hours
// store it is always on; if the switch cannot be read it stays on too, so a
// database hiccup never leaves customers unanswered by both bot and staff.
func (b *Bot) botOn(ctx context.Context, log *slog.Logger, acct int64) bool {
	if b.opts.Hours == nil {
		return true
	}
	s, err := b.opts.Hours.Get(ctx, acct)
	if err != nil {
		log.Warn("bot hours unreadable: the bot stays on", "error", err)
		return true
	}
	return s.Active(b.now())
}

// botOffNoteFor is how long a bot-off handoff covers its chat: messages that
// arrive before Chatwoot shows the chat as open (a quick burst, or a handoff
// Chatwoot refused) do not post a second note or route it twice.
const botOffNoteFor = 10 * time.Minute

// firstBotOff reports whether this chat has no bot-off handoff in the last
// botOffNoteFor, and records one now.
func (b *Bot) firstBotOff(acct, conv int64) bool {
	now := b.now()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.offSent == nil {
		b.offSent = map[convKey]time.Time{}
	}
	for k, at := range b.offSent { // a few entries at most: drop old ones
		if now.Sub(at) >= botOffNoteFor {
			delete(b.offSent, k)
		}
	}
	key := convKey{acct, conv}
	if _, done := b.offSent[key]; done {
		return false
	}
	b.offSent[key] = now
	return true
}

// handOffBotOff passes a chat to staff because the bot is off: once per chat
// (see firstBotOff); later messages are only logged.
func (b *Bot) handOffBotOff(ctx context.Context, log *slog.Logger, acct, inbox, conv int64) {
	if !b.firstBotOff(acct, conv) {
		b.cancelPending(acct, conv)
		log.Info("bot is off: chat already passed to staff")
		return
	}
	b.handoff(ctx, log, acct, inbox, conv, "", ReasonBotOff, nil)
}
