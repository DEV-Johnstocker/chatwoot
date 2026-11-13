package bot

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/reviews"
)

// imageChatwoot also records attachments.
type imageChatwoot struct {
	fakeChatwoot
	fail bool
}

func (c *imageChatwoot) SendAttachment(_ context.Context, _, _ int64, content, filename, contentType string, data []byte) error {
	if c.fail {
		return errors.New("chatwoot down")
	}
	return c.record("image:" + content + "|" + filename + "|" + contentType)
}

func reviewPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// reviewBot is a bot with a business profile and, when withImage, a review image.
func reviewBot(t *testing.T, reply string, withImage bool) (*Bot, *imageChatwoot, *fakeLLM, *memory.History) {
	t.Helper()
	cw, l := &imageChatwoot{}, &fakeLLM{reply: reply}
	b, h := newBot(&cw.fakeChatwoot, l)
	b.opts.Chatwoot = cw
	b.opts.Profiles = &fakeProfiles{byAccount: map[int64]profile.Profile{1: {AccountID: 1, BusinessName: "AutoCut"}}}
	store := &reviews.Memory{}
	if withImage {
		if err := store.Save(context.Background(), reviews.Image{AccountID: 1, Caption: "รีวิวจากผู้เรียนจริงครับ", Data: reviewPNG(t)}); err != nil {
			t.Fatal(err)
		}
	}
	b.opts.Reviews = store
	return b, cw, l, h
}

func TestReviews_MarkerSendsTextThenImageOnce(t *testing.T) {
	// Arrange
	b, cw, l, h := reviewBot(t, "มีผู้ใช้จริงหลายท่านครับ\n[[REVIEWS]]\nสนใจทำคลิปแนวไหนครับ", true)

	// Act
	b.Process(context.Background(), customerMsg("มีรีวิวไหม"))
	b.Process(context.Background(), customerMsg("ขอดูอีกได้ไหม"))

	// Assert
	got := cw.joined()
	if strings.Count(got, "image:รีวิวจากผู้เรียนจริงครับ|reviews.png|image/png") != 1 {
		t.Fatalf("want exactly one image: %s", got)
	}
	if strings.Contains(got, "[[") || !strings.Contains(got, "msg:มีผู้ใช้จริงหลายท่านครับ\nสนใจทำคลิปแนวไหนครับ | image:") {
		t.Fatalf("text must go first, without the marker: %s", got)
	}
	if !strings.Contains(l.got[0].Content, "# Review image") {
		t.Error("prompt must tell the model about the image")
	}
	turns := h.Get(1, 7)
	if !reviewMarker.MatchString(turns[1].Content) {
		t.Errorf("history must remember the image went out: %+v", turns)
	}
}

func TestReviews_NoImageNeverSendsOrMentions(t *testing.T) {
	cases := map[string]struct {
		reply    string
		wantText string
	}{
		"marker with text": {"รีวิวอยู่ที่ลิงก์ตัวอย่างครับ\n[[REVIEWS]]", "msg:รีวิวอยู่ที่ลิงก์ตัวอย่างครับ"},
		"only the marker":  {"[[ review ]]", "msg:connecting you"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			b, cw, l, h := reviewBot(t, c.reply, false)

			// Act
			b.Process(context.Background(), customerMsg("มีรีวิวไหม"))

			// Assert
			got := cw.joined()
			if strings.Contains(got, "image:") || strings.Contains(got, "[[") || !strings.Contains(got, c.wantText) {
				t.Fatalf("got %s", got)
			}
			if strings.Contains(l.got[0].Content, "# Review image") {
				t.Error("no image: the prompt must not offer one")
			}
			if reviewSentBefore(h.Get(1, 7)) {
				t.Error("history must not claim an image went out")
			}
		})
	}
}

func TestReviews_FailedImageStillAnswers(t *testing.T) {
	// Arrange
	b, cw, _, h := reviewBot(t, "[[REVIEWS]]", true)
	cw.fail = true

	// Act
	b.Process(context.Background(), customerMsg("มีรีวิวไหม"))

	// Assert
	if got := cw.joined(); !strings.Contains(got, "msg:connecting you") {
		t.Fatalf("the customer must get a reply: %s", got)
	}
	if reviewSentBefore(h.Get(1, 7)) {
		t.Error("a failed image must not count as sent")
	}
}

func TestReviews_HandoffSendsNoImage(t *testing.T) {
	// Arrange
	b, cw, _, h := reviewBot(t, "ขอส่งต่อให้ทีมงานครับ\n[[REVIEWS]]\n[[HANDOFF]]", true)

	// Act
	b.Process(context.Background(), customerMsg("ขอคุยกับคน"))

	// Assert
	if got := cw.joined(); strings.Contains(got, "image:") || strings.Contains(got, "[[") {
		t.Fatalf("got %s", got)
	}
	if reviewSentBefore(h.Get(1, 7)) {
		t.Error("history must not claim an image went out")
	}
}

func TestAnswer_ShowsTheImageForTheJudge(t *testing.T) {
	// Arrange
	withImg, _, _, _ := reviewBot(t, "มีครับ\n[[REVIEWS]]", true)
	without, _, _, _ := reviewBot(t, "มีครับ\n[[REVIEWS]]", false)
	ask := []memory.Turn{{Role: "user", Content: "มีรีวิวไหม"}}

	// Act
	a, _, err1 := withImg.Answer(context.Background(), 1, 0, ask)
	n, _, err2 := without.Answer(context.Background(), 1, 0, ask)

	// Assert
	if err1 != nil || err2 != nil || a.Content != "มีครับ\n"+ReviewImageSent || n.Content != "มีครับ" {
		t.Fatalf("with %q without %q errs %v %v", a.Content, n.Content, err1, err2)
	}
}
