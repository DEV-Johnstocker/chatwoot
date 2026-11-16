// Package promos sorts an account's knowledge documents that carry dates
// (promotions) into a timeline for the /admin Promotions page: live now,
// ending within 7 days, starting later, and expired rows still stored. It
// flags the two mistakes that make the bot quote a dead offer or go silent
// after one (see .claude/skills/pronto-kb-dataset, "Promotions: write both
// sides with dates"):
//   - a live or upcoming promotion with an end but no price for after it;
//   - an expired row still in the knowledge base.
//
// Read only: nothing here changes a document.
// Issue: DEV-Johnstocker/chatwoot#1
package promos

import (
	"regexp"
	"sort"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
)

// Status of a promotion at a moment.
type Status string

// Statuses, in page order.
const (
	StatusEnding   Status = "ending"   // live, last day within EndingWithin
	StatusLive     Status = "live"     // live now
	StatusUpcoming Status = "upcoming" // starts later
	StatusExpired  Status = "expired"  // past its last day, still stored
)

// Statuses lists every status in page order.
var Statuses = []Status{StatusEnding, StatusLive, StatusUpcoming, StatusExpired}

// EndingWithin is how close the last day must be to count as "ending soon".
const EndingWithin = 7 * 24 * time.Hour

// successorSlack is how far a follow-on row's start may be from a
// promotion's end and still count as its after-price row.
const successorSlack = 24 * time.Hour

// Patterns from .claude/skills/pronto-kb-dataset/scripts/pronto_check.py, so
// the page and the sheet checker agree.
var (
	promoEnd = regexp.MustCompile(`(?i)(ถึง(วันที่)?\s*\d{1,2}\s*[ก-๙]|until\s+\d{1,2}\s+[A-Za-z]|หมดเขต|หมดโปร)`)
	price    = regexp.MustCompile(`(?i)\d[\d,]*\s*(บาท|฿|baht|THB)`)
	after    = regexp.MustCompile(`(?i)(ตั้งแต่|หลังจาก|หลังหมด|from\s+\d|after\s+\d)`)
)

// Item is one promotion on the timeline.
type Item struct {
	Doc    knowledge.Document
	Status Status
	// DatedInText: the end is written in the text only (no last day set),
	// so the bot keeps the row after the offer ends and must read the date.
	DatedInText bool
	// NoAfterPrice: the promotion ends but nothing says the price after it.
	NoAfterPrice bool
	// Days until it ends (ending/live) or starts (upcoming), or since it
	// ended (expired); -1 when not dated.
	Days int
}

// Timeline sorts the documents that are promotions at now. A document is a
// promotion when it has a start or last day, or its text states an end
// ("ถึง 30 พ.ย.", "หมดเขต") next to a price.
func Timeline(docs []knowledge.Document, now time.Time) []Item {
	var out []Item
	for _, d := range docs {
		text := d.Title + "\n" + d.Content
		dated := !d.ValidFrom.IsZero() || !d.ValidUntil.IsZero()
		inText := !dated && promoEnd.MatchString(text) && price.MatchString(text)
		if !dated && !inText {
			continue
		}
		it := Item{Doc: d, DatedInText: inText, Days: -1}
		it.Status, it.Days = status(d, now)
		// An expired row is flagged as expired; its after-price no longer matters.
		it.NoAfterPrice = it.Status != StatusExpired && noAfterPrice(d, text, docs)
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := rank(out[i].Status), rank(out[j].Status); a != b {
			return a < b
		}
		return before(out[i], out[j])
	})
	return out
}

func status(d knowledge.Document, now time.Time) (Status, int) {
	switch d.Status(now) {
	case knowledge.StatusExpired:
		return StatusExpired, days(now.Sub(d.ValidUntil))
	case knowledge.StatusScheduled:
		return StatusUpcoming, days(d.ValidFrom.Sub(now))
	}
	if d.ValidUntil.IsZero() {
		return StatusLive, -1
	}
	left := d.ValidUntil.Sub(now)
	if left <= EndingWithin {
		return StatusEnding, days(left)
	}
	return StatusLive, days(left)
}

// days rounds a duration up to whole days (a few hours left is "1 day").
func days(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int((d + 24*time.Hour - 1) / (24 * time.Hour))
}

// noAfterPrice: a row with a last day disappears whole when it ends, so its
// after-price must be in another row that starts about then. A row with the
// end only in its text must say the after-price itself ("ตั้งแต่ 1 ธ.ค. ...").
// A dated row with no price in it (a coupon note, an event) needs neither.
func noAfterPrice(d knowledge.Document, text string, docs []knowledge.Document) bool {
	if !price.MatchString(text) {
		return false
	}
	if d.ValidUntil.IsZero() {
		return promoEnd.MatchString(text) && !after.MatchString(text)
	}
	for _, o := range docs {
		if o.ID == d.ID || o.ValidFrom.IsZero() {
			continue
		}
		if gap := o.ValidFrom.Sub(d.ValidUntil); gap >= -successorSlack && gap <= successorSlack {
			return false
		}
	}
	return true
}

func rank(s Status) int {
	for i, v := range Statuses {
		if v == s {
			return i
		}
	}
	return len(Statuses)
}

// before orders items of one status: soonest end first for live rows (rows
// with no last day last), soonest start for upcoming, most recently expired
// first.
func before(a, b Item) bool {
	switch a.Status {
	case StatusUpcoming:
		return a.Doc.ValidFrom.Before(b.Doc.ValidFrom)
	case StatusExpired:
		return a.Doc.ValidUntil.After(b.Doc.ValidUntil)
	}
	switch {
	case a.Doc.ValidUntil.IsZero():
		return false
	case b.Doc.ValidUntil.IsZero():
		return true
	}
	return a.Doc.ValidUntil.Before(b.Doc.ValidUntil)
}

// Counts returns how many items have each status.
func Counts(items []Item) map[Status]int {
	out := map[Status]int{}
	for _, it := range items {
		out[it.Status]++
	}
	return out
}
