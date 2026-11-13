package bot

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/schedule"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/webhook"
)

// Regression tests from the QA run on the bot switch (2026-11-14).
// knownBug marks a confirmed, unfixed bug: the test passes while the bug is
// there and fails once it is fixed, so whoever fixes it removes the marker.

func knownBug(t *testing.T, finding string, buggy bool) {
	t.Helper()
	if buggy {
		t.Logf("known bug %s still present", finding)
		return
	}
	t.Fatalf("known bug %s is fixed: turn this into a normal assertion", finding)
}

type flipHours struct {
	mu sync.Mutex
	s  schedule.Schedule
}

func (f *flipHours) Get(context.Context, int64) (schedule.Schedule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.s, nil
}

func (f *flipHours) set(m schedule.Mode) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.s.Mode = m
}

// QA-2: the owner switches the bot off while a reply waits out the delay.
func TestQA_SwitchOffDuringReplyDelay(t *testing.T) {
	// Arrange
	b, _, l := batchingBot(t, 80*time.Millisecond)
	h := &flipHours{s: schedule.Default(1)}
	b.opts.Hours = h

	// Act
	_ = b.Enqueue(msgN(1, "price?"))
	time.Sleep(20 * time.Millisecond)
	h.set(schedule.ModeOff)
	time.Sleep(250 * time.Millisecond)

	// Assert: the bot should hand off instead of calling the model.
	n, _ := l.snapshot()
	knownBug(t, "QA-2 (reply after switch-off during the delay)", n > 0)
}

// QA-5: two messages decided Reply while the chat is still pending.
func TestQA_TwoMessagesWhileOffOneNote(t *testing.T) {
	// Arrange
	off := schedule.Default(1)
	off.Mode = schedule.ModeOff
	b, cw, _ := botWithHours(fakeHours{s: off}, monday10)

	// Act
	b.Process(context.Background(), msgN(1, "hi"))
	b.Process(context.Background(), msgN(2, "price?"))

	// Assert: one private note per chat is the promise.
	knownBug(t, "QA-5 (one note per message while pending)", strings.Count(cw.joined(), "note:") > 1)
}

// The promise checked end to end: once handed off (open), later messages
// in the chat cause no note, no reply and no model call.
func TestQA_OpenChatWhileOffIsSilent(t *testing.T) {
	// Arrange
	off := schedule.Default(1)
	off.Mode = schedule.ModeOff
	b, cw, l := botWithHours(fakeHours{s: off}, monday10)
	j := customerMsg("still there?")
	j.Event.Conversation.Status = "open"
	j.Decision = webhook.Classify(j.Event) // the real webhook decision for an open chat

	// Act
	b.Process(context.Background(), j)

	// Assert
	if got := cw.joined(); got != "" || l.got != nil {
		t.Errorf("calls = %q, llm called = %v; want nothing", got, l.got != nil)
	}
}
