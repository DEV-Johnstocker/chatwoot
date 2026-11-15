package bot

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/alerts"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/trace"
)

// ReasonReplyLimit: one customer made the bot reply ReplyLimit times within
// ReplyLimitWindow. The bot stops calling the model for them and passes the
// chat to staff once, without a message, so a flood cannot burn AI credit or
// hold up other customers' replies.
const ReasonReplyLimit = "ai-handoff:reply-limit"

// Alerts counts the bot's own failures for the owner (alerts.Monitor).
type Alerts interface {
	Record(kind alerts.Kind, acct int64)
	Turn(handedOff bool)
}

// limitKey is who a reply counts against: the contact in this inbox when
// Chatwoot names one (so new chats do not reset the count), else the chat.
// The inbox is part of it, so a visitor who joins someone's contact through
// one channel cannot silence the bot for them on another.
type limitKey struct {
	acct, inbox, id int64
	chat            bool
}

func limitKeyFor(acct, inbox, conv, contact int64) limitKey {
	if contact > 0 {
		return limitKey{acct: acct, inbox: inbox, id: contact}
	}
	return limitKey{acct: acct, inbox: inbox, id: conv, chat: true}
}

// limitPruneEvery bounds how often the reply counts are swept for old entries.
const limitPruneEvery = time.Minute

// limitOn reports whether the reply limit is configured.
func (b *Bot) limitOn() bool {
	return b.opts.ReplyLimit > 0 && b.opts.ReplyLimitWindow > 0
}

// takeReply counts one bot reply for key and reports whether it is allowed.
// The first refused reply also marks key as handed off for the window.
func (b *Bot) takeReply(key limitKey) bool {
	if !b.limitOn() {
		return true
	}
	now := b.now()
	from := now.Add(-b.opts.ReplyLimitWindow)
	b.limitMu.Lock()
	defer b.limitMu.Unlock()
	b.pruneLimits(now, from)
	times := b.replies[key]
	i := 0
	for i < len(times) && !times[i].After(from) {
		i++
	}
	times = times[i:]
	if len(times) >= b.opts.ReplyLimit {
		b.replies[key] = times
		b.limited[key] = now
		return false
	}
	b.replies[key] = append(times, now)
	return true
}

// overLimit reports whether key was handed off for the reply limit within
// the window: its later messages are not answered or even read.
func (b *Bot) overLimit(key limitKey) bool {
	if !b.limitOn() {
		return false
	}
	b.limitMu.Lock()
	defer b.limitMu.Unlock()
	at, ok := b.limited[key]
	return ok && b.now().Sub(at) < b.opts.ReplyLimitWindow
}

// pruneLimits drops counts that left the window, at most once a minute, so
// the maps stay as small as the number of recently active customers.
func (b *Bot) pruneLimits(now, from time.Time) {
	if b.replies == nil {
		b.replies, b.limited = map[limitKey][]time.Time{}, map[limitKey]time.Time{}
	}
	if now.Sub(b.limitPruned) < limitPruneEvery {
		return
	}
	b.limitPruned = now
	for k, ts := range b.replies {
		if len(ts) == 0 || !ts[len(ts)-1].After(from) {
			delete(b.replies, k)
		}
	}
	for k, at := range b.limited {
		if !at.After(from) {
			delete(b.limited, k)
		}
	}
}

// handOffReplyLimit passes a flooding customer's chat to staff once: no
// message to the customer, a private note with the reason, and an alert.
func (b *Bot) handOffReplyLimit(ctx context.Context, log *slog.Logger, acct, inbox, conv int64, tr *trace.Turn) {
	tr.Outcome = "handoff:" + ReasonReplyLimit
	log.Warn("reply limit reached: chat passed to staff", "limit", b.opts.ReplyLimit, "window", b.opts.ReplyLimitWindow.String())
	if b.opts.Alerts != nil {
		b.opts.Alerts.Record(alerts.ReplyLimit, acct)
	}
	b.markLimitNote(acct, conv)
	b.handoff(ctx, log, acct, inbox, conv, "", ReasonReplyLimit, nil)
}

// passLimited hands a chat that is over the limit back to staff: Chatwoot
// sends the bot a message only while the chat waits for it, so it was
// reopened or the first hand-off did not go through. The private note is
// posted at most once per botOffNoteFor; the hand-off itself every time.
func (b *Bot) passLimited(ctx context.Context, log *slog.Logger, acct, inbox, conv int64) {
	if b.markLimitNote(acct, conv) {
		b.handoff(ctx, log, acct, inbox, conv, "", ReasonReplyLimit, nil)
		return
	}
	b.cancelPending(acct, conv)
	if err := b.opts.Chatwoot.Handoff(ctx, acct, conv); err != nil {
		log.Error("handoff failed", "error", err, "reason", ReasonReplyLimit)
		return
	}
	log.Info("reply limit: chat passed to staff again")
}

// markLimitNote reports whether this chat has had no reply-limit note in the
// last botOffNoteFor, and records one now.
func (b *Bot) markLimitNote(acct, conv int64) bool {
	now := b.now()
	b.limitMu.Lock()
	defer b.limitMu.Unlock()
	if b.limitNoted == nil {
		b.limitNoted = map[convKey]time.Time{}
	}
	for k, at := range b.limitNoted { // only chats over the limit: a few at most
		if now.Sub(at) >= botOffNoteFor {
			delete(b.limitNoted, k)
		}
	}
	key := convKey{acct, conv}
	if _, done := b.limitNoted[key]; done {
		return false
	}
	b.limitNoted[key] = now
	return true
}

// alertTurn tells the alert monitor how a customer message ended. A bot
// switched off or a reply limit are the owner's own rules, and a customer
// asking for a person is their choice: none is a spike (one customer typing
// "human" ten times must not raise it);
// waiting and skipped messages are counted when their burst is answered.
func (b *Bot) alertTurn(tr *trace.Turn) {
	a := b.opts.Alerts
	if a == nil {
		return
	}
	reason, handedOff := strings.CutPrefix(tr.Outcome, "handoff:")
	switch {
	case handedOff && (reason == ReasonBotOff || reason == ReasonReplyLimit || reason == ReasonCustomerAsked):
		return
	case handedOff:
		switch reason {
		case ReasonLLMError:
			a.Record(alerts.LLMError, tr.AccountID)
		case ReasonSendFailed:
			a.Record(alerts.SendFailed, tr.AccountID)
		}
		a.Turn(true)
	case tr.Outcome == "replied":
		a.Turn(false)
	}
}
