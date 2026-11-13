package server

import (
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/webhook"
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

func TestWebhook_StaleOrMalformedRecordsNoChat(t *testing.T) {
	// Arrange
	q, chats := &fakeQueue{}, &fakeChats{}
	h := chatsHandler(q, chats)
	body := fmt.Sprintf(psidMsg, "incoming", "false")
	stale := signedRequest(body)
	old := strconv.FormatInt(fixedNow.Add(-time.Hour).Unix(), 10)
	stale.Header.Set("X-Chatwoot-Timestamp", old)
	stale.Header.Set("X-Chatwoot-Signature", "sha256="+hex.EncodeToString(webhook.Sign(secret, old, []byte(body))))

	// Act
	staleCode := do(h, stale)
	badCode := do(h, signedRequest(`{"event":"message_created","conversation":{"contact_inbox":`))

	// Assert
	if staleCode != http.StatusUnauthorized || badCode != http.StatusBadRequest || len(chats.got) != 0 {
		t.Fatalf("stale=%d malformed=%d recorded=%v, want 401, 400 and nothing", staleCode, badCode, chats.got)
	}
}
