// Package messenger relays Meta's Messenger webhook to Chatwoot and keeps the
// ad or link a customer came from (the "referral"), which Chatwoot drops for
// Messenger (DEV-Johnstocker/chatwoot#1).
//
// Payload shapes: Meta's Messenger Platform webhook reference, "messages"
// (message.referral for Click-to-Messenger ads), "messaging_referrals"
// (messaging[].referral for m.me links on an open thread) and
// "messaging_postbacks" (postback.referral for Get Started).
package messenger

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// Referral says where a customer came from. Every field is outside data: the
// signature proves Meta sent it, but a link's ref is chosen by whoever made
// the link, so the bot treats all of it as data, never instructions.
type Referral struct {
	Source  string // ADS, SHORTLINK, CUSTOMER_CHAT_PLUGIN, ...
	Ref     string // the link's or ad's ref parameter (cleaned)
	AdID    string // digits only; empty unless Source is ADS
	AdTitle string // the ad's title (cleaned, cut)
	PostID  string // the ad's post (cleaned)
	At      time.Time
}

// Sighting is a referral for one customer (PSID) of one Page.
type Sighting struct {
	PageID   string
	PSID     string
	Key      string // identifies the event, so a retry of it is kept once
	Referral Referral
}

// Limits for the stored text; Meta's ref is at most 250 characters.
const (
	maxRef   = 64
	maxTitle = 120
	maxID    = 40
)

type rawReferral struct {
	Ref            string `json:"ref"`
	Source         string `json:"source"`
	AdID           string `json:"ad_id"`
	AdsContextData struct {
		AdTitle string `json:"ad_title"`
		PostID  string `json:"post_id"`
	} `json:"ads_context_data"`
}

type payload struct {
	Object string `json:"object"`
	Entry  []struct {
		ID        string `json:"id"`
		Messaging []struct {
			Sender struct {
				ID string `json:"id"`
			} `json:"sender"`
			Timestamp int64        `json:"timestamp"`
			Referral  *rawReferral `json:"referral"`
			Message   *struct {
				MID      string       `json:"mid"`
				IsEcho   bool         `json:"is_echo"`
				Referral *rawReferral `json:"referral"`
			} `json:"message"`
			Postback *struct {
				Referral *rawReferral `json:"referral"`
			} `json:"postback"`
		} `json:"messaging"`
	} `json:"entry"`
}

// ParseReferrals finds every referral in a Messenger webhook body. A body
// that is not a Page webhook, or carries none, gives nil.
func ParseReferrals(body []byte) []Sighting {
	var p payload
	if err := json.Unmarshal(body, &p); err != nil || p.Object != "page" {
		return nil
	}
	var out []Sighting
	for _, e := range p.Entry {
		for _, m := range e.Messaging {
			raw := m.Referral
			if raw == nil && m.Message != nil && !m.Message.IsEcho {
				raw = m.Message.Referral
			}
			if raw == nil && m.Postback != nil {
				raw = m.Postback.Referral
			}
			psid := Digits(m.Sender.ID, maxID)
			if raw == nil || psid == "" {
				continue
			}
			r := clean(*raw)
			if m.Timestamp > 0 {
				r.At = time.UnixMilli(m.Timestamp).UTC()
			}
			if r.Source == "" && r.Ref == "" && r.AdID == "" {
				continue
			}
			key := fmt.Sprintf("%s:%d:%s:%s:%s", psid, m.Timestamp, r.Source, r.AdID, r.Ref)
			if m.Message != nil && m.Message.MID != "" {
				key = "mid:" + m.Message.MID
			}
			out = append(out, Sighting{PageID: Digits(e.ID, maxID), PSID: psid, Key: key, Referral: r})
		}
	}
	return out
}

func clean(raw rawReferral) Referral {
	r := Referral{
		Source:  Token(strings.ToUpper(raw.Source), 32),
		Ref:     Token(raw.Ref, maxRef),
		AdID:    Digits(raw.AdID, maxID),
		AdTitle: Text(raw.AdsContextData.AdTitle, maxTitle),
		PostID:  Token(raw.AdsContextData.PostID, maxID+maxID),
	}
	if r.Source != "ADS" {
		r.AdID = ""
	}
	return r
}

// Digits keeps s only when it is all ASCII digits and not too long.
func Digits(s string, max int) string {
	if s == "" || len(s) > max {
		return ""
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return s
}

// Token keeps ASCII letters, digits, '-', '_' and '.', cut to max
// characters, so it is safe as a label and inside a quoted prompt line.
func Token(s string, max int) string {
	var b strings.Builder
	for _, c := range s {
		if b.Len() == max {
			break
		}
		if c == '-' || c == '_' || c == '.' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// Text makes s one plain line: no control characters, quotes or brackets
// (so it cannot close the quote it is shown in), spaces collapsed, cut to
// max runes.
func Text(s string, max int) string {
	var b strings.Builder
	n, space := 0, false
	for _, c := range s {
		if n == max {
			break
		}
		switch {
		case unicode.IsSpace(c) || unicode.IsControl(c) || strings.ContainsRune("\"'`<>[]{}#“”„‟«»‘’‚‹›＂＇", c):
			space = b.Len() > 0
			continue
		case unicode.Is(unicode.Cf, c): // zero-width and direction marks
			continue
		}
		if space {
			b.WriteByte(' ')
			n++
			space = false
			if n == max {
				break
			}
		}
		b.WriteRune(c)
		n++
	}
	return b.String()
}
