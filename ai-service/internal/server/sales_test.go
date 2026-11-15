package server

import (
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/webhook"
)

type fakeSales struct{ got []webhook.Event }

func (f *fakeSales) Observe(ev webhook.Event) { f.got = append(f.got, ev) }

func salesHandler(q *fakeQueue, s *fakeSales) http.Handler {
	h := &Handler{
		Secret: secret, Queue: q, Sales: s,
		Seen:   memory.NewSeen(10*time.Minute, nil),
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now:    func() time.Time { return fixedNow },
	}
	return h.Routes()
}

const paidUpdate = `{"event":"conversation_updated","id":52,"status":"open","account":{"id":1},"labels":["paid"],
"custom_attributes":{"paid_amount":3900},"changed_attributes":[{"label_list":{}}]}`

func TestWebhook_SignedUpdatesReachSalesEvenWhenRepeated(t *testing.T) {
	// Arrange: the queue drops a repeated conversation_updated; sales must not.
	q, s := &fakeQueue{}, &fakeSales{}
	h := salesHandler(q, s)

	// Act
	do(h, signedRequest(paidUpdate))
	do(h, signedRequest(paidUpdate))

	// Assert
	if len(s.got) != 2 || s.got[0].ID != 52 || len(s.got[0].LabelList()) != 1 {
		t.Fatalf("observed %d events", len(s.got))
	}
}

func TestWebhook_ForgedUpdateReachesNoSales(t *testing.T) {
	// Arrange
	q, s := &fakeQueue{}, &fakeSales{}
	h := salesHandler(q, s)
	req := signedRequest(paidUpdate)
	req.Header.Set("X-Chatwoot-Signature", "sha256=00")

	// Act
	code := do(h, req)

	// Assert
	if code != http.StatusUnauthorized || len(s.got) != 0 {
		t.Fatalf("code=%d observed=%d", code, len(s.got))
	}
}
