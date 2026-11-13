package admin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/comments"
)

type nobodyChatted struct{}

func (nobodyChatted) MaybeChatted(int64, string, time.Time) bool { return false }

func TestComments_AutoSendBannerAndSentRow(t *testing.T) {
	// Arrange
	ctx := context.Background()
	e := newCommentsEnv(t, true, true)
	e.queue.AutoSend, e.queue.Chats = true, nobodyChatted{}
	c := e.waiting(t, 1, "c-auto", "ราคาเท่าไหร่", time.Now())
	_ = e.store.SaveReview(ctx, c.ID, comments.Review{Pass: true})
	if sent, why, err := e.queue.TryAutoSend(ctx, 1, c.ID); !sent {
		t.Fatalf("setup: %q %v", why, err)
	}

	again := e.waiting(t, 1, "c-again", "ส่งรายละเอียดด้วยครับ", time.Now())
	_ = e.store.SaveReview(ctx, again.ID, comments.Review{Pass: true})

	// Act
	page := serve(e.mux, commentsGet("")).Body.String()

	// Assert
	if !strings.Contains(page, "passed every check, but waits for you: this person already got a private reply.") {
		t.Fatalf("hold reason not shown:\n%s", page)
	}
	if !strings.Contains(page, "Auto-send is on.") || !strings.Contains(page, "Sent automatically") ||
		!strings.Contains(page, "Sent automatically so far: 1.") {
		t.Fatalf("auto-send not shown:\n%s", page)
	}
}

func TestComments_NoAutoSendBannerWhenOff(t *testing.T) {
	// Arrange
	e := newCommentsEnv(t, true, true)

	// Act
	page := serve(e.mux, commentsGet("")).Body.String()

	// Assert
	if strings.Contains(page, "Auto-send is on.") {
		t.Fatal("banner shown while auto-send is off")
	}
}
