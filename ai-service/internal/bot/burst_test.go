package bot

import (
	"testing"
	"time"
)

func TestEndlessTypingStillGetsAnAnswer(t *testing.T) {
	// Arrange: a message every 50 ms with an 80 ms reply delay never leaves
	// a quiet gap; the wait is capped at 3x the delay from the first message.
	b, _, l := batchingBot(t, 80*time.Millisecond)

	// Act
	for i := range 12 {
		_ = b.Enqueue(msgN(int64(i+1), "พิมพ์ต่อ"))
		time.Sleep(50 * time.Millisecond)
	}

	// Assert: answered while the customer was still typing.
	if calls, _ := l.snapshot(); calls == 0 {
		t.Fatal("a customer who keeps typing must still be answered")
	}
}
