// Package webhook parses, verifies and filters Chatwoot Agent Bot webhooks.
//
// Payload shape verified against Chatwoot source (commit 656c814):
// app/models/message.rb#webhook_data and
// app/presenters/conversations/event_data_presenter.rb#webhook_data.
package webhook

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/corrections"
)

// Event is the subset of an Agent Bot webhook payload that ai-service uses.
// Message events carry message fields at the top level; conversation events
// (conversation_resolved, ...) carry conversation fields at the top level.
type Event struct {
	Event       string `json:"event"`
	ID          int64  `json:"id"` // message id (message events) or conversation display id
	Content     string `json:"content"`
	MessageType string `json:"message_type"` // incoming | outgoing | activity | template
	Private     bool   `json:"private"`
	Status      string `json:"status"` // conversation events only
	// UpdatedAt is the conversation's updated_at on conversation events (a
	// float of seconds). A redelivery repeats it; a later status change does
	// not. Kept raw so a message event's own fields never fail to parse.
	UpdatedAt    json.RawMessage `json:"updated_at"`
	Account      Account         `json:"account"`
	Inbox        Inbox           `json:"inbox"`
	Sender       Sender          `json:"sender"`
	Conversation Conversation    `json:"conversation"`
	Attachments  []Attachment    `json:"attachments"` // present only when the message has files
	// SourceID is the channel's own id for the message: for Messenger, Meta's
	// message id (an echo of a page's message carries the id Meta returned).
	SourceID string `json:"source_id"`
	// Conversation events only: the chat's labels, custom attributes, inbox
	// and what changed. Kept raw so an unexpected shape never fails the
	// whole event; LabelList and Attribute read them.
	RawLabels         json.RawMessage `json:"labels"`
	RawChannel        json.RawMessage `json:"channel"`
	RawAttributes     json.RawMessage `json:"custom_attributes"`
	InboxID           json.RawMessage `json:"inbox_id"`
	ChangedAttributes json.RawMessage `json:"changed_attributes"`
	// ContentAttributes carries Chatwoot's message flags; external_echo marks
	// a reply sent outside Chatwoot (e.g. from the Messenger app).
	ContentAttributes struct {
		ExternalEcho bool `json:"external_echo"`
	} `json:"content_attributes"`
}

// Attachment is a file on a message (photo, voice note, document, ...).
type Attachment struct {
	FileType  string `json:"file_type"` // image | audio | video | file | location | ...
	DataURL   string `json:"data_url"`  // download URL on the Chatwoot host
	Extension string `json:"extension"` // e.g. m4a, ogg, png (may be empty)
}

// AttachmentNote tells the model that files came with the text, since it
// cannot see them; empty when there are none.
func (ev Event) AttachmentNote() string {
	if len(ev.Attachments) == 0 {
		return ""
	}
	types := make([]string, len(ev.Attachments))
	for i, a := range ev.Attachments {
		types[i] = a.FileType
	}
	noun := "file"
	if len(types) > 1 {
		noun = "files"
	}
	return fmt.Sprintf("[The customer attached %d %s: %s]", len(types), noun, strings.Join(types, ", "))
}

// LabelList is a conversation event's labels; ok is false when the field is
// absent or malformed (then the labels are unknown, not empty).
func (ev Event) LabelList() (labels []string, ok bool) {
	if len(ev.RawLabels) == 0 || json.Unmarshal(ev.RawLabels, &labels) != nil || labels == nil && string(ev.RawLabels) == "null" {
		return nil, false
	}
	return labels, true
}

// Channel is a conversation event's channel type, e.g. "Channel::FacebookPage";
// "" when absent or odd.
func (ev Event) Channel() string {
	var c string
	if json.Unmarshal(ev.RawChannel, &c) != nil {
		return ""
	}
	return c
}

// UpdatedTime is a conversation event's updated_at; zero when absent or odd.
func (ev Event) UpdatedTime() time.Time {
	var f float64
	if json.Unmarshal(ev.UpdatedAt, &f) != nil || f <= 0 || f > 1e11 {
		return time.Time{}
	}
	sec, frac := math.Modf(f)
	return time.Unix(int64(sec), int64(frac*1e9))
}

// Attribute is one of a conversation event's custom attributes, raw; nil
// when absent or malformed.
func (ev Event) Attribute(name string) json.RawMessage {
	var attrs map[string]json.RawMessage
	if json.Unmarshal(ev.RawAttributes, &attrs) != nil {
		return nil
	}
	return attrs[name]
}

