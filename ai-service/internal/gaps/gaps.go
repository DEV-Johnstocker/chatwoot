// Package gaps finds the questions the bot could not answer. When the bot
// hands a chat over only because it lacked the answer, it labels the chat
// "kb-gap" and writes a private note for staff ("📚 บอทไม่มีข้อมูลเรื่องนี้",
// with a "ลูกค้าต้องการ:" line). This package reads those chats back from
// Chatwoot, so the owner can turn each one into a knowledge-base answer.
//
// Chatwoot stays the only copy: nothing is stored here. A handled gap gets the
// label "kb-gap-done" instead of "kb-gap", which also shows in Chatwoot's
// label report.
// Issue: DEV-Johnstocker/chatwoot#1
package gaps

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Labels on a gap chat, before and after the owner handled it.
const (
	Label     = "kb-gap"
	DoneLabel = "kb-gap-done"
)

// NoteTitle opens the bot's private note on a gap handoff.
const NoteTitle = "บอทไม่มีข้อมูลเรื่องนี้"

// wantedPrefix starts the note line that says what the customer wanted.
const wantedPrefix = "ลูกค้าต้องการ:"

// Size caps for what the page shows and pre-fills.
const (
	maxAsked = 300  // the question field allows 300 characters
	maxReply = 2000 // a staff reply used as the draft answer
)

// Message is one Chatwoot message, oldest first in a list.
type Message struct {
	ID       int64
	Incoming bool // from the customer
	Outgoing bool // from the bot or staff
	Private  bool // a note only staff see
	FromBot  bool // sent by the Agent Bot
	// FromStaff is a message a Chatwoot user (staff) wrote.
	FromStaff bool
	// Echo is a message sent outside Chatwoot (e.g. from the Messenger Page
	// app) that Chatwoot copied in.
	Echo    bool
	Content string
	At      time.Time
}

// Chat is a conversation carrying the gap label.
type Chat struct {
	ID           int64
	InboxID      int64
	LastActivity time.Time
}

// Gap is one question the bot could not answer.
type Gap struct {
	ConversationID int64
	InboxID        int64
	At             time.Time // when the bot handed over (the chat's last activity if the note is not found)
	Wanted         string    // the note's "ลูกค้าต้องการ" line
	Asked          string    // the customer's own words just before the handoff
	StaffReply     string    // the first staff reply after the handoff: a draft answer
	NoteFound      bool
}

// Question is what to pre-fill as the FAQ question: the customer's words,
// or the note's summary when there are none.
func (g Gap) Question() string {
	if g.Asked != "" {
		return g.Asked
	}
	return cut(g.Wanted, maxAsked)
}

// Read finds the gap in a conversation's messages (oldest first): the latest
// gap note from the bot, the customer messages of the turn before it, and the
// first public staff reply after it.
func Read(msgs []Message) (Gap, bool) {
	note := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Private && m.FromBot && strings.Contains(m.Content, NoteTitle) {
			note = i
			break
		}
	}
	if note < 0 {
		return Gap{}, false
	}
	g := Gap{At: msgs[note].At, Wanted: wanted(msgs[note].Content), NoteFound: true}
	var asked []string
	for i := note - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Private {
			continue
		}
		if m.Outgoing {
			if len(asked) > 0 {
				break // the bot's or staff's previous reply ends the customer's turn
			}
			continue // the bot's hand-over message to the customer
		}
		if m.Incoming && strings.TrimSpace(m.Content) != "" {
			asked = append([]string{strings.TrimSpace(m.Content)}, asked...)
		}
	}
	g.Asked = cut(strings.Join(asked, "\n"), maxAsked)
	for _, m := range msgs[note+1:] {
		if m.Outgoing && !m.Private && !m.FromBot && strings.TrimSpace(m.Content) != "" {
			g.StaffReply = cut(strings.TrimSpace(m.Content), maxReply)
			break
		}
	}
	return g, true
}

// wanted returns the "ลูกค้าต้องการ" line of a note, without its label.
func wanted(note string) string {
	for _, line := range strings.Split(note, "\n") {
		line = strings.TrimSpace(strings.ReplaceAll(line, "**", ""))
		if v, ok := strings.CutPrefix(line, wantedPrefix); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// cut shortens s to at most n characters, ending with "…" when cut.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// Source reads and relabels gap chats in Chatwoot (chatwoot.Reader).
type Source interface {
	GapChats(ctx context.Context, accountID int64, limit int) ([]Chat, error)
	ChatMessages(ctx context.Context, accountID, conversationID int64) ([]Message, error)
	ReplaceLabel(ctx context.Context, accountID, conversationID int64, from, to string) error
}

// ErrNotAGap means the chat no longer carries the gap label.
var ErrNotAGap = errors.New("this chat is no longer on the gap list")

// Finder lists open gaps.
type Finder struct {
	Source Source
	// Workers bounds the message reads running at once; 0 means 4.
	Workers int
}

// List returns the account's open gaps, newest first, at most limit. A chat
// whose messages cannot be read is still listed, without its question.
func (f *Finder) List(ctx context.Context, accountID int64, limit int) ([]Gap, error) {
	chats, err := f.Source.GapChats(ctx, accountID, limit)
	if err != nil {
		return nil, err
	}
	workers := f.Workers
	if workers <= 0 {
		workers = 4
	}
	out := make([]Gap, len(chats))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, c := range chats {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = f.read(ctx, accountID, c)
		}()
	}
	wg.Wait()
	return out, nil
}

// Done takes the chat off the list: "kb-gap" becomes "kb-gap-done".
func (f *Finder) Done(ctx context.Context, accountID, conversationID int64) error {
	return f.Source.ReplaceLabel(ctx, accountID, conversationID, Label, DoneLabel)
}

func (f *Finder) read(ctx context.Context, accountID int64, c Chat) Gap {
	g := Gap{At: c.LastActivity}
	if msgs, err := f.Source.ChatMessages(ctx, accountID, c.ID); err == nil {
		if found, ok := Read(msgs); ok {
			g = found
		}
	}
	g.ConversationID, g.InboxID = c.ID, c.InboxID
	return g
}
