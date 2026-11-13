package bot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/schedule"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/webhook"
)

type fakeHours struct {
	s   schedule.Schedule
	err error
}

func (f fakeHours) Get(context.Context, int64) (schedule.Schedule, error) { return f.s, f.err }

func botWithHours(h Hours, now time.Time) (*Bot, *fakeChatwoot, *fakeLLM) {
	cw, l := &fakeChatwoot{}, &fakeLLM{reply: "We open at 9."}
	b, _ := newBot(cw, l)
	b.opts.Hours = h
	b.opts.Now = func() time.Time { return now }
	return b, cw, l
}

// monday10 is Monday 2026-11-09 10:00 in Bangkok.
var monday10 = time.Date(2026, 11, 9, 3, 0, 0, 0, time.UTC)

func TestProcess_BotOffHandsToStaffWithoutReplying(t *testing.T) {
	// Arrange
	off := schedule.Default(1)
	off.Mode = schedule.ModeOff
	b, cw, l := botWithHours(fakeHours{s: off}, monday10)

	// Act
	b.Process(context.Background(), customerMsg("price?"))

	// Assert
	want := "note:🤖 " + reasonLabels[ReasonBotOff] + "\n" + ReasonBotOff + " | handoff"
	if got := cw.joined(); got != want {
		t.Errorf("calls = %q, want %q", got, want)
	}
	if l.got != nil {
		t.Error("the LLM must not be called while the bot is off")
	}
}

func TestProcess_OutsideHoursHandsToStaff(t *testing.T) {
	// Arrange: the bot works only 18:00–09:00; 10:00 is staff time.
	s := schedule.Default(1)
	s.Mode = schedule.ModeScheduled
	for i := range s.Days {
		s.Days[i] = schedule.Day{On: true, From: 18 * 60, To: 9 * 60}
	}
	b, cw, l := botWithHours(fakeHours{s: s}, monday10)

	// Act
	b.Process(context.Background(), customerMsg("price?"))

	// Assert
	if got := cw.joined(); got != "note:🤖 "+reasonLabels[ReasonBotOff]+"\n"+ReasonBotOff+" | handoff" {
		t.Errorf("calls = %q", got)
	}
	if l.got != nil {
		t.Error("the LLM must not be called outside the bot's hours")
	}
}

func TestProcess_InsideHoursReplies(t *testing.T) {
	// Arrange
	s := schedule.Default(1) // scheduled 09:00–18:00 every day
	s.Mode = schedule.ModeScheduled
	b, cw, _ := botWithHours(fakeHours{s: s}, monday10)

	// Act
	b.Process(context.Background(), customerMsg("when do you open?"))

	// Assert
	if got := cw.joined(); got != "typing:on | msg:We open at 9. | typing:off" {
		t.Errorf("calls = %q", got)
	}
}

func TestProcess_UnreadableHoursKeepTheBotOn(t *testing.T) {
	// Arrange
	b, cw, _ := botWithHours(fakeHours{err: errors.New("db down")}, monday10)

	// Act
	b.Process(context.Background(), customerMsg("when do you open?"))

	// Assert
	if got := cw.joined(); got != "typing:on | msg:We open at 9. | typing:off" {
		t.Errorf("calls = %q", got)
	}
}

func TestProcess_BotOffStillRecordsStaffMessages(t *testing.T) {
	// Arrange: the switch only stops replies; other events work as before.
	off := schedule.Default(1)
	off.Mode = schedule.ModeOff
	b, cw, _ := botWithHours(fakeHours{s: off}, monday10)
	j := customerMsg("hi")
	j.Decision = webhook.Forget

	// Act
	b.Process(context.Background(), j)

	// Assert
	if got := cw.joined(); got != "" {
		t.Errorf("calls = %q, want none", got)
	}
}
