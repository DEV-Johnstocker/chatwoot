package bot

import (
	"context"
	"log/slog"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/webhook"
)

// Customers on Messenger often send several short messages in a row (or a
// photo, then its caption). Every message is read and remembered at once, but
// the reply waits until the customer has been quiet for Options.ReplyDelay,
// so a burst gets one answer that covers all of it, like a person would give.

type convKey struct{ acct, conv int64 }

// pendingReply is a reply waiting for the customer to stop typing.
type pendingReply struct {
	timer  *time.Timer
	images []string // photos from every message in the burst
	seq    uint64   // bumped by each new message; only the latest timer replies
}

// deferReply (re)starts the conversation's wait and keeps its photos.
func (b *Bot) deferReply(log *slog.Logger, ev webhook.Event, deliveryID string, images []string) {
	key := convKey{ev.Account.ID, ev.ConversationID()}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending == nil {
		b.pending = map[convKey]*pendingReply{}
	}
	p := b.pending[key]
	if p == nil {
		p = &pendingReply{}
		b.pending[key] = p
	} else {
		p.timer.Stop()
	}
	p.images = append(p.images, images...)
	if len(p.images) > maxImages {
		p.images = p.images[len(p.images)-maxImages:]
	}
	p.seq++
	job := Job{Event: ev, Decision: webhook.Reply, DeliveryID: deliveryID, flush: p.seq}
	p.timer = time.AfterFunc(b.opts.ReplyDelay, func() {
		if err := b.Enqueue(job); err != nil {
			log.Warn("waiting reply not queued", "error", err)
			b.cancelPending(key.acct, key.conv)
		}
	})
}

// takePending returns the burst's photos if seq is still the latest wait.
func (b *Bot) takePending(acct, conv int64, seq uint64) ([]string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.pending[convKey{acct, conv}]
	if p == nil || p.seq != seq {
		return nil, false
	}
	delete(b.pending, convKey{acct, conv})
	return p.images, true
}

// cancelPending drops a waiting reply: staff stepped in, the chat was
// resolved or handed off.
func (b *Bot) cancelPending(acct, conv int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p := b.pending[convKey{acct, conv}]; p != nil {
		p.timer.Stop()
		delete(b.pending, convKey{acct, conv})
	}
}

// flush answers a burst once the customer has gone quiet.
func (b *Bot) flush(ctx context.Context, log *slog.Logger, j Job) {
	ev := j.Event
	acct, conv := ev.Account.ID, ev.ConversationID()
	images, ok := b.takePending(acct, conv, j.flush)
	if !ok {
		return // a newer message restarted the wait, or staff took over
	}
	tr := b.startTurn(ev, customerText(ev))
	defer b.finishTurn(tr)
	b.reply(ctx, log, acct, ev.Inbox.ID, conv, tr, images)
}
