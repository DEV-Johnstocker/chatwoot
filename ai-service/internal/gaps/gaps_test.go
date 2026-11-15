package gaps

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)

func in(id int64, s string) Message { return Message{ID: id, Incoming: true, Content: s} }
func bot(id int64, s string) Message {
	return Message{ID: id, Outgoing: true, FromBot: true, Content: s}
}
func staff(id int64, s string) Message {
	return Message{ID: id, Outgoing: true, Content: s}
}

func gapNote(id int64) Message {
	return Message{ID: id, Outgoing: true, Private: true, FromBot: true, At: t0,
		Content: "📚 **" + NoteTitle + "** · สรุปจากบอท\n\n**เหตุผล:** ไม่มีข้อมูล\n**ลูกค้าต้องการ:** อยากรู้ว่าคอร์สมีใบประกาศไหม\n\nai-handoff:ai-request"}
}

func TestRead_FindsQuestionSummaryAndStaffReply(t *testing.T) {
	// Arrange
	msgs := []Message{
		in(1, "สวัสดีครับ"), bot(2, "สวัสดีค่ะ"),
		in(3, "คอร์สนี้"), in(4, "มีใบประกาศไหมครับ"),
		bot(5, "ขอส่งให้ทีมงานตอบนะคะ"), gapNote(6),
		{ID: 7, Outgoing: true, Private: true, Content: "staff note"},
		staff(8, "มีค่ะ ได้ใบประกาศเมื่อเรียนจบ"), in(9, "ขอบคุณครับ"),
	}

	// Act
	g, ok := Read(msgs)

	// Assert
	if !ok || !g.NoteFound || !g.At.Equal(t0) {
		t.Fatalf("gap not found: %+v", g)
	}
	if g.Asked != "คอร์สนี้\nมีใบประกาศไหมครับ" {
		t.Errorf("asked = %q", g.Asked)
	}
	if g.Wanted != "อยากรู้ว่าคอร์สมีใบประกาศไหม" {
		t.Errorf("wanted = %q", g.Wanted)
	}
	if g.StaffReply != "มีค่ะ ได้ใบประกาศเมื่อเรียนจบ" {
		t.Errorf("staff reply = %q", g.StaffReply)
	}
	if g.Question() != g.Asked {
		t.Errorf("question = %q", g.Question())
	}
}

func TestRead_UsesTheLatestGapNote(t *testing.T) {
	// Arrange
	msgs := []Message{in(1, "old question"), gapNote(2), staff(3, "old answer"), in(4, "new question"), gapNote(5)}

	// Act
	g, _ := Read(msgs)

	// Assert
	if g.Asked != "new question" || g.StaffReply != "" {
		t.Errorf("got %+v", g)
	}
}

func TestRead_IgnoresNotesNotWrittenByTheBot(t *testing.T) {
	// Arrange: staff pasting the title into their own note is not a gap.
	n := gapNote(2)
	n.FromBot = false

	// Act
	_, ok := Read([]Message{in(1, "q"), n})

	// Assert
	if ok {
		t.Fatal("a staff note must not count as the bot's gap note")
	}
}

func TestRead_NoCustomerWordsFallsBackToSummary(t *testing.T) {
	// Arrange: the customer only sent a photo (empty text).
	msgs := []Message{in(1, ""), gapNote(2)}

	// Act
	g, _ := Read(msgs)

	// Assert
	if g.Asked != "" || g.Question() != "อยากรู้ว่าคอร์สมีใบประกาศไหม" {
		t.Errorf("asked %q question %q", g.Asked, g.Question())
	}
}

func TestRead_CutsLongText(t *testing.T) {
	// Arrange
	msgs := []Message{in(1, strings.Repeat("ก", 900)), gapNote(2), staff(3, strings.Repeat("ข", 5000))}

	// Act
	g, _ := Read(msgs)

	// Assert
	if n := len([]rune(g.Asked)); n != maxAsked {
		t.Errorf("asked has %d characters, want %d", n, maxAsked)
	}
	if n := len([]rune(g.StaffReply)); n != maxReply {
		t.Errorf("reply has %d characters, want %d", n, maxReply)
	}
}

type fakeSource struct {
	chats    []Chat
	msgs     map[int64][]Message
	listErr  error
	msgErr   error
	replaced []string
}

func (f *fakeSource) GapChats(_ context.Context, _ int64, limit int) ([]Chat, error) {
	if limit > 0 && limit < len(f.chats) {
		return f.chats[:limit], f.listErr
	}
	return f.chats, f.listErr
}

func (f *fakeSource) ChatMessages(_ context.Context, _, conv int64) ([]Message, error) {
	return f.msgs[conv], f.msgErr
}

func (f *fakeSource) ReplaceLabel(_ context.Context, _, conv int64, from, to string) error {
	f.replaced = append(f.replaced, from+">"+to)
	return nil
}

func TestFinder_ListKeepsOrderAndChatsWithoutANote(t *testing.T) {
	// Arrange
	src := &fakeSource{
		chats: []Chat{{ID: 30, InboxID: 2, LastActivity: t0}, {ID: 20, InboxID: 1, LastActivity: t0}},
		msgs:  map[int64][]Message{20: {in(1, "q20"), gapNote(2)}},
	}

	// Act
	gs, err := (&Finder{Source: src, Workers: 1}).List(context.Background(), 1, 25)

	// Assert
	if err != nil || len(gs) != 2 {
		t.Fatalf("gs=%+v err=%v", gs, err)
	}
	if gs[0].ConversationID != 30 || gs[0].NoteFound || gs[0].InboxID != 2 {
		t.Errorf("first = %+v", gs[0])
	}
	if gs[1].ConversationID != 20 || gs[1].Asked != "q20" {
		t.Errorf("second = %+v", gs[1])
	}
}

func TestFinder_ListError(t *testing.T) {
	// Arrange
	src := &fakeSource{listErr: errors.New("401")}

	// Act
	_, err := (&Finder{Source: src}).List(context.Background(), 1, 25)

	// Assert
	if err == nil {
		t.Fatal("want the list error")
	}
}

func TestFinder_DoneSwapsTheLabel(t *testing.T) {
	// Arrange
	src := &fakeSource{}

	// Act
	err := (&Finder{Source: src}).Done(context.Background(), 1, 5)

	// Assert
	if err != nil || len(src.replaced) != 1 || src.replaced[0] != "kb-gap>kb-gap-done" {
		t.Fatalf("err=%v replaced=%v", err, src.replaced)
	}
}
