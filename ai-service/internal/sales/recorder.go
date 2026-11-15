package sales

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/webhook"
)

// Messages reads a chat's messages, oldest first (chatwoot.Reader).
type Messages interface {
	AllMessages(ctx context.Context, accountID, conversationID int64, pages int) ([]gaps.Message, error)
}

// Notes posts a private note in a chat (chatwoot.Client), to confirm a
// #paid note to staff.
type Notes interface {
	SendPrivateNote(ctx context.Context, accountID, conversationID int64, content string) error
}

// creditPages bounds how much of a chat is read to credit a sale.
const (
	creditPages     = 5
	messagesPerPage = 20
)

// queueSize is how many changes wait for the worker; beyond it a change is
// dropped and logged (the webhook must answer at once).
const queueSize = 1024

// untrustedChannels let the customer write conversation labels and custom
// attributes themselves (the website widget's setLabel and
// setConversationCustomAttributes, and the API channel's public client API),
// so "paid" and paid_amount from them are ignored. Staff use a #paid note
// there instead.
var untrustedChannels = map[string]bool{"Channel::WebWidget": true, "Channel::Api": true}

// Note tags staff write in a private note. The Thai ones mean "paid" and
// "cancel payment".
var (
	paidTags   = []string{"#paid", "#ชำระแล้ว", "#จ่ายแล้ว"}
	unpaidTags = []string{"#unpaid", "#ยกเลิกชำระ"}
)

// Notes the recorder writes back.
const (
	notePaid      = "✅ บันทึกว่าแชตนี้ชำระแล้ว%s (นับในหน้า Sales)"
	noteUnpaid    = "↩️ ยกเลิกยอดชำระของแชตนี้แล้ว (ไม่นับในหน้า Sales)"
	noteBadAmount = "⚠️ ยังไม่ได้บันทึก: อ่านยอดเงินไม่ได้ พิมพ์แบบนี้ เช่น\n#paid 3,900"
	noteFailed    = "⚠️ ยังไม่ได้บันทึก: ระบบบันทึกไม่สำเร็จ ลองส่งอีกครั้ง"
)

// change is one paid state seen for a chat.
type change struct {
	accountID, conversationID, inboxID int64
	paid                               bool
	amount                             *int64
	byStaff                            bool // from a #paid note
	reply                              bool // answer the note with a note
	badAmount                          bool // a #paid note whose amount could not be read
	at, source                         time.Time
}

// Recorder turns Chatwoot events into sale rows: the "paid" label and the
// paid_amount attribute on conversation_updated (trusted channels only), and
// staff #paid / #unpaid notes on any channel. The webhook handler calls
// Observe; one worker does the database and Chatwoot work, in order.
type Recorder struct {
	Store    Store
	Messages Messages // optional; nil leaves the credit unknown
	Notes    Notes    // optional; nil sends no confirmation note
	Logger   *slog.Logger
	Now      func() time.Time

	queue chan change
	mu    sync.Mutex
	seen  map[int64]time.Time // note ids already handled (redeliveries)
}

