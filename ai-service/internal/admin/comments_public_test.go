package admin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/comments"
)

type recordingPublic struct {
	posted []string
	err    error
}

func (p *recordingPublic) PostPublicReply(_ context.Context, commentID, text string) error {
	p.posted = append(p.posted, commentID+"|"+text)
	return p.err
}

const publicLine = "สวัสดีครับ ส่งรายละเอียดทางข้อความแล้วนะครับ 🙏"

// newPublicEnv is newCommentsEnv with public replies on.
func newPublicEnv(t *testing.T, failPublic bool) (*commentsEnv, *recordingPublic) {
	t.Helper()
	e := newCommentsEnv(t, true, true)
	pub := &recordingPublic{}
	if failPublic {
		pub.err = &comments.SendError{Code: 200, Message: "(#200) Requires pages_manage_engagement permission"}
	}
	e.queue.Public, e.queue.PublicText = pub, publicLine
	return e, pub
}

func TestComments_PublicReplyCheckboxOnlyWhenOn(t *testing.T) {
	// Arrange
	off := newCommentsEnv(t, true, true)
	off.waiting(t, 2, "c1", "ราคา", time.Now())
	on, _ := newPublicEnv(t, false)
	on.waiting(t, 2, "c1", "ราคา", time.Now())

	// Act
	offBody := serve(off.mux, commentsGet("account=2")).Body.String()
	onBody := serve(on.mux, commentsGet("account=2")).Body.String()

	// Assert
	if strings.Contains(offBody, `name="public"`) {
		t.Fatal("checkbox shown while public replies are off")
	}
	if !strings.Contains(onBody, `name="public" value="on" checked`) || !strings.Contains(onBody, publicLine) {
		t.Fatal("checkbox or its line missing while on")
	}
}

func TestComments_SendWithPublicReply(t *testing.T) {
	cases := map[string]struct {
		tick       bool
		failPublic bool
		wantDone   string
		wantPosts  int
	}{
		"ticked":         {true, false, "sent-public", 1},
		"not ticked":     {false, false, "sent", 0},
		"public refused": {true, true, "sent-public-failed", 1},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			e, pub := newPublicEnv(t, c.failPublic)
			cm := e.waiting(t, 2, "c1", "ราคา", time.Now())
			form := decide(cm, 2, "send", "สวัสดีครับ")
			if c.tick {
				form.Set("public", "on")
			}

			// Act
			res := serve(e.mux, commentsPost(form))
			page := serve(e.mux, commentsGet("account=2&done="+c.wantDone)).Body.String()

			// Assert
			got, _ := e.store.Get(context.Background(), 2, cm.ID)
			if loc := res.Header().Get("Location"); loc != "/admin/comments?account=2&done="+c.wantDone {
				t.Fatalf("location %q", loc)
			}
			if got.Status != comments.StatusSent || len(e.sender.sent) != 1 || len(pub.posted) != c.wantPosts {
				t.Fatalf("status %s private %v public %v", got.Status, e.sender.sent, pub.posted)
			}
			if !strings.Contains(page, `role="status">Sent`) {
				t.Fatalf("no sent notice for %s", c.wantDone)
			}
		})
	}
}

func TestComments_DismissNeverRepliesPublicly(t *testing.T) {
	// Arrange
	e, pub := newPublicEnv(t, false)
	cm := e.waiting(t, 2, "c1", "ราคา", time.Now())
	form := decide(cm, 2, "dismiss", "")
	form.Set("public", "on")

	// Act
	_ = serve(e.mux, commentsPost(form))

	// Assert
	if len(pub.posted) != 0 || len(e.sender.sent) != 0 {
		t.Fatalf("public %v private %v", pub.posted, e.sender.sent)
	}
}
