package autoresolve

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

const after = 22 * time.Hour

func idle(id int64, hours float64) Conversation {
	return Conversation{
		ID: id, Status: "pending",
		LastActivity: now.Add(-time.Duration(hours * float64(time.Hour))),
		LastMessage:  &Message{Outgoing: true},
	}
}

func TestFinished_BotSpokeLastAndIdleLongEnough(t *testing.T) {
	// Arrange
	c := idle(1, 23)

	// Act
	ok := Finished(c, now, after)

	// Assert
	if !ok {
		t.Fatal("an idle chat the bot answered last should be finished")
	}
}

func TestFinished_KeepsChatsThatAreNotDone(t *testing.T) {
	cases := map[string]func(*Conversation){
		"too recent":          func(c *Conversation) { c.LastActivity = now.Add(-21 * time.Hour) },
		"staff has it":        func(c *Conversation) { c.Status = "open" },
		"already resolved":    func(c *Conversation) { c.Status = "resolved" },
		"customer waiting":    func(c *Conversation) { c.WaitingSince = now.Add(-30 * time.Hour) },
		"customer spoke last": func(c *Conversation) { c.LastMessage = &Message{Outgoing: false} },
		"last is a note":      func(c *Conversation) { c.LastMessage = &Message{Outgoing: true, Private: true} },
		"no messages":         func(c *Conversation) { c.LastMessage = nil },
		"muted":               func(c *Conversation) { c.Muted = true },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			c := idle(1, 30)
			change(&c)

			// Act
			ok := Finished(c, now, after)

			// Assert
			if ok {
				t.Errorf("%s: should not be resolved", name)
			}
		})
	}
}

type fakeChatwoot struct {
	accounts []int64
	convs    map[int64][]Conversation
	status   map[int64]string // live status per conversation, read just before resolving
	resolved []int64
	listErr  error
	resErr   error
}

func (f *fakeChatwoot) Accounts(context.Context) ([]int64, error) { return f.accounts, nil }

func (f *fakeChatwoot) IdlePending(_ context.Context, acct int64, before time.Time) ([]Conversation, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []Conversation
	for _, c := range f.convs[acct] {
		if !c.LastActivity.After(before) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeChatwoot) ConversationStatus(_ context.Context, _, conv int64) (string, error) {
	if s, ok := f.status[conv]; ok {
		return s, nil
	}
	return "pending", nil
}

func (f *fakeChatwoot) Resolve(_ context.Context, _, conv int64) error {
	if f.resErr != nil {
		return f.resErr
	}
	f.resolved = append(f.resolved, conv)
	return nil
}

func newSweeper(f *fakeChatwoot) *Sweeper {
	return &Sweeper{
		List: f, Chat: f, After: after,
		Now:    func() time.Time { return now },
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestSweep_ResolvesOnlyFinishedChats(t *testing.T) {
	// Arrange
	waiting := idle(3, 30)
	waiting.WaitingSince = now.Add(-30 * time.Hour)
	f := &fakeChatwoot{
		accounts: []int64{1, 3},
		convs: map[int64][]Conversation{
			1: {idle(10, 23), idle(11, 5), waiting},
			3: {idle(30, 48)},
		},
	}

	// Act
	n := newSweeper(f).Sweep(context.Background())

	// Assert
	if n != 2 || len(f.resolved) != 2 || f.resolved[0] != 10 || f.resolved[1] != 30 {
		t.Fatalf("resolved %v (n=%d), want [10 30]", f.resolved, n)
	}
}

func TestSweep_SkipsAChatStaffTookMeanwhile(t *testing.T) {
	// Arrange: listed as pending, but staff opened it before the resolve.
	f := &fakeChatwoot{
		accounts: []int64{1},
		convs:    map[int64][]Conversation{1: {idle(10, 23)}},
		status:   map[int64]string{10: "open"},
	}

	// Act
	n := newSweeper(f).Sweep(context.Background())

	// Assert
	if n != 0 || len(f.resolved) != 0 {
		t.Fatalf("resolved %v, want none", f.resolved)
	}
}

func TestSweep_ErrorsChangeNothingAndDoNotStop(t *testing.T) {
	// Arrange
	f := &fakeChatwoot{
		accounts: []int64{1},
		convs:    map[int64][]Conversation{1: {idle(10, 23), idle(11, 23)}},
		resErr:   errors.New("chatwoot down"),
	}

	// Act
	n := newSweeper(f).Sweep(context.Background())

	// Assert
	if n != 0 {
		t.Fatalf("n=%d, want 0 when every resolve fails", n)
	}
}

func TestSweep_OffWhenAfterIsZero(t *testing.T) {
	// Arrange
	f := &fakeChatwoot{accounts: []int64{1}, convs: map[int64][]Conversation{1: {idle(10, 100)}}}
	s := newSweeper(f)
	s.After = 0

	// Act
	n := s.Sweep(context.Background())

	// Assert
	if n != 0 || len(f.resolved) != 0 {
		t.Fatalf("resolved %v with the feature off", f.resolved)
	}
}
