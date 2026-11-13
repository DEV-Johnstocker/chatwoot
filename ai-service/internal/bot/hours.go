package bot

import (
	"context"
	"log/slog"

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
