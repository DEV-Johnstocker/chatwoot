package bot

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
)

// The bot shows "typing…" for about as long as a person would take to type
// its reply. Issue: DEV-Johnstocker/chatwoot#1

func TestTypingTime_GrowsWithLengthAndIsCapped(t *testing.T) {
	// Arrange
	short, thai, long := "ได้เลยครับ", "ราคาคอร์ส 3,900 บาท ถึงสิ้นเดือนนี้ครับ", strings.Repeat("ก", 500)

	// Act
	gotShort := typingTime(short, 10, 8*time.Second)
	gotThai := typingTime(thai, 10, 8*time.Second)
	gotLong := typingTime(long, 10, 8*time.Second)

	// Assert
	if gotShort != time.Second {
		t.Errorf("10 characters at 10/s = %v, want 1s", gotShort)
	}
	if gotThai <= gotShort || gotThai >= 8*time.Second {
		t.Errorf("a longer reply = %v, want between %v and the cap", gotThai, gotShort)
	}
	if gotLong != 8*time.Second {
		t.Errorf("a very long reply = %v, want the 8s cap", gotLong)
	}
}

func TestTypingTime_OffWhenSpeedIsZero(t *testing.T) {
	// Arrange / Act / Assert
	if got := typingTime("สวัสดีครับ", 0, 8*time.Second); got != 0 {
		t.Errorf("speed 0 = %v, want no wait", got)
	}
}

func TestTypingPause_CountsTimeSpentWritingTheAnswer(t *testing.T) {
	// Arrange
	b := &Bot{opts: Options{TypingSpeed: 10, TypingMax: 8 * time.Second}}
	var waited time.Duration
	b.wait = func(_ context.Context, d time.Duration) { waited = d }
	reply := strings.Repeat("ก", 50) // 5s of typing

	// Act
	b.typingPause(context.Background(), true, time.Now().Add(-3*time.Second), reply)

	// Assert: the model already took 3s, so about 2s are left.
	if waited < 1900*time.Millisecond || waited > 2*time.Second {
		t.Errorf("waited %v, want about 2s", waited)
	}
}

func TestTypingPause_NoWaitWhenTheAnswerTookLonger(t *testing.T) {
	// Arrange
	b := &Bot{opts: Options{TypingSpeed: 10, TypingMax: 8 * time.Second}}
	called := false
	b.wait = func(context.Context, time.Duration) { called = true }

	// Act
	b.typingPause(context.Background(), true, time.Now().Add(-10*time.Second), "ได้เลยครับ")

	// Assert
	if called {
		t.Error("waited although the answer already took longer than typing it")
	}
}

func TestTypingPause_OffForTheAccountSendsAtOnce(t *testing.T) {
	// Arrange
	b := &Bot{opts: Options{TypingSpeed: 10, TypingMax: 8 * time.Second}}
	called := false
	b.wait = func(context.Context, time.Duration) { called = true }

	// Act
	b.typingPause(context.Background(), false, time.Now(), strings.Repeat("ก", 80))

	// Assert
	if called {
		t.Error("waited although the account has typing time off")
	}
}

func TestNewGuard_CarriesTheTypingSwitch(t *testing.T) {
	// Arrange / Act
	on := newGuard(profile.Profile{Features: profile.Features{Typing: true}})
	off := newGuard(profile.Profile{})

	// Assert
	if !on.typing || off.typing {
		t.Errorf("typing on=%v off=%v, want true and false", on.typing, off.typing)
	}
}
