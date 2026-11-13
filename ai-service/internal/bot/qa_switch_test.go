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
	b, cw, l := batchingBot(t, 80*time.Millisecond)
	h := &flipHours{s: schedule.Default(1)}
	b.opts.Hours = h

	// Act
	_ = b.Enqueue(msgN(1, "price?"))
	time.Sleep(20 * time.Millisecond)
	h.set(schedule.ModeOff)
	time.Sleep(250 * time.Millisecond)

	// Assert: the waiting reply is handed to staff, the model never called.
	n, _ := l.snapshot()
	if n != 0 {
		t.Errorf("model called %d times after the switch went off", n)
	}
	if got := cw.joined(); !strings.Contains(got, ReasonBotOff) || strings.Contains(got, "msg:") {
		t.Errorf("calls = %q, want a bot-off handoff and no customer message", got)
	}
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

	// Assert: one private note and one handoff per chat.
	got := cw.joined()
	if strings.Count(got, "note:") != 1 || strings.Count(got, "| handoff") != 1 {
		t.Errorf("calls = %q, want one note and one handoff", got)
	}
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

// QA-5: a different chat while off still gets its own note.
func TestQA_EachChatWhileOffGetsOneNote(t *testing.T) {
	// Arrange
	off := schedule.Default(1)
	off.Mode = schedule.ModeOff
	b, cw, _ := botWithHours(fakeHours{s: off}, monday10)
	other := msgN(2, "hello")
	other.Event.Conversation.ID = 99

	// Act
	b.Process(context.Background(), msgN(1, "hi"))
	b.Process(context.Background(), other)

	// Assert
	if n := strings.Count(cw.joined(), "note:"); n != 2 {
		t.Errorf("notes = %d, want one per chat (2)", n)
	}
}

// QA-5: after botOffNoteFor the same chat may be handed off again (a chat
// reopened later is a new visit).
func TestQA_BotOffNoteExpires(t *testing.T) {
	// Arrange
	off := schedule.Default(1)
	off.Mode = schedule.ModeOff
	now := monday10
	b, cw, _ := botWithHours(fakeHours{s: off}, now)
	b.opts.Now = func() time.Time { return now }

	// Act
	b.Process(context.Background(), msgN(1, "hi"))
	now = now.Add(botOffNoteFor + time.Second)
	b.Process(context.Background(), msgN(2, "back again"))

	// Assert
	if n := strings.Count(cw.joined(), "note:"); n != 2 {
		t.Errorf("notes = %d, want 2", n)
	}
}

// QA-2: a reply waiting while the bot stays on is still sent.
func TestQA_ReplyDelayWhileOnStillReplies(t *testing.T) {
	// Arrange
	b, cw, l := batchingBot(t, 40*time.Millisecond)
	b.opts.Hours = &flipHours{s: schedule.Default(1)}

	// Act
	_ = b.Enqueue(msgN(1, "price?"))
	waitFor(t, func() bool { n, _ := l.snapshot(); return n > 0 })
	time.Sleep(50 * time.Millisecond)

	// Assert
	if !strings.Contains(cw.joined(), "msg:") {
		t.Errorf("calls = %q, want a reply", cw.joined())
	}
}
