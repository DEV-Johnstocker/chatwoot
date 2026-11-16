// Package leads keeps the list of customers who showed buying interest, for
// the /admin Leads page and its CSV export.
//
// A chat becomes a lead when the customer asks about price, buying or a
// promotion (AsksPrice). It moves to "quoted" when the bot's reply gives a
// price or a link (GivesPrice). The later stages come from the sales table
// (schema v20): a slip the bot read is "slip", a payment staff
// confirmed is "paid". Facebook comments the comment scorer marked as leads
// are listed too, at the stage "commented".
//
// Personal data: a row holds the Chatwoot contact's name and id. It is shown
// only on /admin and never logged. An index on account and contact finds one
// customer's rows for a PDPA request; deleting them and a retention prune are
// not built yet (open item on #1).
// Issue: DEV-Johnstocker/chatwoot#1
package leads

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Stage is how far a lead got.
type Stage string

// Stages, in order.
const (
	StageCommented Stage = "commented"   // a Facebook comment the scorer marked as a lead
	StageAsked     Stage = "asked_price" // asked about price, buying or a promotion
	StageQuoted    Stage = "quoted"      // the bot gave a price or a link
	StageSlip      Stage = "slip"        // sent a payment slip (sales table)
	StagePaid      Stage = "paid"        // staff confirmed the payment (sales table)
)

// Stages lists every stage in funnel order.
var Stages = []Stage{StageCommented, StageAsked, StageQuoted, StageSlip, StagePaid}

// Valid reports whether s is a known stage.
func (s Stage) Valid() bool {
	for _, v := range Stages {
		if s == v {
			return true
		}
	}
	return false
}

// Source says where a lead came from.
type Source string

// Sources.
const (
	SourceChat    Source = "chat"    // a Chatwoot conversation
	SourceComment Source = "comment" // a Facebook Page comment
)

// Lead is one row of the Leads page.
type Lead struct {
	AccountID      int64
	Source         Source
	ConversationID int64 // chat leads
	CommentID      int64 // comment leads (fb_comments.id)
	InboxID        int64
	ContactID      int64
	Name           string
	Interest       string // the product or topic, from the knowledge the bot used
	Stage          Stage
	FirstAt        time.Time
	LastAt         time.Time // the customer's last message (or the comment's time)
}

// Contact is the customer behind a chat message.
type Contact struct {
	AccountID, ConversationID, InboxID, ContactID int64
	Name                                          string
}

// Filter narrows List. A zero Stage lists every stage.
type Filter struct {
	Stage Stage
	Limit int
}

// Store keeps chat leads (Postgres in production).
type Store interface {
	// Customer records a customer message. asked makes the chat a lead;
	// otherwise only an existing lead's last-contact time is updated.
	Customer(ctx context.Context, c Contact, asked bool) error
	// Replied records a bot reply in a lead's chat: the interest it was
	// about, and quoted when it gave a price or link. A chat that is not a
	// lead is left alone.
	Replied(ctx context.Context, accountID, conversationID int64, interest string, quoted bool) error
	// List returns the account's leads, newest contact first.
	List(ctx context.Context, accountID int64, f Filter) ([]Lead, error)
	// Counts returns how many leads the account has at each stage.
	Counts(ctx context.Context, accountID int64) (map[Stage]int, error)
}

// Field limits, matching schema v20's checks.
const (
	MaxName     = 256
	MaxInterest = 200
	MaxList     = 1000
)

// asksPrice matches a customer asking about price, buying, paying or a
// promotion, in Thai or English. Thai has no spaces between words, so Thai
// words match anywhere; English words need word boundaries.
var asksPrice = regexp.MustCompile(`(?i)(ราคา|เท่าไ|กี่บาท|ค่าใช้จ่าย|ค่าเรียน|ค่าคอร์ส|โปรโมชั่น|มีโปร|ส่วนลด|ลดราคา|คูปอง|ซื้อ|สมัคร|ผ่อน|ชำระ|จ่ายเงิน|โอนเงิน|เลขบัญชี|` +
	`\b(how much|prices?|pricing|costs?|buy|purchase|order|discounts?|promos?|promotions?|coupons?|pay|payment|enrol|enroll|sign ?up)\b)`)

// givesPrice matches a reply that states an amount in baht or gives a link.
var givesPrice = regexp.MustCompile(`(?i)(\d[\d,]*(\.\d+)?\s*(บาท|฿|baht|thb)|฿\s*\d|https?://\S+)`)

// AsksPrice reports whether a customer message shows buying interest.
func AsksPrice(text string) bool { return asksPrice.MatchString(text) }

// GivesPrice reports whether a bot reply gives a price or a link.
func GivesPrice(reply string) bool { return givesPrice.MatchString(reply) }

// Clip trims s to at most n runes, without breaking a character. It drops
// invalid UTF-8 and NUL bytes, which Postgres text refuses.
func Clip(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ToValidUTF8(s, ""), "\x00", ""))
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n]))
}
