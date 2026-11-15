// Package corrections turns a staff member's "#fix" note in a Chatwoot chat
// into a knowledge-base FAQ that waits for the owner's approval.
//
// Staff see wrong bot answers every day. Instead of retyping the fix on
// /admin, they write a private note in the chat:
//
//	#fix The course costs 3,900 baht until 30 Nov.
//
// The question is the customer's message the bot last answered before the
// note; the answer is the note's text. The proposal lands on /admin →
// Approvals, where the owner can edit both before approving. Nothing reaches
// customers until then. Issue: DEV-Johnstocker/chatwoot#1
package corrections

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/pending"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/ratings"
)

// Tags start a correction note. The Thai ones mean "fix".
var Tags = []string{"#fix", "#แก้ไข", "#แก้"}

// Limits on what a note can propose.
const (
	MaxAnswer   = 4000
	MaxBotReply = 2000
	MaxName     = 100
)

// Errors, each answered with its own note in the chat.
var (
	ErrNoAnswer   = errors.New("no answer after the tag")
	ErrNoQuestion = errors.New("no customer question before the note")
)

// Parse reports whether a note is a correction and returns the answer
// written after the tag (possibly empty). The tag must open the note and be
// followed by a space, a line break or nothing; a leading backslash (the
// Chatwoot editor escapes a "#" at the start of a line) is allowed.
func Parse(content string) (answer string, ok bool) {
	s := strings.TrimSpace(content)
	s = strings.TrimPrefix(s, `\`)
	for _, tag := range Tags {
		if len(s) < len(tag) || !strings.EqualFold(s[:len(tag)], tag) {
			continue
		}
		rest := s[len(tag):]
		if r := firstRune(rest); r != 0 && !unicode.IsSpace(r) && r != ':' {
			continue // "#fixed", "#แก้ว": another word
		}
		rest = strings.TrimPrefix(strings.TrimSpace(rest), ":")
		return strings.TrimSpace(rest), true
	}
	return "", false
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

// Messages reads a chat's newest messages (chatwoot.Reader).
type Messages interface {
	LatestMessages(ctx context.Context, accountID, conversationID int64) ([]gaps.Message, error)
}

// Note is one correction note as the webhook delivered it.
type Note struct {
	AccountID      int64
	ConversationID int64
	NoteID         int64  // the note's message id
	Staff          string // who wrote it
	Answer         string // the text after the tag
	// Memory is the bot's own memory of the chat, used when Chatwoot cannot
	// be read (no reader token, or the read failed).
	Memory []memory.Turn
}

// Service proposes corrections.
type Service struct {
	Pending  pending.Store
	Messages Messages // optional; nil uses only the bot's memory
}

// Key is the knowledge key a note's FAQ gets: one per note, so two notes
// never overwrite each other and a redelivered webhook updates the same one.
func Key(conversationID, noteID int64) string {
	return "staff-fix-" + strconv.FormatInt(conversationID, 10) + "-" + strconv.FormatInt(noteID, 10)
}

// proposal is the stored change: the fields mcp's document proposals use,
// plus the staff marker. Base "" means the key must still be new.
type proposal struct {
	Key     string            `json:"key"`
	Kind    string            `json:"kind"`
	Title   string            `json:"title"`
	Content string            `json:"content"`
	Base    *string           `json:"base"`
	Staff   *pending.StaffFix `json:"staff"`
}

// Propose stores the correction for approval and returns its id.
func (s *Service) Propose(ctx context.Context, n Note) (int64, error) {
	answer := ratings.Cut(clean(n.Answer), MaxAnswer)
	if answer == "" {
		return 0, ErrNoAnswer
	}
	question, botReply := s.lastExchange(ctx, n)
	question = ratings.Cut(clean(question), knowledge.MaxTitleRunes)
	if question == "" {
		return 0, ErrNoQuestion
	}
	base := ""
	key := Key(n.ConversationID, n.NoteID)
	raw, err := json.Marshal(proposal{Key: key, Kind: "faq", Title: question, Content: answer, Base: &base,
		Staff: &pending.StaffFix{Name: ratings.Cut(clean(n.Staff), MaxName), ConversationID: n.ConversationID,
			NoteID: n.NoteID, BotReply: ratings.Cut(clean(botReply), MaxBotReply)}})
	if err != nil {
		return 0, err
	}
	return s.Pending.Propose(ctx, pending.Change{AccountID: n.AccountID, Kind: pending.KindDocument, Target: key,
		Proposed: raw, Actor: Actor(n.Staff, n.ConversationID)})
}

// Actor names a correction's author on the Approvals page.
func Actor(staff string, conversationID int64) string {
	name := ratings.Cut(clean(staff), MaxName)
	if name == "" {
		name = "Staff"
	}
	return name + " (chat #" + strconv.FormatInt(conversationID, 10) + ")"
}

// lastExchange finds the customer question and the bot reply the note
// corrects: from Chatwoot when it can be read, else from the bot's memory.
func (s *Service) lastExchange(ctx context.Context, n Note) (question, botReply string) {
	if s.Messages != nil {
		if msgs, err := s.Messages.LatestMessages(ctx, n.AccountID, n.ConversationID); err == nil {
			if q, b := fromMessages(msgs, n.NoteID); q != "" {
				return q, b
			}
		}
	}
	return fromMemory(n.Memory)
}

// fromMessages is the last bot reply before the note and the customer
// messages it answered; with no bot reply, the customer's latest messages.
func fromMessages(msgs []gaps.Message, noteID int64) (question, botReply string) {
	before := make([]gaps.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.ID < noteID {
			before = append(before, m)
		}
	}
	if rs := ratings.Replies(before); len(rs) > 0 {
		if last := rs[len(rs)-1]; last.Customer != "" {
			return last.Customer, last.Bot
		}
	}
	var asked []string
	for _, m := range before {
		switch {
		case m.Private:
		case m.Incoming:
			if t := strings.TrimSpace(m.Content); t != "" {
				asked = append(asked, t)
			}
		case m.Outgoing:
			asked = nil
		}
	}
	return strings.Join(asked, "\n"), ""
}

// fromMemory does the same from the bot's memory of the chat.
func fromMemory(turns []memory.Turn) (question, botReply string) {
	var asked []string
	for _, t := range turns {
		switch {
		case t.Role == "user":
			asked = append(asked, strings.TrimSpace(t.Content))
		case strings.HasPrefix(t.Content, memory.PageMessagePrefix):
		default:
			if len(asked) > 0 {
				question, botReply = strings.Join(asked, "\n"), t.Content
			}
			asked = nil
		}
	}
	if question == "" && len(asked) > 0 { // no bot reply yet: the customer's latest messages
		return strings.Join(asked, "\n"), ""
	}
	return question, botReply
}

// clean makes text safe to store: valid UTF-8, no NUL, trimmed.
func clean(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ToValidUTF8(s, "�"), "\x00", ""))
}
