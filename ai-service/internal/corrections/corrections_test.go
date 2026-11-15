package corrections

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/pending"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in, answer string
		ok         bool
	}{
		{"#fix 3,900 บาท", "3,900 บาท", true},
		{"  #FIX\nบรรทัดแรก\nบรรทัดสอง ", "บรรทัดแรก\nบรรทัดสอง", true},
		{`\#fix escaped by the editor`, "escaped by the editor", true},
		{"#fix: with a colon", "with a colon", true},
		{"#แก้ ราคา 3,900", "ราคา 3,900", true},
		{"#แก้ไข ราคา 3,900", "ราคา 3,900", true},
		{"#fix", "", true},
		{"#fixed it already", "", false},
		{"#แก้ว", "", false},
		{"please #fix this", "", false},
		{"ลูกค้าโอนแล้ว", "", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			// Act
			answer, ok := Parse(c.in)

			// Assert
			if ok != c.ok || answer != c.answer {
				t.Fatalf("Parse(%q) = %q, %v; want %q, %v", c.in, answer, ok, c.answer, c.ok)
			}
		})
	}
}

type fakeMessages struct {
	msgs []gaps.Message
	err  error
}

func (f fakeMessages) LatestMessages(context.Context, int64, int64) ([]gaps.Message, error) {
	return f.msgs, f.err
}

func chat() []gaps.Message {
	at := time.Unix(1_700_000_000, 0)
	return []gaps.Message{
		{ID: 1, Incoming: true, Content: "สวัสดี", At: at},
		{ID: 2, Outgoing: true, FromBot: true, Content: "สวัสดีครับ", At: at},
		{ID: 3, Incoming: true, Content: "คอร์สราคาเท่าไหร่", At: at},
		{ID: 4, Incoming: true, Content: "มีโปรไหม", At: at},
		{ID: 5, Outgoing: true, FromBot: true, Content: "ไม่ทราบครับ", At: at},
		{ID: 6, Outgoing: true, Private: true, Content: "#fix 3,900 บาท", At: at},
		{ID: 7, Incoming: true, Content: "ข้อความหลังโน้ต", At: at},
	}
}

func proposed(t *testing.T, st *pending.Memory) (pending.Change, map[string]any) {
	t.Helper()
	list, _ := st.List(context.Background(), 1)
	if len(list) != 1 {
		t.Fatalf("waiting = %d", len(list))
	}
	var p map[string]any
	if err := json.Unmarshal(list[0].Proposed, &p); err != nil {
		t.Fatal(err)
	}
	return list[0], p
}

func TestPropose_UsesTheExchangeBeforeTheNote(t *testing.T) {
	// Arrange
	st := &pending.Memory{}
	svc := &Service{Pending: st, Messages: fakeMessages{msgs: chat()}}

	// Act
	_, err := svc.Propose(context.Background(), Note{AccountID: 1, ConversationID: 12, NoteID: 6, Staff: "Nok", Answer: "3,900 บาท"})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	c, p := proposed(t, st)
	if c.Kind != pending.KindDocument || c.Target != "staff-fix-12-6" || c.Actor != "Nok (chat #12)" {
		t.Fatalf("change = %+v", c)
	}
	staff := p["staff"].(map[string]any)
	if p["title"] != "คอร์สราคาเท่าไหร่\nมีโปรไหม" || p["content"] != "3,900 บาท" || p["kind"] != "faq" || p["base"] != "" ||
		staff["bot_reply"] != "ไม่ทราบครับ" || staff["name"] != "Nok" {
		t.Fatalf("proposal = %v", p)
	}
}