// Start runs the worker until ctx ends. Observe before Start drops events.
func (r *Recorder) Start(ctx context.Context) {
	r.queue = make(chan change, queueSize)
	r.seen = map[int64]time.Time{}
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

// Observe looks at a verified webhook event and queues a sale change. It
// never blocks.
func (r *Recorder) Observe(ev webhook.Event) {
	if r == nil || r.queue == nil || ev.Account.ID < 1 {
		return
	}
	var c change
	var ok bool
	switch ev.Event {
	case "conversation_updated":
		c, ok = r.fromUpdate(ev)
	case "message_created":
		c, ok = r.fromNote(ev)
	}
	if !ok {
		return
	}
	select {
	case r.queue <- c:
	default:
		r.Logger.Error("sale change dropped: queue full", "account_id", c.accountID, "conversation_id", c.conversationID)
	}
}

// fromUpdate reads the label and amount of a conversation update.
func (r *Recorder) fromUpdate(ev webhook.Event) (change, bool) {
	if ev.ID < 1 {
		return change{}, false
	}
	if names, ok := ev.Changed(); ok && !touchesSale(names) {
		return change{}, false
	}
	channel := ev.Channel()
	if channel == "" || untrustedChannels[channel] {
		return change{}, false // the customer could have written it
	}
	labels, ok := ev.LabelList()
	if !ok {
		return change{}, false // labels unknown: never cancel on a guess
	}
	c := change{accountID: ev.Account.ID, conversationID: ev.ID, inboxID: ev.ConversationInbox(),
		paid: hasLabel(labels, LabelPaid), at: r.now(), source: ev.UpdatedTime()}
	if raw := ev.Attribute(AmountAttribute); len(raw) > 0 && string(raw) != "null" {
		if n, err := ParseBaht(raw); err == nil {
			c.amount = &n
		} else {
			r.Logger.Warn("paid_amount is not an amount in baht", "account_id", c.accountID, "conversation_id", c.conversationID)
		}
	}
	return c, true
}

// fromNote reads a staff #paid or #unpaid private note.
func (r *Recorder) fromNote(ev webhook.Event) (change, bool) {
	if !ev.Private || ev.MessageType != "outgoing" || ev.Sender.Type != "user" || ev.Conversation.ID < 1 {
		return change{}, false
	}
	paid, rest, ok := parseNote(ev.Content)
	if !ok || !r.firstTime(ev.ID) {
		return change{}, false
	}
	c := change{accountID: ev.Account.ID, conversationID: ev.Conversation.ID, inboxID: max(ev.Inbox.ID, 0),
		paid: paid, byStaff: true, reply: true, at: r.now()}
	if paid && rest != "" {
		raw, _ := json.Marshal(rest)
		n, err := ParseBaht(raw)
		if err != nil {
			r.Logger.Warn("#paid note amount not read", "account_id", c.accountID, "conversation_id", c.conversationID)
			c.badAmount = true
			return c, true
		}
		c.amount = &n
	}
	return c, true
}

// parseNote reports whether a note starts with a paid or unpaid tag, and the
// rest of its first line.
func parseNote(content string) (paid bool, rest string, ok bool) {
	s := strings.TrimPrefix(strings.TrimSpace(content), `\`)
	line, _, _ := strings.Cut(s, "\n")
	for _, set := range []struct {
		tags []string
		paid bool
	}{{paidTags, true}, {unpaidTags, false}} {
		for _, tag := range set.tags {
			if len(line) < len(tag) || !strings.EqualFold(line[:len(tag)], tag) {
				continue
			}
			after := line[len(tag):]
			if r := firstRune(after); r != 0 && !unicode.IsSpace(r) && r != ':' {
				continue
			}
			return set.paid, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(after), ":")), true
		}
	}
	return false, "", false
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

// firstTime remembers note ids for a day, so a redelivered webhook does not
// post a second confirmation.
func (r *Recorder) firstTime(id int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if _, dup := r.seen[id]; dup {
		return false
	}
	if len(r.seen) > 10_000 {
		for k, t := range r.seen {
			if now.Sub(t) > 24*time.Hour {
				delete(r.seen, k)
			}
		}
	}
	r.seen[id] = now
	return true
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
	if c.badAmount {
		r.reply(ctx, log, c, noteBadAmount)
		return
	}
	if !c.paid {
		if err := r.Store.Cancel(ctx, c.accountID, c.conversationID, c.byStaff, c.source); err != nil {
			log.Error("sale cancel failed", "error", err)
			r.reply(ctx, log, c, noteFailed)
			return
		}
		r.reply(ctx, log, c, noteUnpaid)
		return
	}
	applied, needCredit, err := r.Store.MarkPaid(ctx, Paid{AccountID: c.accountID, ConversationID: c.conversationID,
		InboxID: c.inboxID, Amount: c.amount, ByStaff: c.byStaff, At: c.at, Source: c.source})
	if err != nil {
		log.Error("sale not recorded", "error", err)
		r.reply(ctx, log, c, noteFailed)
		return
	}
	if !applied {
		log.Info("older sale change skipped")
		return
	}
	log.Info("sale recorded", "has_amount", c.amount != nil, "by_note", c.byStaff)
	amount := ""
	if c.amount != nil {
		amount = " ยอด " + bahtText(*c.amount)
	}
	r.reply(ctx, log, c, fmt.Sprintf(notePaid, amount))
	if needCredit {
		r.credit(ctx, log, c)
	}
}

// credit works out and saves who closed the sale.
func (r *Recorder) credit(ctx context.Context, log *slog.Logger, c change) {
	if r.Messages == nil {
		return
	}
	msgs, err := r.Messages.AllMessages(ctx, c.accountID, c.conversationID, creditPages)
	if err != nil || len(msgs) == 0 {
		log.Warn("sale credit unknown: chat not read", "error", err)
		return
	}
	credit := CreditOf(msgs, len(msgs) < creditPages*messagesPerPage)
	if credit == "" {
		log.Info("sale credit unknown: chat longer than what was read")
		return
	}
	if err := r.Store.SetCredit(ctx, c.accountID, c.conversationID, credit); err != nil {
		log.Error("sale credit not saved", "error", err)
		return
	}
	log.Info("sale credited", "credit", string(credit))
}

// reply answers a #paid or #unpaid note; label changes get no note.
func (r *Recorder) reply(ctx context.Context, log *slog.Logger, c change, note string) {
	if !c.reply || r.Notes == nil {
		return
	}
	if err := r.Notes.SendPrivateNote(ctx, c.accountID, c.conversationID, note); err != nil {
		log.Warn("sale note not sent", "error", err)
	}
}

// bahtText formats satang for a note: "฿3,900" or "฿3,900.50".
func bahtText(satang int64) string {
	whole, frac := satang/100, satang%100
	s := fmt.Sprint(whole)
	var b strings.Builder
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(ch)
	}
	if frac != 0 {
		return fmt.Sprintf("฿%s.%02d", b.String(), frac)
	}
	return "฿" + b.String()
}
