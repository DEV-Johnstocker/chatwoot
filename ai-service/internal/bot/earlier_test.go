package bot

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/images"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
)

// earlierReader also returns the customer's previous chat.
type earlierReader struct {
	fakeReader
	earlier memory.Earlier
	err     error
	calls   int
	args    [5]int64
	limit   int
}

func (f *earlierReader) PreviousChat(_ context.Context, acct, inbox, contact, conv int64, limit int) (memory.Earlier, error) {
	f.calls++
	f.args, f.limit = [5]int64{acct, inbox, contact, conv}, limit
	return f.earlier, f.err
}

// returningMsg is a customer message from contact 55 in inbox 2.
func returningMsg(text string) Job {
	j := customerMsg(text)
	j.Event.Inbox.ID, j.Event.Sender.ID, j.Event.Sender.Type = 2, 55, "contact"
	return j
}

func systemText(msgs []llm.Message) string {
	var s []string
	for _, m := range msgs {
		if m.Role == "system" {
			s = append(s, m.Content)
		}
	}
	return strings.Join(s, "\n")
}

func pngOf(t *testing.T, side int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, side, side))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestEarlier_NewChatSeesTheCustomersPreviousChat(t *testing.T) {
	// Arrange: a returning customer opens a new chat.
	cw, l := &fakeChatwoot{}, &fakeLLM{reply: "ได้ครับ"}
	b, h := newBot(cw, l)
	r := &earlierReader{earlier: memory.Earlier{
		When: time.Date(2026, 11, 18, 23, 10, 0, 0, time.UTC),
		Turns: []memory.Turn{
			{Role: "user", Content: "ใช้ Windows 11 ครับ"},
			{Role: "assistant", Content: "ราคาโปร 3,900 บาทครับ\n[[IMAGE: ?209108]]"},
		},
	}}
	b.opts.HistoryReader = r

	// Act
	b.Process(context.Background(), returningMsg("สนใจครับ"))

	// Assert
	if r.calls != 1 || r.args != [5]int64{1, 2, 55, 7} || r.limit != earlierTurns {
		t.Fatalf("previous chat calls=%d args=%v limit=%d", r.calls, r.args, r.limit)
	}
	sys := systemText(l.got)
	for _, want := range []string{"Earlier chat with this customer", "2026-11-18", "Customer: ใช้ Windows 11 ครับ", "Shop: ราคาโปร 3,900 บาทครับ [sent a picture]"} {
		if !strings.Contains(sys, want) {
			t.Errorf("system prompt lacks %q:\n%s", want, sys)
		}
	}
	if strings.Contains(sys, "?209108") {
		t.Error("the model must not see the recalled picture marker")
	}
	for _, turn := range h.Get(1, 7) {
		if strings.Contains(turn.Content, "Windows 11") {
			t.Errorf("the earlier chat must stay out of this chat's turns: %+v", h.Get(1, 7))
		}
	}
}

func TestEarlier_PicturesFromThePreviousChatAreNotSentAgain(t *testing.T) {
	// Arrange: two pictures of different sizes; the earlier chat sent the first.
	auto, work := pngOf(t, 2), pngOf(t, 40)
	cw, l := &imageChatwoot{}, &fakeLLM{reply: "นี่รีวิวครับ\n[[IMAGE: รีวิว AutoCut]]\n[[IMAGE: รีวิว Workshop]]"}
	b, _ := newBot(&cw.fakeChatwoot, l)
	b.opts.Chatwoot = cw
	b.opts.Profiles = &fakeProfiles{byAccount: map[int64]profile.Profile{1: {AccountID: 1, BusinessName: "AutoCut"}}}
	store := &images.Memory{}
	for _, img := range []images.Image{
		{Meta: images.Meta{AccountID: 1, Name: "รีวิว AutoCut", When: "ขอดูรีวิว"}, Data: auto},
		{Meta: images.Meta{AccountID: 1, Name: "รีวิว Workshop", When: "ถามรีวิว Workshop"}, Data: work},
	} {
		if _, err := store.Add(context.Background(), img); err != nil {
			t.Fatal(err)
		}
	}
	b.opts.Images = store
	// Chatwoot reports the stored bytes' size (the upload is re-encoded on save).
	sent, ok, err := store.Find(context.Background(), 1, "รีวิว AutoCut")
	if err != nil || !ok {
		t.Fatal(err)
	}
	b.opts.HistoryReader = &earlierReader{earlier: memory.Earlier{
		When:  time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC),
		Turns: []memory.Turn{{Role: "assistant", Content: profile.ImageMarker("?" + strconv.Itoa(sent.Size))}},
	}}

	// Act
	b.Process(context.Background(), returningMsg("ขอดู review ครับ"))

	// Assert
	if other, _, _ := store.Find(context.Background(), 1, "รีวิว Workshop"); other.Size == sent.Size {
		t.Fatal("the two test pictures must differ in size")
	}
	if n := strings.Count(cw.joined(), "image:"); n != 1 {
		t.Fatalf("images sent %d, want only the one the customer has not seen: %s", n, cw.joined())
	}
}

