package ratings

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
)

var t0 = time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)

func msg(id int64, kind string, s string) gaps.Message {
	m := gaps.Message{ID: id, Content: s, At: t0.Add(time.Duration(id) * time.Minute)}
	switch kind {
	case "in":
		m.Incoming = true
	case "bot":
		m.Outgoing, m.FromBot = true, true
	case "staff":
		m.Outgoing = true
	case "note":
		m.Outgoing, m.Private, m.FromBot = true, true, true
	}
	return m
}

func TestReplies_PairsEachBotReplyWithTheCustomerTurn(t *testing.T) {
	// Arrange
	msgs := []gaps.Message{
		msg(1, "in", "สวัสดีครับ"), msg(2, "in", "คอร์สราคาเท่าไหร่"), msg(3, "bot", "3,900 บาทค่ะ"),
		msg(4, "note", "private"), msg(5, "staff", "แอดมินตอบ"), msg(6, "bot", "มีอะไรสอบถามเพิ่มไหมคะ"),
		msg(7, "in", "ขอลิงก์"), msg(8, "bot", "https://example.com/course"), msg(9, "bot", ""),
	}

	// Act
	rs := Replies(msgs)

	// Assert
	if len(rs) != 3 {
		t.Fatalf("replies = %+v", rs)
	}
	if rs[0].MessageID != 3 || rs[0].Customer != "สวัสดีครับ\nคอร์สราคาเท่าไหร่" || rs[0].Bot != "3,900 บาทค่ะ" {
		t.Errorf("first = %+v", rs[0])
	}
	if rs[1].MessageID != 6 || rs[1].Customer != "" {
		t.Errorf("after a staff reply the bot answered nothing new: %+v", rs[1])
	}
	if rs[2].Customer != "ขอลิงก์" || !rs[2].At.Equal(t0.Add(8*time.Minute)) {
		t.Errorf("third = %+v", rs[2])
	}
}

func TestClean_RulesForReasons(t *testing.T) {
	cases := map[string]struct {
		r    Rating
		want error
	}{
		"good drops a reason": {Rating{Good: true, Reason: "tone"}, nil},
		"bad needs a reason":  {Rating{}, ErrNoReason},
		"unknown reason":      {Rating{Reason: "drop table"}, ErrBadReason},
		"bad with a reason":   {Rating{Reason: "missed_sale"}, nil},
		"missing ids":         {Rating{Good: true, MessageID: -1}, ErrBadIDs},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			r := c.r
			if r.MessageID == 0 {
				r.AccountID, r.ConversationID, r.MessageID = 1, 2, 3
			}

			// Act
			err := r.Clean()

			// Assert
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if r.Good && r.Reason != "" {
				t.Error("a good rating keeps no reason")
			}
		})
	}
}

func TestClean_CapsText(t *testing.T) {
	// Arrange
	r := Rating{AccountID: 1, ConversationID: 2, MessageID: 3, Good: true,
		Note: strings.Repeat("ก", 5000), Customer: strings.Repeat("ข", 5000), Bot: strings.Repeat("ค", 9000)}

	// Act
	err := r.Clean()

	// Assert
	if err != nil || len([]rune(r.Note)) != MaxNote || len([]rune(r.Customer)) != MaxCustomer || len([]rune(r.Bot)) != MaxBot {
		t.Fatalf("err=%v note=%d customer=%d bot=%d", err, len([]rune(r.Note)), len([]rune(r.Customer)), len([]rune(r.Bot)))
	}
}

type fakeSource struct {
	chats []gaps.Chat
	msgs  map[int64][]gaps.Message
	err   error
}

func (f *fakeSource) RecentChats(context.Context, int64, int) ([]gaps.Chat, error) {
	return f.chats, f.err
}

func (f *fakeSource) LatestMessages(_ context.Context, _, conv int64) ([]gaps.Message, error) {
	if m, ok := f.msgs[conv]; ok {
		return m, nil
	}
	return nil, errors.New("404")
}

func TestFeed_NewestFirstAcrossChats(t *testing.T) {
	// Arrange
	src := &fakeSource{
		chats: []gaps.Chat{{ID: 10, InboxID: 1}, {ID: 20, InboxID: 2}, {ID: 30}},
		msgs: map[int64][]gaps.Message{
			10: {msg(1, "in", "a"), msg(2, "bot", "reply a")},
			20: {msg(3, "in", "b"), msg(4, "bot", "reply b")},
		},
	}

	// Act
	rs, err := (&Feed{Source: src, Workers: 2}).Recent(context.Background(), 1, 1)

	// Assert
	if err != nil || len(rs) != 2 {
		t.Fatalf("rs=%+v err=%v", rs, err)
	}
	if rs[0].MessageID != 4 || rs[0].ConversationID != 20 || rs[0].InboxID != 2 || rs[1].ConversationID != 10 {
		t.Errorf("order or ids wrong: %+v", rs)
	}
}

func TestFeed_ListError(t *testing.T) {
	// Arrange
	src := &fakeSource{err: errors.New("401")}

	// Act
	_, err := (&Feed{Source: src}).Recent(context.Background(), 1, 1)

	// Assert
	if err == nil {
		t.Fatal("want the error")
	}
}
