package bot

import (
	"context"
	"log/slog"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/sales"
)

// SlipRecorder keeps a payment slip the bot saw (sales.Store).
// Issue: DEV-Johnstocker/chatwoot#1
type SlipRecorder interface {
	RecordSlip(ctx context.Context, s sales.Sale) error
}

// recordSlip saves a slip for the sales dashboard; a failure only loses the
// dashboard row, the chat still has its label and note.
func (b *Bot) recordSlip(ctx context.Context, log *slog.Logger, acct, inbox, conv int64, slip *slipSeen) {
	if b.opts.Sales == nil {
		return
	}
	at := time.Now()
	if b.opts.Now != nil {
		at = b.opts.Now()
	}
	if err := b.opts.Sales.RecordSlip(ctx, sales.Sale{AccountID: acct, ConversationID: conv, InboxID: max(inbox, 0),
		Status: sales.StatusSlip, SlipAmount: slip.amount, SlipAt: at}); err != nil {
		log.Error("payment slip not recorded", "error", err)
		return
	}
	log.Info("payment slip recorded", "has_amount", slip.amount != nil)
}
