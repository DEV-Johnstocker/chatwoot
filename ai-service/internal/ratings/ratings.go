// Package ratings keeps the owner's thumbs up or down on bot replies. The
// replies are read live from Chatwoot; a rating stores a copy of the reply and
// the customer message before it, so it can later become a test case.
// Issue: DEV-Johnstocker/chatwoot#1
package ratings

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
)

// Size caps, the same as the table's checks.
const (
	MaxCustomer = 2000
	MaxBot      = 4000
	MaxNote     = 1000
	MaxRatedBy  = 200
)

// Reasons a reply can be rated down, in the order the page offers them.
var Reasons = []string{"wrong_fact", "missed_sale", "handoff", "tone", "too_long", "other"}

// Reply is one bot reply and the customer message(s) it answered.
type Reply struct {
	ConversationID int64
	InboxID        int64
	MessageID      int64
	Customer       string
	Bot            string
	At             time.Time
}

// Rating is the owner's verdict on one reply.
type Rating struct {
	AccountID      int64
	ConversationID int64
	MessageID      int64
	Good           bool
	Reason         string // one of Reasons; only for a bad rating
	Note           string
	Customer       string
	Bot            string
	RatedBy        string
	RepliedAt      time.Time
	RatedAt        time.Time
}

// Validation errors, shown to the owner.
var (
	ErrNoReason  = errors.New("pick what was wrong with the reply")
	ErrBadReason = errors.New("unknown reason")
	ErrBadIDs    = errors.New("account, chat and message must be positive numbers")
)

// Clean checks r and trims and caps its text fields.
func (r *Rating) Clean() error {
	if r.AccountID < 1 || r.ConversationID < 1 || r.MessageID < 1 {
		return ErrBadIDs
	}
	if r.Good {
		r.Reason = ""
	} else if r.Reason == "" {
		return ErrNoReason
	} else if !slices.Contains(Reasons, r.Reason) {
		return ErrBadReason
	}
	r.Note = Cut(text(r.Note), MaxNote)
	r.Customer = Cut(text(r.Customer), MaxCustomer)
	r.Bot = Cut(text(r.Bot), MaxBot)
	r.RatedBy = Cut(text(r.RatedBy), MaxRatedBy)
	return nil
}

// text makes s safe for a Postgres TEXT column: valid UTF-8, no NUL, trimmed.
func text(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ToValidUTF8(s, "\uFFFD"), "\x00", ""))
}

// Cut shortens s to at most n characters, ending with "…" when cut.
func Cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// Replies finds the bot's public replies in a chat's messages (oldest
// first), each with the customer messages since the previous reply.
func Replies(msgs []gaps.Message) []Reply {
	var out []Reply
	var asked []string
	for _, m := range msgs {
		switch {
		case m.Private:
			continue
		case m.Incoming:
			if t := strings.TrimSpace(m.Content); t != "" {
				asked = append(asked, t)
			}
		case m.Outgoing:
			t := strings.TrimSpace(m.Content)
			if m.FromBot && t != "" {
				out = append(out, Reply{MessageID: m.ID, Bot: Cut(t, MaxBot),
					Customer: Cut(strings.Join(asked, "\n"), MaxCustomer), At: m.At})
			}
			asked = nil // staff or bot spoke: the next reply answers newer messages
		}
	}
	return out
}

// Store keeps ratings (Postgres in production).
type Store interface {
	Save(ctx context.Context, r Rating) error
	// Rated returns the ratings of the given messages, by message ID.
	Rated(ctx context.Context, accountID int64, messageIDs []int64) (map[int64]Rating, error)
	// List returns ratings newest first; good nil means both kinds.
	List(ctx context.Context, accountID int64, good *bool, limit int) ([]Rating, error)
	Counts(ctx context.Context, accountID int64) (good, bad int, err error)
}

// Source reads recent chats and their latest messages (chatwoot.Reader).
type Source interface {
	RecentChats(ctx context.Context, accountID int64, page int) ([]gaps.Chat, error)
	LatestMessages(ctx context.Context, accountID, conversationID int64) ([]gaps.Message, error)
}

// Feed lists recent bot replies.
type Feed struct {
	Source  Source
	Workers int // message reads at once; 0 means 4
}

// Recent returns the bot replies in one page of the most recently active
// chats, newest first. A chat whose messages cannot be read is skipped.
func (f *Feed) Recent(ctx context.Context, accountID int64, page int) ([]Reply, error) {
	chats, err := f.Source.RecentChats(ctx, accountID, page)
	if err != nil {
		return nil, err
	}
	workers := f.Workers
	if workers <= 0 {
		workers = 4
	}
	per := make([][]Reply, len(chats))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, c := range chats {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			msgs, err := f.Source.LatestMessages(ctx, accountID, c.ID)
			if err != nil {
				return
			}
			rs := Replies(msgs)
			for j := range rs {
				rs[j].ConversationID, rs[j].InboxID = c.ID, c.InboxID
			}
			per[i] = rs
		}()
	}
	wg.Wait()
	var out []Reply
	for _, rs := range per {
		out = append(out, rs...)
	}
	slices.SortStableFunc(out, func(a, b Reply) int { return b.At.Compare(a.At) })
	return out, nil
}
