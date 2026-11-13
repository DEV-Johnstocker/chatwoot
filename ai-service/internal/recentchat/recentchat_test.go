package recentchat

import (
	"strconv"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func TestMaybeChatted_BlindForOneWindowAfterStart(t *testing.T) {
	// Arrange: other customers keep chatting, so the record is live.
	c := New(t0)
	c.Saw(1, "someone-else", t0.Add(24*time.Hour+30*time.Minute))

	// Act
	early := c.MaybeChatted(1, "psid", t0.Add(23*time.Hour))
	later := c.MaybeChatted(1, "psid", t0.Add(25*time.Hour))

	// Assert
	if !early || later {
		t.Fatalf("early %v (want true: cannot know yet), later %v (want false)", early, later)
	}
}

func TestMaybeChatted_SeenWithinWindowOnly(t *testing.T) {
	// Arrange: past the blind window; both accounts keep hearing customers.
	c := New(t0.Add(-48 * time.Hour))
	c.Saw(1, "psid", t0)
	c.Saw(1, "busy", t0.Add(24*time.Hour+30*time.Minute))
	c.Saw(2, "busy", t0.Add(30*time.Minute))

	// Act
	within := c.MaybeChatted(1, "psid", t0.Add(23*time.Hour))
	after := c.MaybeChatted(1, "psid", t0.Add(25*time.Hour))
	otherAccount := c.MaybeChatted(2, "psid", t0.Add(time.Hour))
	otherPerson := c.MaybeChatted(1, "someone", t0.Add(time.Hour))

	// Assert
	if !within || after || otherAccount || otherPerson {
		t.Fatalf("within %v after %v otherAccount %v otherPerson %v", within, after, otherAccount, otherPerson)
	}
}

func TestMaybeChatted_EmptyIDIsUnknown(t *testing.T) {
	// Arrange
	c := New(t0.Add(-48 * time.Hour))
	c.Saw(1, "", t0)

	// Act
	got := c.MaybeChatted(1, "", t0)

	// Assert
	if !got || len(c.last) != 0 {
		t.Fatalf("got %v, stored %d", got, len(c.last))
	}
}

func TestSaw_OlderMessageDoesNotRewindAndCapHolds(t *testing.T) {
	// Arrange
	c := New(t0.Add(-48 * time.Hour))
	c.max = 3

	// Act
	c.Saw(1, "a", t0)
	c.Saw(1, "a", t0.Add(-30*time.Hour)) // a late, older delivery
	for i := range 5 {
		c.Saw(1, "p"+strconv.Itoa(i), t0.Add(time.Duration(i)*time.Minute))
	}

	// Assert
	if len(c.last) > 3 {
		t.Fatalf("len %d, want at most 3", len(c.last))
	}
	if !c.MaybeChatted(1, "p4", t0.Add(time.Hour)) {
		t.Error("the newest person must stay")
	}
}

func TestSaw_EvictingARecentChatterFailsClosed(t *testing.T) {
	// Arrange: past the blind window, cap of 2, three people chat now.
	c := New(t0.Add(-48 * time.Hour))
	c.max = 2

	// Act
	for _, id := range []string{"a", "b", "c"} {
		c.Saw(1, id, t0)
	}

	// Assert: whoever was dropped must still count as maybe chatting.
	for _, id := range []string{"a", "b", "c"} {
		if !c.MaybeChatted(1, id, t0.Add(time.Hour)) {
			t.Errorf("%s: forgotten chatter treated as not chatting", id)
		}
	}
}

func TestMaybeChatted_QuietRecordFailsClosed(t *testing.T) {
	// Arrange: started long ago, last customer message 7 hours ago.
	c := New(t0.Add(-72 * time.Hour))
	c.Saw(1, "someone", t0.Add(-30*time.Hour))
	c.Saw(1, "", t0.Add(-7*time.Hour)) // an incoming message without an id still counts as heard
	never := New(t0.Add(-72 * time.Hour))

	// Act
	quiet := c.MaybeChatted(1, "stranger", t0)
	noEvents := never.MaybeChatted(1, "stranger", t0)
	c.Saw(1, "other", t0.Add(-time.Hour))
	live := c.MaybeChatted(1, "stranger", t0)

	// Assert
	if !quiet || !noEvents || live {
		t.Fatalf("quiet %v noEvents %v (want both true), live %v (want false)", quiet, noEvents, live)
	}
}