func TestEarlier_FirstChatIsUnchanged(t *testing.T) {
	// Arrange: no previous chat.
	l := &fakeLLM{reply: "ok"}
	b, _ := newBot(&fakeChatwoot{}, l)
	b.opts.HistoryReader = &earlierReader{}

	// Act
	b.Process(context.Background(), returningMsg("ราคา"))

	// Assert
	if strings.Contains(systemText(l.got), "Earlier chat") {
		t.Error("a first chat must get no earlier-chat section")
	}
}

func TestEarlier_ReaderErrorStillAnswers(t *testing.T) {
	// Arrange
	cw := &fakeChatwoot{}
	b, _ := newBot(cw, &fakeLLM{reply: "ตอบได้ครับ"})
	b.opts.HistoryReader = &earlierReader{err: errors.New("chatwoot down")}

	// Act
	b.Process(context.Background(), returningMsg("ราคา"))

	// Assert
	if !strings.Contains(cw.joined(), "msg:ตอบได้ครับ") || strings.Contains(cw.joined(), "handoff") {
		t.Errorf("a failed earlier-chat lookup must not stop or hand off the reply: %s", cw.joined())
	}
}

func TestEarlier_OwnMemoryWins(t *testing.T) {
	// Arrange: the bot already remembers this chat.
	b, h := newBot(&fakeChatwoot{}, &fakeLLM{reply: "ok"})
	r := &earlierReader{}
	b.opts.HistoryReader = r
	h.Append(1, 7, memory.Turn{Role: "user", Content: "ก่อนหน้า"})

	// Act
	b.Process(context.Background(), returningMsg("ต่อ"))

	// Assert
	if r.calls != 0 {
		t.Errorf("previous chat calls = %d, want 0 while the bot remembers this chat", r.calls)
	}
}

func TestEarlier_LongTurnsAreCut(t *testing.T) {
	// Arrange
	l := &fakeLLM{reply: "ok"}
	b, _ := newBot(&fakeChatwoot{}, l)
	long := strings.Repeat("ก", 2000)
	b.opts.HistoryReader = &earlierReader{earlier: memory.Earlier{Turns: []memory.Turn{{Role: "user", Content: long}}}}

	// Act
	b.Process(context.Background(), returningMsg("ราคา"))

	// Assert
	if strings.Contains(systemText(l.got), strings.Repeat("ก", earlierTurnRunes+1)) {
		t.Errorf("an earlier turn must be cut to %d characters", earlierTurnRunes)
	}
}

func TestImageSentBefore_RecalledPictureMatchesBySize(t *testing.T) {
	// Arrange
	recalled := []memory.Turn{{Role: "assistant", Content: "[[IMAGE: ?209108]]"}}
	same := images.Meta{Name: "รีวิวผู้ซื้อ", Size: 209108}
	other := images.Meta{Name: "รีวิวครีเอเตอร์", Size: 138474}

	// Act
	gotSame, gotOther := imageSentBefore(recalled, same), imageSentBefore(recalled, other)

	// Assert
	if !gotSame || gotOther {
		t.Errorf("same size sent=%v (want true), other size sent=%v (want false)", gotSame, gotOther)
	}
}
