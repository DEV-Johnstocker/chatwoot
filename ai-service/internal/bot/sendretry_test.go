package bot

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/chatwoot"
)

// flakyChatwoot fails the first `fails` SendMessage calls.
type flakyChatwoot struct {
	fakeChatwoot
	mu    sync.Mutex
	fails int
	err   error // returned while failing; nil means a network-style error
}

func (c *flakyChatwoot) SendMessage(ctx context.Context, a, conv int64, s string) error {
	c.mu.Lock()
	if c.fails > 0 {
		c.fails--
		c.mu.Unlock()
		_ = c.record("send-error")
		if c.err != nil {
			return c.err
		}
		return errors.New("connection reset")
	}
	c.mu.Unlock()
	return c.fakeChatwoot.SendMessage(ctx, a, conv, s)
}

func TestFailedSendIsRetriedOnce(t *testing.T) {
	// Arrange
	cw := &flakyChatwoot{fails: 1}
	b, h := newBot(&cw.fakeChatwoot, &fakeLLM{reply: "เปิด 9 โมงครับ"})
	b.opts.Chatwoot, b.sendRetryWait = cw, time.Millisecond

	// Act
	b.Process(context.Background(), customerMsg("เปิดกี่โมง"))

	// Assert
	if got := cw.joined(); got != "typing:on | send-error | msg:เปิด 9 โมงครับ | typing:off" {
		t.Fatalf("calls = %q", got)
	}
	if turns := h.Get(1, 7); turns[len(turns)-1].Content != "เปิด 9 โมงครับ" {
		t.Errorf("history = %+v", turns)
	}
}

func TestReplyThatCannotBeSentGoesToStaff(t *testing.T) {
	// Arrange: Chatwoot rejects every attempt to send the reply.
	cw := &flakyChatwoot{fails: 2}
	b, h := newBot(&cw.fakeChatwoot, &fakeLLM{reply: "เปิด 9 โมงครับ"})
	b.opts.Chatwoot, b.sendRetryWait = cw, time.Millisecond
	b.opts.NoteLLM = &noteLLM{reply: "เหตุผล: ไม่ควรถูกเรียก"}

	// Act
	b.Process(context.Background(), customerMsg("เปิดกี่โมง"))

	// Assert: staff see a note and the chat is handed over; the customer
	// gets no second message.
	got := cw.joined()
	want := "send-error | send-error | note:🤖 **บอทส่งต่อ:** " + reasonLabels[ReasonSendFailed] + "\n" + ReasonSendFailed + " | handoff"
	if !strings.Contains(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	if strings.Contains(got, "msg:") {
		t.Errorf("nothing reached the customer, so nothing should be claimed sent: %q", got)
	}
	for _, turn := range h.Get(1, 7) {
		if turn.Role == "assistant" {
			t.Errorf("history must not hold a reply the customer never got: %+v", turn)
		}
	}
}

func TestSendRetryDependsOnTheError(t *testing.T) {
	cases := map[string]struct {
		err       error
		wantTries int
	}{
		"server error is retried":    {&chatwoot.APIError{Status: 503}, 2},
		"rate limit is retried":      {&chatwoot.APIError{Status: 429}, 2},
		"network error is retried":   {errors.New("connection reset"), 2},
		"client error goes to staff": {&chatwoot.APIError{Status: 422}, 1},
		"not found goes to staff":    {&chatwoot.APIError{Status: 404}, 1},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange: every send fails with this error.
			cw := &flakyChatwoot{fails: 5, err: c.err}
			b, _ := newBot(&cw.fakeChatwoot, &fakeLLM{reply: "เปิด 9 โมงครับ"})
			b.opts.Chatwoot, b.sendRetryWait = cw, time.Millisecond

			// Act
			b.Process(context.Background(), customerMsg("เปิดกี่โมง"))

			// Assert
			got := cw.joined()
			if n := strings.Count(got, "send-error"); n != c.wantTries {
				t.Errorf("send attempts = %d, want %d: %q", n, c.wantTries, got)
			}
			if !strings.Contains(got, ReasonSendFailed+" | handoff") {
				t.Errorf("a reply that never went out must reach staff: %q", got)
			}
		})
	}
}