// ConversationInbox is a conversation event's inbox id; 0 when absent.
func (ev Event) ConversationInbox() int64 {
	var id int64
	if json.Unmarshal(ev.InboxID, &id) != nil || id < 0 {
		return 0
	}
	return id
}

// Changed lists the attribute names a conversation_updated event changed;
// ok is false when Chatwoot did not say (then assume anything changed).
func (ev Event) Changed() (names []string, ok bool) {
	var list []map[string]json.RawMessage
	if json.Unmarshal(ev.ChangedAttributes, &list) != nil || len(list) == 0 {
		return nil, false
	}
	for _, m := range list {
		for k := range m {
			names = append(names, k)
		}
	}
	return names, true
}

// Account identifies the Chatwoot account.
type Account struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Inbox identifies the Chatwoot inbox (channel).
type Inbox struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Sender is the message author. Type is "contact", "user" or "agent_bot".
type Sender struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"` // a Chatwoot user's display name; personal data, never logged
}

// Conversation is the conversation a message belongs to.
// ID is the display id used in REST API paths.
type Conversation struct {
	ID     int64  `json:"id"`
	Status string `json:"status"` // open | resolved | pending | snoozed
	// Channel is the inbox's channel type, e.g. "Channel::FacebookPage".
	Channel string `json:"channel"`
	// ContactInbox.SourceID is the customer's id in the channel: for
	// Messenger, the page-scoped id Facebook also gives the comment's author.
	// HmacVerified is true when a website widget visitor's identity was
	// signed by the site (Chatwoot's identity validation).
	ContactInbox struct {
		SourceID     string `json:"source_id"`
		HmacVerified bool   `json:"hmac_verified"`
	} `json:"contact_inbox"`
}

// Parse decodes a webhook body.
func Parse(body []byte) (Event, error) {
	var ev Event
	err := json.Unmarshal(body, &ev)
	return ev, err
}

// Decision says what to do with an event.
type Decision int

const (
	// Ignore means no action.
	Ignore Decision = iota
	// Reply means the bot should answer this customer message.
	Reply
	// Record means remember the message as context but do not answer
	// (e.g. a human agent's public reply).
	Record
	// Forget means drop stored state for the conversation (resolved).
	Forget
	// HandOff means a customer sent only files (a payment slip, a photo, a
	// voice note) that the bot cannot read: acknowledge and pass to a human.
	HandOff
	// Pause means staff took the chat without a public reply (opened it, or
	// wrote a private note or sent a file): drop any reply the bot is waiting
	// to send.
	Pause
	// Correct means a staff member wrote a "#fix" private note: propose the
	// corrected answer for the owner's approval (internal/corrections). It
	// also pauses the bot, like any staff note.
	Correct
)

// Classify decides how to handle an event. It answers only public customer
// messages in pending conversations, which prevents loops on the bot's own
// replies, private notes and conversations a human has taken over.
func Classify(ev Event) Decision {
	switch ev.Event {
	case "conversation_resolved":
		return Forget
	case "conversation_status_changed", "conversation_updated":
		if ev.Status != "" && ev.Status != "pending" {
			return Pause
		}
		return Ignore
	case "message_created":
	default:
		return Ignore
	}
	staff := ev.MessageType == "outgoing" && ev.Sender.Type != "agent_bot"
	if ev.Private {
		if staff && ev.Sender.Type == "user" {
			if _, ok := corrections.Parse(ev.Content); ok {
				return Correct
			}
		}
		if staff {
			return Pause
		}
		return Ignore
	}
	if strings.TrimSpace(ev.Content) == "" {
		if ev.MessageType == "incoming" && len(ev.Attachments) > 0 && ev.Conversation.Status == "pending" {
			return HandOff
		}
		if staff {
			return Pause
		}
		return Ignore
	}
	switch ev.MessageType {
	case "incoming":
		if ev.Conversation.Status != "pending" {
			return Record
		}
		return Reply
	case "outgoing":
		if ev.Sender.Type == "agent_bot" {
			return Ignore // already recorded when we sent it
		}
		return Record
	default:
		return Ignore
	}
}

// ConversationID returns the conversation display id for any event type.
func (ev Event) ConversationID() int64 {
	if ev.Event == "message_created" || ev.Event == "message_updated" {
		return ev.Conversation.ID
	}
	return ev.ID
}