func TestPropose_FallsBackToMemory(t *testing.T) {
	// Arrange: Chatwoot cannot be read.
	st := &pending.Memory{}
	svc := &Service{Pending: st, Messages: fakeMessages{err: errors.New("down")}}
	turns := []memory.Turn{{Role: "user", Content: "ราคา?"}, {Role: "assistant", Content: "ไม่ทราบ"},
		{Role: "assistant", Content: memory.PageMessagePrefix + "page post"}}

	// Act
	_, err := svc.Propose(context.Background(), Note{AccountID: 1, ConversationID: 12, NoteID: 9, Answer: "3,900", Memory: turns})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	c, p := proposed(t, st)
	if p["title"] != "ราคา?" || p["staff"].(map[string]any)["bot_reply"] != "ไม่ทราบ" || c.Actor != "Staff (chat #12)" {
		t.Fatalf("proposal = %v actor %q", p, c.Actor)
	}
}

func TestPropose_NoBotReplyUsesTheCustomersLatestMessages(t *testing.T) {
	// Arrange
	st := &pending.Memory{}
	msgs := []gaps.Message{{ID: 1, Incoming: true, Content: "ส่งสลิปแล้ว"}, {ID: 2, Outgoing: true, Content: "staff reply"},
		{ID: 3, Incoming: true, Content: "เปิดคอร์สให้หรือยัง"}}
	svc := &Service{Pending: st, Messages: fakeMessages{msgs: msgs}}

	// Act
	_, err := svc.Propose(context.Background(), Note{AccountID: 1, ConversationID: 12, NoteID: 4, Answer: "เปิดภายใน 24 ชม."})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if _, p := proposed(t, st); p["title"] != "เปิดคอร์สให้หรือยัง" {
		t.Fatalf("title = %v", p["title"])
	}
}

func TestPropose_Errors(t *testing.T) {
	cases := map[string]struct {
		note Note
		want error
	}{
		"no answer":   {Note{AccountID: 1, ConversationID: 12, NoteID: 6, Answer: "  "}, ErrNoAnswer},
		"no question": {Note{AccountID: 1, ConversationID: 12, NoteID: 1, Answer: "3,900"}, ErrNoQuestion},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			st := &pending.Memory{}
			svc := &Service{Pending: st, Messages: fakeMessages{msgs: chat()}}

			// Act
			_, err := svc.Propose(context.Background(), c.note)

			// Assert
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if n, _ := st.Count(context.Background(), 1); n != 0 {
				t.Fatalf("stored %d", n)
			}
		})
	}
}

func TestPropose_CapsAndCleansText(t *testing.T) {
	// Arrange
	st := &pending.Memory{}
	long := strings.Repeat("ก", 5000)
	svc := &Service{Pending: st, Messages: fakeMessages{msgs: []gaps.Message{
		{ID: 1, Incoming: true, Content: strings.Repeat("ถ", 400)},
		{ID: 2, Outgoing: true, FromBot: true, Content: long}}}}

	// Act
	_, err := svc.Propose(context.Background(), Note{AccountID: 1, ConversationID: 12, NoteID: 3,
		Staff: "N\x00ok", Answer: long + "\x00\xff"})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	_, p := proposed(t, st)
	for field, max := range map[string]int{"title": 300, "content": MaxAnswer} {
		if n := len([]rune(p[field].(string))); n != max {
			t.Errorf("%s has %d characters, want %d", field, n, max)
		}
	}
	staff := p["staff"].(map[string]any)
	if len([]rune(staff["bot_reply"].(string))) != MaxBotReply || staff["name"] != "Nok" {
		t.Errorf("staff = %v", staff["name"])
	}
	if strings.Contains(p["content"].(string), "\x00") {
		t.Error("NUL kept")
	}
}

func TestPropose_RedeliveryUpdatesTheSameChange(t *testing.T) {
	// Arrange
	st := &pending.Memory{}
	svc := &Service{Pending: st, Messages: fakeMessages{msgs: chat()}}
	n := Note{AccountID: 1, ConversationID: 12, NoteID: 6, Answer: "3,900"}

	// Act
	first, _ := svc.Propose(context.Background(), n)
	second, _ := svc.Propose(context.Background(), n)

	// Assert
	if first != second {
		t.Fatalf("ids %d and %d", first, second)
	}
	if c, _ := st.Count(context.Background(), 1); c != 1 {
		t.Fatalf("waiting = %d", c)
	}
}
