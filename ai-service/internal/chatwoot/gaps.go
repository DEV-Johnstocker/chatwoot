package chatwoot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
)

// The gap report (/admin/gaps) reads kb-gap chats with the reader token: the
// bot token can neither list conversations nor read messages.
// Issue: DEV-Johnstocker/chatwoot#1

const (
	conversationsPerPage = 25 // Chatwoot's conversation list page size
	maxGapPages          = 4  // at most 100 gap chats are read
	messagesPerPage      = 20 // Chatwoot's message list page size
	maxMessagePages      = 3  // look back at most 60 messages for the gap note
)

// GapChats lists conversations labelled kb-gap in any status, the most
// recently active first. limit <= 0 reads up to maxGapPages pages.
func (r *Reader) GapChats(ctx context.Context, accountID int64, limit int) ([]gaps.Chat, error) {
	var out []gaps.Chat
	for page := 1; page <= maxGapPages; page++ {
		q := url.Values{
			"status": {"all"}, "assignee_type": {"all"}, "labels[]": {gaps.Label},
			"sort_by": {"last_activity_at_desc"}, "page": {fmt.Sprint(page)},
		}
		var res struct {
			Data struct {
				Payload []struct {
					ID             int64    `json:"id"`
					InboxID        int64    `json:"inbox_id"`
					LastActivityAt int64    `json:"last_activity_at"`
					Labels         []string `json:"labels"`
				} `json:"payload"`
			} `json:"data"`
		}
		path := fmt.Sprintf("/api/v1/accounts/%d/conversations?%s", accountID, q.Encode())
		if err := r.getJSON(ctx, path, nil, &res); err != nil {
			return out, err
		}
		for _, c := range res.Data.Payload {
			if c.Labels != nil && !slices.Contains(c.Labels, gaps.Label) {
				continue // the list filter should not let these through; be safe
			}
			out = append(out, gaps.Chat{ID: c.ID, InboxID: c.InboxID, LastActivity: time.Unix(c.LastActivityAt, 0)})
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
		if len(res.Data.Payload) < conversationsPerPage {
			break
		}
	}
	return out, nil
}

type gapMessage struct {
	ID          int64  `json:"id"`
	MessageType int    `json:"message_type"` // 0 incoming, 1 outgoing, 2 activity, 3 template
	Private     bool   `json:"private"`
	Content     string `json:"content"`
	CreatedAt   int64  `json:"created_at"`
	Sender      *struct {
		Type string `json:"type"`
	} `json:"sender"`
	// Raw, so an odd shape on one message never fails the whole page.
	ContentAttributes json.RawMessage `json:"content_attributes"`
}

// echo reports Chatwoot's external_echo flag; false when absent or odd.
func (m gapMessage) echo() bool {
	var a struct {
		ExternalEcho bool `json:"external_echo"`
	}
	return json.Unmarshal(m.ContentAttributes, &a) == nil && a.ExternalEcho
}

func (m gapMessage) toMessage() gaps.Message {
	return gaps.Message{
		ID: m.ID, Incoming: m.MessageType == 0, Outgoing: m.MessageType == 1, Private: m.Private,
		FromBot:   m.Sender != nil && m.Sender.Type == "agent_bot",
		FromStaff: m.Sender != nil && m.Sender.Type == "user",
		Echo:      m.echo(),
		Content:   m.Content, At: time.Unix(m.CreatedAt, 0),
	}
}

// ChatMessages returns a conversation's latest messages, oldest first. It
// reads older pages until it has the bot's gap note, at most maxMessagePages.
func (r *Reader) ChatMessages(ctx context.Context, accountID, conversationID int64) ([]gaps.Message, error) {
	return r.messages(ctx, accountID, conversationID, maxMessagePages, hasGapNote)
}

// AllMessages returns up to pages pages of a conversation's messages (20
// each), oldest first: the whole chat when it is shorter.
func (r *Reader) AllMessages(ctx context.Context, accountID, conversationID int64, pages int) ([]gaps.Message, error) {
	return r.messages(ctx, accountID, conversationID, pages, nil)
}

// messages reads pages back from the newest until a page is short, stop
// holds on a page, or maxPages were read.
func (r *Reader) messages(ctx context.Context, accountID, conversationID int64, maxPages int, stop func([]gaps.Message) bool) ([]gaps.Message, error) {
	var all []gaps.Message
	before := int64(0)
	for page := 0; page < maxPages; page++ {
		path := fmt.Sprintf("/api/v1/accounts/%d/conversations/%d/messages", accountID, conversationID)
		if before > 0 {
			path += "?before=" + fmt.Sprint(before)
		}
		var res struct {
			Payload []gapMessage `json:"payload"`
		}
		if err := r.getJSON(ctx, path, nil, &res); err != nil {
			return all, err
		}
		got := make([]gaps.Message, 0, len(res.Payload))
		for _, m := range res.Payload {
			got = append(got, m.toMessage())
		}
		all = append(got, all...)
		if len(res.Payload) < messagesPerPage || (stop != nil && stop(got)) {
			break
		}
		before = res.Payload[0].ID
	}
	return all, nil
}

func hasGapNote(msgs []gaps.Message) bool {
	for _, m := range msgs {
		if m.Private && m.FromBot && strings.Contains(m.Content, gaps.NoteTitle) {
			return true
		}
	}
	return false
}

// ReplaceLabel swaps one label for another and keeps the rest (Chatwoot's
// labels call replaces the whole list). Without the from label it does
// nothing and returns gaps.ErrNotAGap, so a second click changes nothing.
// Chatwoot has no compare-and-set for labels: a label staff change in the
// moment between the read and the write is lost (accepted: rare, and only
// labels on one chat).
func (r *Reader) ReplaceLabel(ctx context.Context, accountID, conversationID int64, from, to string) error {
	path := fmt.Sprintf("/api/v1/accounts/%d/conversations/%d/labels", accountID, conversationID)
	var current struct {
		Payload []string `json:"payload"`
	}
	if err := r.getJSON(ctx, path, nil, &current); err != nil {
		return err
	}
	if !slices.Contains(current.Payload, from) {
		return gaps.ErrNotAGap
	}
	labels := make([]string, 0, len(current.Payload)+1)
	for _, l := range current.Payload {
		if l != from && l != to {
			labels = append(labels, l)
		}
	}
	labels = append(labels, to)
	var ignored struct{}
	return r.getJSON(ctx, path, map[string]any{"labels": labels}, &ignored)
}
