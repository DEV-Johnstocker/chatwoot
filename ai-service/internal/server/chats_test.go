package server

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
)

type fakeChats struct{ got []string }

func (f *fakeChats) Saw(acct int64, id string, _ time.Time) {
	f.got = append(f.got, id)
}

func chatsHandler(q *fakeQueue, c *fakeChats) http.Handler {
	h := &Handler{
		Secret: secret, Queue: q, Chats: c,
		Seen:   memory.NewSeen(10*time.Minute, nil),
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now:    func() time.Time { return fixedNow },
	}
	return h.Routes()
}

const psidMsg = `{"event":"message_created","id":43,"content":"hi","message_type":"%s","private":%s,
"account":{"id":1},"inbox":{"id":3},"sender":{"id":9,"type":"contact"},
"conversation":{"id":7,"status":"open","contact_inbox":{"source_id":"2898"}}}`

func TestWebhook_RecordsCustomerChatMessagesOnly(t *testing.T) {
	cases := map[string]struct {
		kind, private string
		want          int
	}{
		"customer message, staff-handled chat": {"incoming", "false", 1},
		"page or bot message":                  {"outgoing", "false", 0},
		"private note":                         {"outgoing", "true", 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			q, chats := &fakeQueue{}, &fakeChats{}
			h := chatsHandler(q, chats)

			// Act
			do(h, signedRequest(fmt.Sprintf(psidMsg, c.kind, c.private)))

			// Assert
			if len(chats.got) != c.want || (c.want == 1 && chats.got[0] != "2898") {
				t.Errorf("recorded %v, want %d", chats.got, c.want)
			}
		})
	}
}

func TestWebhook_ForgedRequestRecordsNoChat(t *testing.T) {
	// Arrange
	q, chats := &fakeQueue{}, &fakeChats{}
	h := chatsHandler(q, chats)
	req := signedRequest(fmt.Sprintf(psidMsg, "incoming", "false"))
	req.Header.Set("X-Chatwoot-Signature", "sha256=00")

	// Act
	code := do(h, req)

	// Assert
	if code != http.StatusUnauthorized || len(chats.got) != 0 {
		t.Errorf("code=%d recorded=%v, want 401 and nothing", code, chats.got)
	}
}
