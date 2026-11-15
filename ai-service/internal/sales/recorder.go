package sales

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/webhook"
)

// Messages reads a chat's messages, oldest first (chatwoot.Reader).
type Messages interface {
	AllMessages(ctx context.Context, accountID, conversationID int64, pages int) ([]gaps.Message, error)
}

// creditPages bounds how much of a chat is read to credit a sale (20 each).
const creditPages = 5

// queueSize is how many label changes wait for the worker; beyond it a
// change is dropped and logged (the webhook must answer at once).
const queueSize = 256

// change is one paid-label state seen on a conversation event.
type change struct {
	accountID, conversationID, inboxID int64
	paid                               bool
	amount                             *int64
	at                                 time.Time
}

// Recorder turns Chatwoot conversation_updated events into sale rows. The
// webhook handler calls Observe; one worker does the database and Chatwoot
// work, in order.
type Recorder struct {
	Store    Store
	Messages Messages // optional; nil leaves the credit unknown
	Logger   *slog.Logger
	Now      func() time.Time

	queue chan change
}

// Start runs the worker until ctx ends. Observe before Start drops events.
func (r *Recorder) Start(ctx context.Context) {
	r.queue = make(chan change, queueSize)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case c := <-r.queue:
				r.apply(ctx, c)
			}
		}
	}()
}

// Observe looks at a verified webhook event and queues a sale change when a
// conversation's labels or attributes changed. It never blocks.
func (r *Recorder) Observe(ev webhook.Event) {
	if r == nil || r.queue == nil || ev.Event != "conversation_updated" || ev.Account.ID < 1 || ev.ID < 1 {
		return
	}
	if names, ok := ev.Changed(); ok && !touchesSale(names) {
		return
	}
	c := change{accountID: ev.Account.ID, conversationID: ev.ID, inboxID: ev.ConversationInbox(),
		paid: hasLabel(ev.LabelList(), LabelPaid), at: r.now()}
	if raw := ev.Attribute(AmountAttribute); len(raw) > 0 && string(raw) != "null" {
		if n, err := ParseBaht(raw); err == nil {
			c.amount = &n
		} else {
			r.Logger.Warn("paid_amount is not an amount in baht", "account_id", c.accountID, "conversation_id", c.conversationID)
		}
	}
	select {
	case r.queue <- c:
	default:
		r.Logger.Error("sale change dropped: queue full", "account_id", c.accountID, "conversation_id", c.conversationID)
	}
}

func (r *Recorder) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// touchesSale reports whether changed attributes can change a sale.
func touchesSale(names []string) bool {
	for _, n := range names {
		if strings.Contains(n, "label") || n == "custom_attributes" {
			return true
		}
	}
	return false
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if strings.EqualFold(strings.TrimSpace(l), want) {
			return true
		}
	}
	return false
}

// apply writes one change: paid (and its credit, the first time) or cancelled.
func (r *Recorder) apply(parent context.Context, c change) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	log := r.Logger.With("account_id", c.accountID, "conversation_id", c.conversationID)
	if !c.paid {
		if err := r.Store.Cancel(ctx, c.accountID, c.conversationID); err != nil {
			log.Error("sale cancel failed", "error", err)
		}
		return
	}
	needCredit, err := r.Store.MarkPaid(ctx, c.accountID, c.conversationID, c.inboxID, c.amount, c.at)
	if err != nil {
		log.Error("sale not recorded", "error", err)
		return
	}
	log.Info("sale recorded", "has_amount", c.amount != nil)
	if !needCredit || r.Messages == nil {
		return
	}
	msgs, err := r.Messages.AllMessages(ctx, c.accountID, c.conversationID, creditPages)
	if err != nil || len(msgs) == 0 {
		log.Warn("sale credit unknown: chat not read", "error", err)
		return
	}
	credit := CreditOf(msgs)
	if err := r.Store.SetCredit(ctx, c.accountID, c.conversationID, credit); err != nil {
		log.Error("sale credit not saved", "error", err)
		return
	}
	log.Info("sale credited", "credit", string(credit))
}
