// Package referral captures where a Messenger chat came from: a
// Click-to-Messenger ad, an m.me link with a ref, or another referral source.
// Facebook attaches this to the customer's message or sends it alone, but
// Chatwoot drops it, so ai-service reads it on the way to Chatwoot (Proxy) and
// keeps it here. Issues: DEV-Johnstocker/chatwoot#1, #155.
package referral

import (
	"encoding/json"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Field limits. A headline is shown to the owner and, later, to the bot as a
// hint, so it is kept short and on one line.
const (
	MaxTitleRunes = 120
	maxRefLen     = 250
	maxIDLen      = 64
	maxSourceLen  = 32
)

// Referral is one referral event: who arrived, on which Page, from what.
type Referral struct {
	PageID     string    // the Page that received the message
	PSID       string    // the customer's Page-scoped Messenger id (personal data)
	Source     string    // "ADS", "SHORTLINK", ... upper-cased
	Ref        string    // the ref parameter of an m.me link or ad, if any
	AdID       string    // set for ads
	AdTitle    string    // the ad's headline at the time of the tap
	ReferredAt time.Time // Facebook's timestamp of the event, UTC
}

// IsAd reports whether the referral came from an ad.
func (r Referral) IsAd() bool { return r.Source == "ADS" }

type rawReferral struct {
	Ref            string `json:"ref"`
	AdID           string `json:"ad_id"`
	Source         string `json:"source"`
	AdsContextData struct {
		AdTitle string `json:"ad_title"`
	} `json:"ads_context_data"`
}

type payload struct {
	Object string `json:"object"`
	Entry  []struct {
		ID        string `json:"id"`
		Messaging []struct {
			Sender    struct{ ID string } `json:"sender"`
			Recipient struct{ ID string } `json:"recipient"`
			Timestamp int64               `json:"timestamp"`
			Referral  *rawReferral        `json:"referral"`
			Postback  *struct {
				Referral *rawReferral `json:"referral"`
			} `json:"postback"`
			Message *struct {
				IsEcho   bool         `json:"is_echo"`
				Referral *rawReferral `json:"referral"`
			} `json:"message"`
		} `json:"messaging"`
	} `json:"entry"`
}

// Parse returns the referrals in a Messenger webhook body. Facebook's docs
// place the referral beside the message, inside a Get Started postback, or
// (for some ad formats) inside the message, so all three are read. Echoes of
// the Page's own messages and events without a source, ad or ref are skipped.
func Parse(body []byte) ([]Referral, error) {
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	var out []Referral
	for _, e := range p.Entry {
		for _, m := range e.Messaging {
			if m.Message != nil && m.Message.IsEcho {
				continue
			}
			raw := m.Referral
			if raw == nil && m.Postback != nil {
				raw = m.Postback.Referral
			}
			if raw == nil && m.Message != nil {
				raw = m.Message.Referral
			}
			if raw == nil || m.Timestamp <= 0 {
				continue // no timestamp: it would be saved as 1970 and break de-duplication
			}
			page := clean(m.Recipient.ID, maxIDLen)
			if page == "" {
				page = clean(e.ID, maxIDLen)
			}
			r := Referral{
				PageID:     page,
				PSID:       clean(m.Sender.ID, maxIDLen),
				Source:     strings.ToUpper(clean(raw.Source, maxSourceLen)),
				Ref:        clean(raw.Ref, maxRefLen),
				AdID:       clean(raw.AdID, maxIDLen),
				AdTitle:    cleanRunes(raw.AdsContextData.AdTitle, MaxTitleRunes),
				ReferredAt: eventTime(m.Timestamp),
			}
			if r.PageID == "" || r.PSID == "" || r.PSID == r.PageID || (r.Source == "" && r.AdID == "" && r.Ref == "") {
				continue
			}
			out = append(out, r)
		}
	}
	return out, nil
}

// clean keeps one line of printable text, cut to n bytes on a rune boundary.
func clean(s string, n int) string {
	s = oneLine(s)
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// cleanRunes keeps one line of printable text, cut to n characters.
func cleanRunes(s string, n int) string {
	s = oneLine(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// eventTime reads Facebook's event timestamp. Messages carry milliseconds,
// but a standalone referral carries seconds (STAGING, 2026-11-23); a value
// below 10^11 cannot be milliseconds after 1973, so it is seconds.
func eventTime(ts int64) time.Time {
	if ts < 100_000_000_000 {
		return time.Unix(ts, 0).UTC()
	}
	return time.UnixMilli(ts).UTC()
}
