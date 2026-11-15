package bot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/alerts"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/handoff"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/webhook"
)

type fakeAlerts struct {
	mu     sync.Mutex
	events []string
	turns  []bool
}

func (f *fakeAlerts) Record(k alerts.Kind, acct int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, string(k))
}

func (f *fakeAlerts) Turn(handedOff bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.turns = append(f.turns, handedOff)
}

// tallyLLM counts model calls.
type tallyLLM struct {
	fakeLLM
	calls int
}

func (c *tallyLLM) Complete(ctx context.Context, m []llm.Message) (llm.Result, error) {
	c.calls++
	return c.fakeLLM.Complete(ctx, m)
}

func limitedBot(limit int, now *time.Time) (*Bot, *fakeChatwoot, *tallyLLM, *fakeAlerts) {
	cw, l, a := &fakeChatwoot{}, &tallyLLM{fakeLLM: fakeLLM{reply: "ok"}}, &fakeAlerts{}
	b := New(Options{
		Chatwoot: cw, LLM: l, History: memory.NewHistory(20, time.Hour, nil),
		Handoff:        handoff.New([]string{"human"}),
		HandoffMessage: "connecting you", FailureMessage: "sorry",
		Logger:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now:        func() time.Time { return *now },
		ReplyLimit: limit, ReplyLimitWindow: time.Hour, Alerts: a,
	}, 1, 4)
	return b, cw, l, a
}

func fromContact(text string, conv, contact int64) Job {
	j := customerMsg(text)
	j.Event.Conversation.ID = conv
	j.Event.Sender = webhook.Sender{ID: contact, Type: "contact"}
	return j
}

func TestReplyLimit_HandsOffQuietlyOnceWhenReached(t *testing.T) {
	// Arrange
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	b, cw, l, a := limitedBot(3, &now)
	for i := 0; i < 3; i++ {
		b.Process(context.Background(), fromContact("hi", 7, 55))
		now = now.Add(time.Minute)
	}
	cw.calls = nil

	// Act: the fourth message within the hour
	b.Process(context.Background(), fromContact("hi again", 7, 55))

	// Assert
	if l.calls != 3 {
		t.Errorf("model calls = %d, want 3 (none for the message over the limit)", l.calls)
	}
	got := cw.joined()
	if strings.Contains(got, "msg:") {
		t.Errorf("customer got a message at the limit: %s", got)
	}
	if !strings.Contains(got, "note:") || !strings.Contains(got, ReasonReplyLimit) || !strings.HasSuffix(got, "handoff") {
		t.Errorf("calls = %s, want a private note with the reason, then the handoff", got)
	}
	if len(a.events) != 1 || a.events[0] != string(alerts.ReplyLimit) {
		t.Errorf("alert events = %v", a.events)
	}
	if len(a.turns) != 3 {
		t.Errorf("turns counted for the spike = %v; the limit hand-off is not a spike", a.turns)
	}
}

func TestReplyLimit_LaterMessagesAreNotReadOrNotedAgain(t *testing.T) {
	// Arrange
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	b, cw, l, a := limitedBot(1, &now)
	b.Process(context.Background(), fromContact("one", 7, 55))
	b.Process(context.Background(), fromContact("two", 7, 55)) // limit: note + handoff
	cw.calls = nil

	// Act: the chat is somehow still waiting for the bot (reopened)
	b.Process(context.Background(), fromContact("three", 7, 55))

	// Assert
	if got := cw.joined(); got != "handoff" {
		t.Errorf("calls = %s, want only the hand-off (no note, no reply)", got)
	}
	if l.calls != 1 || len(a.events) != 1 {
		t.Errorf("model calls %d, alerts %v", l.calls, a.events)
	}
}

func TestReplyLimit_CountsTheContactAcrossChats(t *testing.T) {
	// Arrange
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	b, cw, l, _ := limitedBot(2, &now)
	b.Process(context.Background(), fromContact("a", 7, 55))
	b.Process(context.Background(), fromContact("b", 8, 55))
	cw.calls = nil

	// Act: a third chat from the same contact
	b.Process(context.Background(), fromContact("c", 9, 55))

	// Assert
	if l.calls != 2 || !strings.Contains(cw.joined(), ReasonReplyLimit) {
		t.Errorf("model calls %d, calls %s", l.calls, cw.joined())
	}
}

