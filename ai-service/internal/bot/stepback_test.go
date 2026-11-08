package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/webhook"
)

// staffReply is an outgoing public message from someone other than the bot:
// an admin in Chatwoot, or a Facebook echo of an admin replying in Messenger
// (Chatwoot stores those with no sender).
func staffReply(status, senderType string, files bool) Job {
	ev := webhook.Event{
		Event: "message_created", ID: 30, Content: "แอดมินตอบเองครับ", MessageType: "outgoing",
		Account: webhook.Account{ID: 1}, Conversation: webhook.Conversation{ID: 7, Status: status},
		Sender: webhook.Sender{Type: senderType},
	}
	if files {
		ev.Content = ""
		ev.Attachments = []webhook.Attachment{{FileType: "image"}}
	}
	return Job{Event: ev, Decision: webhook.Classify(ev)}
}

func TestBotStepsBackWhenAnAdminRepliesOutsideChatwoot(t *testing.T) {
	cases := map[string]Job{
		"messenger echo (no sender)": staffReply("pending", "", false),
		"staff typed in chatwoot":    staffReply("pending", "user", false),
		"messenger echo of a photo":  staffReply("pending", "", true),
	}
	for name, job := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			cw := &fakeChatwoot{}
			b, _ := newBot(cw, &fakeLLM{reply: "x"})

			// Act
			b.Process(context.Background(), job)

			// Assert
			got := cw.joined()
			if strings.Contains(got, "msg:") {
				t.Errorf("the customer must not get a message: %s", got)
			}
			if !strings.Contains(got, "note:🤖 "+reasonLabels[ReasonStaffReplied]+"\n"+ReasonStaffReplied+" | handoff") {
				t.Errorf("want a staff note and the chat moved to staff, got %s", got)
			}
		})
	}
}

func TestStaffReplyOnAStaffChatChangesNothing(t *testing.T) {
	// Arrange
	cw := &fakeChatwoot{}
	b, h := newBot(cw, &fakeLLM{reply: "x"})

	// Act
	b.Process(context.Background(), staffReply("open", "user", false))

	// Assert
	if got := cw.joined(); got != "" {
		t.Errorf("no calls expected on a chat staff already own, got %s", got)
	}
	if turns := h.Get(1, 7); len(turns) != 1 || turns[0].Role != "assistant" {
		t.Errorf("the staff reply should still be remembered as context: %+v", turns)
	}
}
