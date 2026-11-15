// Package sales records which chats paid, how much, and whether the bot or
// staff closed the sale, for the /admin sales dashboard.
//
// The flow follows the owner's choice (2026-11-14):
//   - When a customer sends a payment slip, the bot's handoff note files it
//     under payments: the chat gets the label "paid-unconfirmed" and a "slip"
//     row with the amount the note model read from the image (not checked).
//   - Staff check the money and confirm in Chatwoot: the label "paid" and the
//     conversation attribute "paid_amount". Chatwoot's conversation_updated
//     webhook carries both, and the row becomes "paid".
//   - Removing "paid" marks the row "cancelled".
//
// The sale is credited to the bot when nothing went to staff before the
// payment: no hand-off note and no staff reply before the slip.
// Issue: DEV-Johnstocker/chatwoot#1
package sales

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
)

// Chatwoot names staff use.
const (
	LabelPaid       = "paid"             // staff checked the money
	LabelSlip       = "paid-unconfirmed" // the bot saw a slip; not checked
	AmountAttribute = "paid_amount"      // conversation attribute, in baht
)

// Status of a sale row.
type Status string

// Statuses.
const (
	StatusSlip      Status = "slip"
	StatusPaid      Status = "paid"
	StatusCancelled Status = "cancelled"
)

// Credit says who closed a sale; "" is not known (the chat could not be read).
type Credit string

// Credits.
const (
	CreditBot   Credit = "bot"
	CreditStaff Credit = "staff"
)

// Sale is one paying chat.
type Sale struct {
	AccountID      int64
	ConversationID int64
	InboxID        int64
	Status         Status
	SlipAmount     *int64 // satang, as the bot read it from the slip
	Amount         *int64 // satang, as staff entered it
	Credit         Credit
	SlipAt         time.Time
	PaidAt         time.Time
}

// Totals sum the paid sales of a period.
type Totals struct {
	Paid, Bot, Staff, Unknown                     int
	Revenue, BotRevenue, StaffRevenue, UnknownRev int64 // satang
	NoAmount                                      int   // paid without paid_amount
}

// Store keeps sales (Postgres in production). Every method is scoped to one
// account.
type Store interface {
	// RecordSlip marks a chat as having sent a slip, unless it is already paid.
	RecordSlip(ctx context.Context, s Sale) error
	// MarkPaid marks a chat paid. amount nil keeps the amount already stored.
	// It reports whether the sale still needs its credit worked out.
	MarkPaid(ctx context.Context, accountID, conversationID, inboxID int64, amount *int64, at time.Time) (needCredit bool, err error)
	SetCredit(ctx context.Context, accountID, conversationID int64, c Credit) error
	// Cancel marks a paid chat cancelled; other rows are left alone.
	Cancel(ctx context.Context, accountID, conversationID int64) error
	Totals(ctx context.Context, accountID int64, from, to time.Time) (Totals, error)
	// Paid lists paid sales in [from, to), newest first.
	Paid(ctx context.Context, accountID int64, from, to time.Time, limit int) ([]Sale, error)
	// Slips lists slips not confirmed yet, newest first.
	Slips(ctx context.Context, accountID int64, limit int) ([]Sale, error)
}

// MaxSatang bounds an amount (under 1 billion baht), as the table does.
const MaxSatang = 100_000_000_000

// ErrAmount means an amount could not be read as baht.
var ErrAmount = errors.New("not an amount in baht")

// ParseBaht reads an amount staff typed or Chatwoot stored: a JSON number or
// a string such as "3,900", "3900.50 บาท" or "฿1,300". It returns satang.
func ParseBaht(raw json.RawMessage) (int64, error) {
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return 0, ErrAmount
		}
		return parseBahtText(s)
	}
	return toSatang(f)
}

var bahtNoise = strings.NewReplacer(",", "", " ", "", "บาท", "", "฿", "", "THB", "", "thb", "", " ", "")

func parseBahtText(s string) (int64, error) {
	s = bahtNoise.Replace(strings.TrimSpace(s))
	if s == "" {
		return 0, ErrAmount
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, ErrAmount
	}
	return toSatang(f)
}

func toSatang(f float64) (int64, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f*100 >= MaxSatang {
		return 0, ErrAmount
	}
	return int64(math.Round(f * 100)), nil
}

// slipAmount finds an amount in baht in the note model's "จากไฟล์" line.
var slipAmount = regexp.MustCompile(`(?:฿\s*([0-9][0-9,]*(?:\.[0-9]{1,2})?))|(?:([0-9][0-9,]*(?:\.[0-9]{1,2})?)\s*(?:บาท|THB|฿))`)

// SlipAmount reads the amount from a handoff summary's "จากไฟล์" (from the
// file) line; nil when there is none. It is what the model read off the
// image: shown as a hint, never as confirmed money.
func SlipAmount(summary string) *int64 {
	for _, line := range strings.Split(summary, "\n") {
		if !strings.Contains(line, "จากไฟล์") {
			continue
		}
		m := slipAmount.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		v := m[1]
		if v == "" {
			v = m[2]
		}
		if n, err := parseBahtText(v); err == nil && n > 0 {
			return &n
		}
	}
	return nil
}

// PaymentNoteTitle heads the bot's handoff note for a payment (bot/note.go).
const PaymentNoteTitle = "เรื่องชำระเงิน"

// handoffCode ends every handoff note the bot writes.
const handoffCode = "ai-handoff:"

// CreditOf decides who closed a sale from the chat's messages (oldest
// first). The payment point is the bot's first payment note; with none, the
// end of the chat. The bot gets the credit when, before that point, no staff
// member replied to the customer and the bot handed nothing over.
func CreditOf(msgs []gaps.Message) Credit {
	end := len(msgs)
	for i, m := range msgs {
		if m.Private && m.FromBot && strings.Contains(m.Content, PaymentNoteTitle) {
			end = i
			break
		}
	}
	for _, m := range msgs[:end] {
		switch {
		case m.Outgoing && !m.Private && m.FromStaff:
			return CreditStaff
		case m.Private && m.FromBot && strings.Contains(m.Content, handoffCode):
			return CreditStaff
		}
	}
	return CreditBot
}
