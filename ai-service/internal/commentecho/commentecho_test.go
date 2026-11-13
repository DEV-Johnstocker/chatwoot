package commentecho

import (
	"strconv"
	"testing"
	"time"
)

func TestEchoes_TakeOnce(t *testing.T) {
	// Arrange
	e := &Echoes{}
	e.Put("m_1", Lead{CommentID: "c1", FromName: "Nok"})

	// Act
	first, ok1 := e.Take("m_1")
	_, ok2 := e.Take("m_1")

	// Assert
	if !ok1 || first.CommentID != "c1" || ok2 {
		t.Fatalf("first %+v %v, second %v", first, ok1, ok2)
	}
}

func TestEchoes_UnknownEmptyAndExpired(t *testing.T) {
	// Arrange
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	e := &Echoes{TTL: time.Minute, Now: func() time.Time { return now }}
	e.Put("", Lead{CommentID: "ignored"})
	e.Put("m_old", Lead{CommentID: "c"})
	now = now.Add(2 * time.Minute)

	// Act
	_, unknown := e.Take("m_x")
	_, empty := e.Take("")
	_, expired := e.Take("m_old")

	// Assert
	if unknown || empty || expired || e.Len() != 0 {
		t.Fatalf("unknown %v empty %v expired %v len %d", unknown, empty, expired, e.Len())
	}
}

func TestEchoes_CapDropsOldest(t *testing.T) {
	// Arrange
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	e := &Echoes{Max: 3, Now: func() time.Time { now = now.Add(time.Second); return now }}

	// Act
	for i := range 5 {
		e.Put("m_"+strconv.Itoa(i), Lead{})
	}

	// Assert
	if e.Len() != 3 {
		t.Fatalf("len %d, want 3", e.Len())
	}
	if _, ok := e.Take("m_0"); ok {
		t.Error("the oldest entry must be dropped first")
	}
	if _, ok := e.Take("m_4"); !ok {
		t.Error("the newest entry must stay")
	}
}