func TestReplyLimit_OtherCustomersAndLaterHoursAreAnswered(t *testing.T) {
	// Arrange
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	b, cw, _, _ := limitedBot(1, &now)
	b.Process(context.Background(), fromContact("one", 7, 55))
	b.Process(context.Background(), fromContact("two", 7, 55)) // over the limit

	// Act
	cw.calls = nil
	b.Process(context.Background(), fromContact("hello", 8, 66))
	other := cw.joined()
	now = now.Add(time.Hour + time.Minute)
	cw.calls = nil
	b.Process(context.Background(), fromContact("back", 7, 55))
	later := cw.joined()

	// Assert
	if !strings.Contains(other, "msg:ok") {
		t.Errorf("another customer was not answered: %s", other)
	}
	if !strings.Contains(later, "msg:ok") {
		t.Errorf("the customer was not answered after the window: %s", later)
	}
}

func TestReplyLimit_ZeroIsOff(t *testing.T) {
	// Arrange
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	b, cw, l, _ := limitedBot(0, &now)

	// Act
	for i := 0; i < 50; i++ {
		b.Process(context.Background(), fromContact("hi", 7, 55))
	}

	// Assert
	if l.calls != 50 || strings.Contains(cw.joined(), ReasonReplyLimit) {
		t.Errorf("model calls %d with the limit off", l.calls)
	}
}

func TestReplyLimit_NoContactCountsTheChat(t *testing.T) {
	// Arrange
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	b, _, l, _ := limitedBot(1, &now)

	// Act
	b.Process(context.Background(), fromContact("a", 7, 0))
	b.Process(context.Background(), fromContact("b", 8, 0))
	b.Process(context.Background(), fromContact("c", 7, 0))

	// Assert
	if l.calls != 2 {
		t.Errorf("model calls = %d, want 2 (chat 7 limited, chat 8 its own count)", l.calls)
	}
}

func TestAlerts_FailuresAreRecorded(t *testing.T) {
	// Arrange
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	b, _, l, a := limitedBot(0, &now)
	l.err = errors.New("provider down")

	// Act
	b.Process(context.Background(), fromContact("price?", 7, 55))

	// Assert
	if len(a.events) != 1 || a.events[0] != string(alerts.LLMError) || len(a.turns) != 1 || !a.turns[0] {
		t.Errorf("events %v, turns %v", a.events, a.turns)
	}
}

func TestAlerts_SendFailureIsRecorded(t *testing.T) {
	// Arrange
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	a := &fakeAlerts{}
	b := New(Options{
		Chatwoot: &failingSend{}, LLM: &fakeLLM{reply: "ok"}, History: memory.NewHistory(20, time.Hour, nil),
		Handoff: handoff.New(nil), Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now: func() time.Time { return now }, Alerts: a,
	}, 1, 4)
	b.sendRetryWait = 0

	// Act
	b.Process(context.Background(), fromContact("hi", 7, 55))

	// Assert
	if len(a.events) != 1 || a.events[0] != string(alerts.SendFailed) {
		t.Errorf("events %v", a.events)
	}
}

func TestAlerts_QueueFullIsRecorded(t *testing.T) {
	// Arrange: one worker, queue of one, never started
	a := &fakeAlerts{}
	b := New(Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Alerts: a}, 1, 1)
	b.enqueueWait = time.Millisecond
	_ = b.Enqueue(customerMsg("first"))

	// Act
	err := b.Enqueue(customerMsg("second"))

	// Assert
	if !errors.Is(err, ErrQueueFull) || len(a.events) != 1 || a.events[0] != string(alerts.QueueFull) {
		t.Errorf("err %v, events %v", err, a.events)
	}
}

// failingSend refuses every public message with a network error.
type failingSend struct{ fakeChatwoot }

func (f *failingSend) SendMessage(context.Context, int64, int64, string) error {
	return errors.New("connection refused")
}
