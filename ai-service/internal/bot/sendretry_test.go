package bot

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// flakyChatwoot fails the first `fails` SendMessage calls.
type flakyChatwoot struct {
	fakeChatwoot
	mu    sync.Mutex
	fails int
}

func (c *flakyChatwoot) SendMessage(ctx context.Context, a, conv int64, s string) error {
	c.mu.Lock()
	if c.fails > 0 {
		c.fails--
		c.mu.Unlock()
		_ = c.record("send-failed")
		return errors.New("chatwoot 502")
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
	if got := cw.joined(); got != "typing:on | send-failed | msg:เปิด 9 โมงครับ | typing:off" {
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
	want := "send-failed | send-failed | note:🤖 **บอทส่งต่อ:** " + reasonLabels[ReasonSendFailed] + "\n" + ReasonSendFailed + " | handoff"
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
