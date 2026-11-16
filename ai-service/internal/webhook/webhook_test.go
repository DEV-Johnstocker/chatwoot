package webhook

import (
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"
)

const samplePayload = `{
  "event": "message_created",
  "id": 42,
  "content": "สวัสดีค่ะ",
  "message_type": "incoming",
  "private": false,
  "account": {"id": 1, "name": "Acme"},
  "inbox": {"id": 3, "name": "LINE"},
  "sender": {"id": 9, "name": "Somchai", "type": "contact"},
  "conversation": {"id": 7, "status": "pending", "inbox_id": 3}
}`

func TestParse_ReadsMessageFields(t *testing.T) {
	// Arrange
	body := []byte(samplePayload)

	// Act
	ev, err := Parse(body)

	// Assert
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ev.ID != 42 || ev.Account.ID != 1 || ev.Conversation.ID != 7 || ev.Conversation.Status != "pending" ||
		ev.MessageType != "incoming" || ev.Sender.Type != "contact" || ev.Content != "สวัสดีค่ะ" {
		t.Errorf("unexpected event: %+v", ev)
	}
	if ev.ConversationID() != 7 {
		t.Errorf("ConversationID = %d", ev.ConversationID())
	}
}

func TestClassify(t *testing.T) {
	base := func() Event {
		ev, _ := Parse([]byte(samplePayload))
		return ev
	}
	cases := []struct {
		name   string
		mutate func(*Event)
		want   Decision
	}{
		{"pending customer message", func(*Event) {}, Reply},
		{"open conversation (human took over)", func(e *Event) { e.Conversation.Status = "open" }, Record},
		{"private note", func(e *Event) { e.Private = true }, Ignore},
		{"empty content, no attachment", func(e *Event) { e.Content = "  " }, Ignore},
		{"attachment only (photo, voice, slip)", func(e *Event) { e.Content = ""; e.Attachments = []Attachment{{FileType: "image"}} }, HandOff},
		{"attachment only, human took over", func(e *Event) {
			e.Content = ""
			e.Attachments = []Attachment{{FileType: "audio"}}
			e.Conversation.Status = "open"
		}, Ignore},
		{"agent sends only a file", func(e *Event) {
			e.Content = ""
			e.MessageType = "outgoing"
			e.Sender.Type = "user"
			e.Attachments = []Attachment{{FileType: "image"}}
		}, Pause},
		{"staff private note", func(e *Event) { e.Private = true; e.MessageType = "outgoing"; e.Sender.Type = "user" }, Pause},
		{"bot's own private note", func(e *Event) { e.Private = true; e.MessageType = "outgoing"; e.Sender.Type = "agent_bot" }, Ignore},
		{"staff #fix note", func(e *Event) {
			e.Private, e.MessageType, e.Sender.Type, e.Content = true, "outgoing", "user", "#fix 3,900 บาท"
		}, Correct},
		{"staff #fix note in an open chat", func(e *Event) {
			e.Private, e.MessageType, e.Sender.Type, e.Content = true, "outgoing", "user", "#แก้ 3,900"
			e.Conversation.Status = "open"
		}, Correct},
		{"bot's own note starting #fix", func(e *Event) {
			e.Private, e.MessageType, e.Sender.Type, e.Content = true, "outgoing", "agent_bot", "#fix x"
		}, Ignore},
		{"automation note starting #fix", func(e *Event) {
			e.Private, e.MessageType, e.Sender.Type, e.Content = true, "outgoing", "", "#fix x"
		}, Pause},
		{"customer writes #fix", func(e *Event) { e.Content = "#fix ราคา" }, Reply},
		{"staff public reply starting #fix", func(e *Event) {
			e.MessageType, e.Sender.Type, e.Content = "outgoing", "user", "#fix x"
			e.Conversation.Status = "open"
		}, Record},
		{"staff opened the chat", func(e *Event) { e.Event = "conversation_status_changed"; e.Status = "open" }, Pause},
		{"chat back to the bot", func(e *Event) { e.Event = "conversation_status_changed"; e.Status = "pending" }, Ignore},
		{"conversation updated, staff", func(e *Event) { e.Event = "conversation_updated"; e.Status = "open" }, Pause},
		{"text with attachment", func(e *Event) { e.Attachments = []Attachment{{FileType: "image"}} }, Reply},
		{"bot's own reply", func(e *Event) { e.MessageType = "outgoing"; e.Sender.Type = "agent_bot" }, Ignore},
		{"human agent reply", func(e *Event) { e.MessageType = "outgoing"; e.Sender.Type = "user" }, Record},
		{"activity message", func(e *Event) { e.MessageType = "activity" }, Ignore},
		{"message_updated", func(e *Event) { e.Event = "message_updated" }, Ignore},
		{"conversation resolved", func(e *Event) { e.Event = "conversation_resolved" }, Forget},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			ev := base()
			tc.mutate(&ev)

			// Act
			got := Classify(ev)

			// Assert
			if got != tc.want {
				t.Errorf("Classify = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParse_ReadsAttachmentsAndDescribesThem(t *testing.T) {
	// Arrange
	body := []byte(`{"event":"message_created","id":5,"content":"โอนแล้วค่ะ","message_type":"incoming",
		"attachments":[{"id":1,"file_type":"image","data_url":"https://x/y.png"},{"id":2,"file_type":"file"}],
		"account":{"id":1},"inbox":{"id":1},"conversation":{"id":2,"status":"pending"}}`)

	// Act
	ev, err := Parse(body)

	// Assert
	if err != nil || len(ev.Attachments) != 2 || ev.Attachments[0].FileType != "image" {
		t.Fatalf("attachments = %+v, err %v", ev.Attachments, err)
	}
	if got := ev.AttachmentNote(); got != "[The customer attached 2 files: image, file]" {
		t.Errorf("AttachmentNote() = %q", got)
	}
	if (Event{}).AttachmentNote() != "" {
		t.Error("no attachments must give an empty note")
	}
}

func TestConversationID_ForConversationEvent(t *testing.T) {
	// Arrange
	ev := Event{Event: "conversation_resolved", ID: 11}

	// Act
	id := ev.ConversationID()

	// Assert
	if id != 11 {
		t.Errorf("ConversationID = %d, want 11", id)
	}
}

func TestVerify(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := []byte(samplePayload)
	ts := strconv.FormatInt(now.Unix(), 10)
	valid := "sha256=" + hex.EncodeToString(Sign("s3cret", ts, body))

	cases := []struct {
		name    string
		ts, sig string
		body    []byte
		wantErr error
	}{
		{"valid", ts, valid, body, nil},
		{"missing headers", "", "", body, ErrMissingSignature},
		{"tampered body", ts, valid, []byte(`{"event":"x"}`), ErrBadSignature},
		{"wrong prefix", ts, "md5=abc", body, ErrBadSignature},
		{"not hex", ts, "sha256=zz", body, ErrBadSignature},
		{"stale", strconv.FormatInt(now.Add(-10*time.Minute).Unix(), 10), valid, body, ErrStaleTimestamp},
		{"garbage timestamp", "abc", valid, body, ErrStaleTimestamp},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange: inputs in table

			// Act
			err := Verify("s3cret", tc.ts, tc.sig, tc.body, now)

			// Assert
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Verify err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestConversationFields(t *testing.T) {
	// Arrange
	ev, err := Parse([]byte(`{"event":"conversation_updated","id":52,"inbox_id":2,"labels":["paid","bot-handoff"],
		"channel":"Channel::FacebookPage","updated_at":1794700000.5,
		"custom_attributes":{"paid_amount":"3,900"},"changed_attributes":[{"label_list":{}},{"custom_attributes":{}}]}`))

	// Act
	names, ok := ev.Changed()
	labels, labelsOK := ev.LabelList()

	// Assert
	if err != nil || !labelsOK || len(labels) != 2 || string(ev.Attribute("paid_amount")) != `"3,900"` || ev.ConversationInbox() != 2 {
		t.Fatalf("err=%v labels=%v attr=%s inbox=%d", err, labels, ev.Attribute("paid_amount"), ev.ConversationInbox())
	}
	if ev.Channel() != "Channel::FacebookPage" || ev.UpdatedTime().Unix() != 1794700000 {
		t.Fatalf("channel=%q updated=%v", ev.Channel(), ev.UpdatedTime())
	}
	if !ok || len(names) != 2 {
		t.Fatalf("changed = %v %v", names, ok)
	}
}

func TestConversationFields_OddShapesNeverFailTheEvent(t *testing.T) {
	// Arrange: shapes Chatwoot might send elsewhere must not break parsing.
	body := `{"event":"message_created","id":9,"content":"hi","labels":[{"title":"x"}],"custom_attributes":"x","inbox_id":"2","changed_attributes":{"a":1},"channel":{"x":1}}`

	// Act
	ev, err := Parse([]byte(body))

	// Assert
	if err != nil || ev.Content != "hi" {
		t.Fatalf("err = %v", err)
	}
	if _, ok := ev.LabelList(); ok || ev.Attribute("paid_amount") != nil || ev.ConversationInbox() != 0 || ev.Channel() != "" {
		t.Fatal("odd shapes should read as empty")
	}
	if _, ok := ev.Changed(); ok {
		t.Fatal("odd changed_attributes should read as unknown")
	}
	missing, _ := Parse([]byte(`{"event":"conversation_updated","id":1}`))
	if _, ok := missing.LabelList(); ok {
		t.Fatal("absent labels must read as unknown, not empty")
	}
}
