package memory

import (
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestHistory_TrimsToLimit(t *testing.T) {
	// Arrange
	h := NewHistory(2, time.Hour, nil)

	// Act
	h.Append(1, 7, Turn{"user", "a"})
	h.Append(1, 7, Turn{"assistant", "b"})
	h.Append(1, 7, Turn{"user", "c"})

	// Assert
	got := h.Get(1, 7)
	if len(got) != 2 || got[0].Content != "b" || got[1].Content != "c" {
		t.Errorf("got %+v", got)
	}
}

func TestHistory_SeparatesAccounts(t *testing.T) {
	// Arrange
	h := NewHistory(10, time.Hour, nil)
	h.Append(1, 7, Turn{"user", "acct1"})

	// Act
	got := h.Get(2, 7)

	// Assert
	if got != nil {
		t.Errorf("leaked across accounts: %+v", got)
	}
}

func TestHistory_GetReturnsCopy(t *testing.T) {
	// Arrange
	h := NewHistory(10, time.Hour, nil)
	h.Append(1, 7, Turn{"user", "a"})

	// Act
	got := h.Get(1, 7)
	got[0].Content = "mutated"

	// Assert
	if h.Get(1, 7)[0].Content != "a" {
		t.Error("Get exposed internal slice")
	}
}

func TestHistory_ForgetAndIdleEviction(t *testing.T) {
	// Arrange
	c := &clock{t: time.Unix(0, 0)}
	h := NewHistory(10, time.Hour, c.now)
	h.Append(1, 1, Turn{"user", "old"})
	h.Append(1, 2, Turn{"user", "resolved"})

	// Act
	h.Forget(1, 2)
	c.t = c.t.Add(2 * time.Hour)
	h.Append(1, 3, Turn{"user", "new"})

	// Assert
	if h.Len() != 1 || h.Get(1, 3) == nil {
		t.Errorf("expected only conversation 3, len=%d", h.Len())
	}
}

func TestSeen_DetectsDuplicatesUntilTTL(t *testing.T) {
	// Arrange
	c := &clock{t: time.Unix(0, 0)}
	s := NewSeen(time.Minute, c.now)

	// Act
	first := s.FirstTime("m:1")
	dup := s.FirstTime("m:1")
	c.t = c.t.Add(2 * time.Minute)
	afterTTL := s.FirstTime("m:1")

	// Assert
	if !first || dup || !afterTTL {
		t.Errorf("first=%v dup=%v afterTTL=%v", first, dup, afterTTL)
	}
}

func TestSeen_Release(t *testing.T) {
	// Arrange
	s := NewSeen(time.Hour, nil)
	s.FirstTime("m:2")

	// Act
	s.Release("m:2")

	// Assert
	if !s.FirstTime("m:2") {
		t.Error("released key should be accepted again")
	}
}

func TestHistory_EarlierChatStaysBesideTheTurns(t *testing.T) {
	// Arrange
	h := NewHistory(2, time.Hour, nil)
	when := time.Date(2026, 11, 18, 23, 10, 0, 0, time.UTC)
	h.SetEarlier(1, 7, Earlier{When: when, Turns: []Turn{{Role: "user", Content: "ใช้ Windows 11"}}})

	// Act: more turns than the limit
	for _, s := range []string{"a", "b", "c"} {
		h.Append(1, 7, Turn{Role: "user", Content: s})
	}
	got := h.Earlier(1, 7)

	// Assert
	if !got.When.Equal(when) || len(got.Turns) != 1 || got.Turns[0].Content != "ใช้ Windows 11" {
		t.Errorf("earlier chat = %+v, want it kept apart from the trimmed turns", got)
	}
	if n := len(h.Get(1, 7)); n != 2 {
		t.Errorf("turns = %d, want 2 (the earlier chat never counts against the limit)", n)
	}
	if len(h.Earlier(1, 8).Turns) != 0 {
		t.Error("another conversation must have no earlier chat")
	}
}

func TestHistory_ForgetDropsTheEarlierChat(t *testing.T) {
	// Arrange
	h := NewHistory(20, time.Hour, nil)
	h.SetEarlier(1, 7, Earlier{Turns: []Turn{{Role: "user", Content: "x"}}})

	// Act
	h.Forget(1, 7)

	// Assert
	if len(h.Earlier(1, 7).Turns) != 0 {
		t.Error("a resolved chat must forget its earlier chat too")
	}
}
