package bot

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/images"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
)

// followUpBot is a bot with three pictures of different sizes: "รีวิว AutoCut"
// and "รีวิว Workshop" ticked for follow-ups, "รีวิว ผู้ซื้อ" not.
func followUpBot(t *testing.T) (*Bot, *imageChatwoot, *earlierReader, map[string]images.Image) {
	t.Helper()
	cw := &imageChatwoot{}
	b, _ := newBot(&cw.fakeChatwoot, &fakeLLM{reply: "ok"})
	b.opts.Chatwoot = cw
	b.opts.Now = func() time.Time { return time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC) }
	store := &images.Memory{}
	byName := map[string]images.Image{}
	for i, m := range []images.Meta{
		{AccountID: 1, Name: "รีวิว ผู้ซื้อ"},
		{AccountID: 1, Name: "รีวิว AutoCut", Caption: "รีวิวจริงครับ", FollowUp: true},
		{AccountID: 1, Name: "รีวิว Workshop", FollowUp: true},
	} {
		if _, err := store.Add(context.Background(), images.Image{Meta: m, Data: pngOf(t, 2+i*10)}); err != nil {
			t.Fatal(err)
		}
		img, _, _ := store.Find(context.Background(), 1, m.Name)
		byName[m.Name] = img
	}
	b.opts.Images = store
	r := &earlierReader{}
	b.opts.HistoryReader = r
	return b, cw, r, byName
}

// recalled is a bot turn as Chatwoot recall shows a picture it sent.
func recalled(img images.Image) memory.Turn {
	return memory.Turn{Role: "assistant", Content: profile.ImageMarker("?" + strconv.Itoa(img.Size))}
}

func TestFollowUpPicture_SendsFirstTickedPictureWithoutCaption(t *testing.T) {
	// Arrange
	b, cw, _, _ := followUpBot(t)

	// Act
	sent, err := b.FollowUpPicture(context.Background(), 1, 2, 55, 7, true)

	// Assert: "รีวิว AutoCut" (the first ticked by ID), with no caption text.
	if err != nil || !sent {
		t.Fatalf("sent %v err %v", sent, err)
	}
	if got := cw.joined(); strings.Count(got, "image:") != 1 || !strings.Contains(got, "image:|image.png|image/png") {
		t.Fatalf("sent %q, want one picture with an empty caption", got)
	}
}

func TestFollowUpPicture_SkipsPictureAlreadySentInThisChat(t *testing.T) {
	// Arrange: this chat already got "รีวิว AutoCut".
	b, cw, r, pics := followUpBot(t)
	r.turns = []memory.Turn{{Role: "user", Content: "ขอดูรีวิว"}, recalled(pics["รีวิว AutoCut"])}

	// Act
	sent, err := b.FollowUpPicture(context.Background(), 1, 2, 55, 7, true)

	// Assert: the next ticked one goes out instead.
	if err != nil || !sent || strings.Count(cw.joined(), "image:") != 1 {
		t.Fatalf("sent %v err %v images %q", sent, err, cw.joined())
	}
	if b.imageRemembered(1, 7, "รีวิว AutoCut") || !b.imageRemembered(1, 7, "รีวิว Workshop") {
		t.Errorf("want รีวิว Workshop sent and remembered, not รีวิว AutoCut")
	}
}

func TestFollowUpPicture_SkipsPictureSentInThePreviousChat(t *testing.T) {
	// Arrange: the previous chat (yesterday) got "รีวิว AutoCut".
	b, _, r, pics := followUpBot(t)
	r.earlier = memory.Earlier{When: time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC), Turns: []memory.Turn{recalled(pics["รีวิว AutoCut"])}}

	// Act
	sent, err := b.FollowUpPicture(context.Background(), 1, 2, 55, 7, true)

	// Assert
	if err != nil || !sent || !b.imageRemembered(1, 7, "รีวิว Workshop") {
		t.Fatalf("sent %v err %v: want รีวิว Workshop, the one the customer has not had", sent, err)
	}
	if r.args != [5]int64{1, 2, 55, 7} {
		t.Errorf("previous chat looked up with %v", r.args)
	}
}

func TestFollowUpPicture_PreviousChatOnlyForVerifiedCustomers(t *testing.T) {
	// Arrange: an unverified channel; its previous chat must not be read.
	b, _, r, pics := followUpBot(t)
	r.earlier = memory.Earlier{When: time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC), Turns: []memory.Turn{recalled(pics["รีวิว AutoCut"])}}

	// Act
	_, err := b.FollowUpPicture(context.Background(), 1, 2, 55, 7, false)

	// Assert
	if err != nil || r.calls != 0 {
		t.Fatalf("err %v previous chat calls %d, want 0", err, r.calls)
	}
}

func TestFollowUpPicture_NothingWhenAllTickedWereSentOrNoneTicked(t *testing.T) {
	// Arrange: case 1, both ticked pictures already sent; case 2, none ticked.
	b, cw, r, pics := followUpBot(t)
	r.turns = []memory.Turn{recalled(pics["รีวิว AutoCut"]), recalled(pics["รีวิว Workshop"])}
	b2, cw2, _, _ := followUpBot(t)
	for _, name := range []string{"รีวิว AutoCut", "รีวิว Workshop"} {
		m := pics[name].Meta
		img, _, _ := b2.opts.Images.Find(context.Background(), 1, name)
		m.ID, m.FollowUp = img.ID, false
		if err := b2.opts.Images.(*images.Memory).Update(context.Background(), m, nil); err != nil {
			t.Fatal(err)
		}
	}

	// Act
	sent, err := b.FollowUpPicture(context.Background(), 1, 2, 55, 7, true)
	sent2, err2 := b2.FollowUpPicture(context.Background(), 1, 2, 55, 7, true)

	// Assert
	if sent || err != nil || cw.joined() != "" {
		t.Errorf("all sent before: sent %v err %v %q", sent, err, cw.joined())
	}
	if sent2 || err2 != nil || cw2.joined() != "" {
		t.Errorf("none ticked: sent %v err %v %q", sent2, err2, cw2.joined())
	}
}

func TestFollowUpPicture_SendErrorIsReturned(t *testing.T) {
	// Arrange
	b, cw, _, _ := followUpBot(t)
	cw.fail = true

	// Act
	sent, err := b.FollowUpPicture(context.Background(), 1, 2, 55, 7, true)

	// Assert
	if sent || err == nil {
		t.Fatalf("sent %v err %v, want the send error", sent, err)
	}
}
