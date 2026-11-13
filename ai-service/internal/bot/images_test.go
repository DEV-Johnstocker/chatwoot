package bot

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/images"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
)

// imageChatwoot also records attachments.
type imageChatwoot struct {
	fakeChatwoot
	fail bool
}

func (c *imageChatwoot) SendAttachment(_ context.Context, _, _ int64, content, filename, contentType string, _ []byte) error {
	if c.fail {
		return errors.New("chatwoot down")
	}
	return c.record("image:" + content + "|" + filename + "|" + contentType)
}

func smallPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// imageBot is a bot with a business profile and, when withImages, two pictures.
func imageBot(t *testing.T, reply string, withImages bool) (*Bot, *imageChatwoot, *fakeLLM, *memory.History) {
	t.Helper()
	cw, l := &imageChatwoot{}, &fakeLLM{reply: reply}
	b, h := newBot(&cw.fakeChatwoot, l)
	b.opts.Chatwoot = cw
	b.opts.Profiles = &fakeProfiles{byAccount: map[int64]profile.Profile{1: {AccountID: 1, BusinessName: "AutoCut"}}}
	store := &images.Memory{}
	if withImages {
		for _, m := range []images.Meta{
			{AccountID: 1, Name: "รีวิว AutoCut", When: "ขอดูรีวิวคอร์ส AutoCut", Caption: "รีวิวจากผู้เรียนจริงครับ"},
			{AccountID: 1, Name: "รีวิว Workshop", When: "ถามรีวิว Workshop", Caption: "บรรยากาศ Workshop ครับ"},
		} {
			if _, err := store.Add(context.Background(), images.Image{Meta: m, Data: smallPNG(t)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	b.opts.Images = store
	return b, cw, l, h
}

func TestImages_MarkerSendsTheNamedPictureOnce(t *testing.T) {
	// Arrange
	b, cw, l, h := imageBot(t, "มีผู้ใช้จริงหลายท่านครับ\n[[IMAGE: รีวิว autocut]]\nสนใจทำคลิปแนวไหนครับ", true)

	// Act
	b.Process(context.Background(), customerMsg("มีรีวิวไหม"))
	b.Process(context.Background(), customerMsg("ขอดูอีกได้ไหม"))

	// Assert
	got := cw.joined()
	if strings.Count(got, "image:รีวิวจากผู้เรียนจริงครับ|image.png|image/png") != 1 || strings.Contains(got, "Workshop") {
		t.Fatalf("want the AutoCut picture exactly once: %s", got)
	}
	if strings.Contains(got, "[[") || !strings.Contains(got, "msg:มีผู้ใช้จริงหลายท่านครับ\nสนใจทำคลิปแนวไหนครับ | image:") {
		t.Fatalf("text must go first, without the marker: %s", got)
	}
	if !strings.Contains(l.got[0].Content, "- รีวิว Workshop: ถามรีวิว Workshop") {
		t.Error("the prompt must list the pictures with when to send them")
	}
	if turns := h.Get(1, 7); !strings.Contains(turns[1].Content, "[[IMAGE: รีวิว AutoCut]]") {
		t.Errorf("history must remember which picture went out: %+v", turns)
	}
}

func TestImages_UnknownOrMissingNeverSends(t *testing.T) {
	cases := map[string]struct {
		reply      string
		withImages bool
		wantText   string
	}{
		"unknown name":         {"ดูรีวิวได้ที่ลิงก์ครับ\n[[IMAGE: รีวิว Course X]]", true, "msg:ดูรีวิวได้ที่ลิงก์ครับ"},
		"no images at all":     {"ดูรีวิวได้ที่ลิงก์ครับ\n[[IMAGE: รีวิว AutoCut]]", false, "msg:ดูรีวิวได้ที่ลิงก์ครับ"},
		"only an unknown mark": {"[[ image : nothing ]]", true, "msg:connecting you"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			b, cw, l, h := imageBot(t, c.reply, c.withImages)

			// Act
			b.Process(context.Background(), customerMsg("มีรีวิวไหม"))

			// Assert
			got := cw.joined()
			if strings.Contains(got, "image:") || strings.Contains(got, "[[") || !strings.Contains(got, c.wantText) {
				t.Fatalf("got %s", got)
			}
			if !c.withImages && strings.Contains(l.got[0].Content, "# Images you can send") {
				t.Error("no pictures: the prompt must not offer any")
			}
			if turns := h.Get(1, 7); strings.Contains(turns[len(turns)-1].Content, "[[IMAGE") {
				t.Error("history must not claim a picture went out")
			}
		})
	}
}

func TestImages_AtMostTwoPerReply(t *testing.T) {
	// Arrange: three markers, two real pictures and a repeat.
	b, cw, _, _ := imageBot(t, "[[IMAGE: รีวิว AutoCut]]\n[[IMAGE: รีวิว Workshop]]\n[[IMAGE: รีวิว AutoCut]]", true)

	// Act
	b.Process(context.Background(), customerMsg("ขอดูรีวิวทั้งหมด"))

	// Assert
	if n := strings.Count(cw.joined(), "image:"); n != 2 {
		t.Fatalf("images %d, want 2: %s", n, cw.joined())
	}
}

func TestImages_FailedSendStillAnswers(t *testing.T) {
	// Arrange
	b, cw, _, h := imageBot(t, "[[IMAGE: รีวิว AutoCut]]", true)
	cw.fail = true

	// Act
	b.Process(context.Background(), customerMsg("มีรีวิวไหม"))

	// Assert
	if got := cw.joined(); !strings.Contains(got, "msg:connecting you") {
		t.Fatalf("the customer must get a reply: %s", got)
	}
	if turns := h.Get(1, 7); strings.Contains(turns[len(turns)-1].Content, "[[IMAGE") {
		t.Error("a failed picture must not count as sent")
	}
}

func TestImages_HandoffSendsNoPicture(t *testing.T) {
	// Arrange
	b, cw, _, _ := imageBot(t, "ขอส่งต่อให้ทีมงานครับ\n[[IMAGE: รีวิว AutoCut]]\n[[HANDOFF]]", true)

	// Act
	b.Process(context.Background(), customerMsg("ขอคุยกับคน"))

	// Assert
	if got := cw.joined(); strings.Contains(got, "image:") || strings.Contains(got, "[[") {
		t.Fatalf("got %s", got)
	}
}

func TestImages_OncePerChatSurvivesHistoryLossAndRecall(t *testing.T) {
	// Arrange
	b, cw, _, h := imageBot(t, "[[IMAGE: รีวิว AutoCut]]", true)
	b.Process(context.Background(), customerMsg("มีรีวิวไหม"))
	h.Forget(1, 7) // the turn limit or the 24 h expiry dropped the chat

	// Act
	b.Process(context.Background(), customerMsg("ขอรีวิวอีกที"))

	// Assert
	if n := strings.Count(cw.joined(), "image:"); n != 1 {
		t.Fatalf("images sent %d, want 1: %s", n, cw.joined())
	}
	recalled := []memory.Turn{{Role: "assistant", Content: "รีวิวจากผู้เรียนจริงครับ\n[[IMAGE: ?]]"}}
	auto := images.Meta{Name: "รีวิว AutoCut", Caption: "รีวิวจากผู้เรียนจริงครับ"}
	work := images.Meta{Name: "รีวิว Workshop", Caption: "บรรยากาศ Workshop ครับ"}
	if !imageSentBefore(recalled, auto) || imageSentBefore(recalled, work) {
		t.Error("a recalled picture must count by its caption, and only that one")
	}
}

func TestAnswer_ShowsWhichPictureForTheJudge(t *testing.T) {
	// Arrange
	withImg, _, _, _ := imageBot(t, "มีครับ\n[[IMAGE: รีวิว Workshop]]", true)
	without, _, _, _ := imageBot(t, "มีครับ\n[[IMAGE: รีวิว Workshop]]", false)
	ask := []memory.Turn{{Role: "user", Content: "Workshop มีรีวิวไหม"}}

	// Act
	a, _, err1 := withImg.Answer(context.Background(), 1, 0, ask)
	n, _, err2 := without.Answer(context.Background(), 1, 0, ask)

	// Assert
	if err1 != nil || err2 != nil || a.Content != "มีครับ\n"+ImageSent("รีวิว Workshop") || n.Content != "มีครับ" {
		t.Fatalf("with %q without %q errs %v %v", a.Content, n.Content, err1, err2)
	}
}

func TestAnswer_HandoffShowsNoPicture(t *testing.T) {
	// Arrange
	b, _, _, _ := imageBot(t, "ขอส่งต่อทีมงานครับ\n[[IMAGE: รีวิว Workshop]]\n[[HANDOFF]]", true)

	// Act
	a, _, err := b.Answer(context.Background(), 1, 0, []memory.Turn{{Role: "user", Content: "Workshop มีรีวิวไหม"}})

	// Assert
	if err != nil || strings.Contains(a.Content, "ส่งรูป") || !strings.Contains(a.Content, "[[HANDOFF]]") {
		t.Fatalf("got %q err %v", a.Content, err)
	}
}
