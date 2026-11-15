package nudge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/schedule"
)

var now = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

const after = 20 * time.Hour

// pricedChat is a chat that is due: the customer asked 21h ago, the bot
// answered with a price and nothing happened since.
func pricedChat() (Conversation, []gaps.Message) {
	asked := now.Add(-21 * time.Hour)
	c := Conversation{ID: 7, Status: "pending", LastActivity: asked.Add(5 * time.Second)}
	msgs := []gaps.Message{
		{ID: 1, Incoming: true, Content: "ราคาเท่าไหร่คะ", At: asked},
		{ID: 2, Outgoing: true, FromBot: true, Content: "คอร์สนี้ราคา 9,900 บาทค่ะ", At: asked.Add(5 * time.Second)},
	}
	return c, msgs
}

func TestDue_PricedQuietChat(t *testing.T) {
	// Arrange
	c, msgs := pricedChat()
	// Act
	got := Due(c, msgs, now, after)
	// Assert
	if !got {
		t.Fatal("Due = false, want true for a priced chat quiet for 21h")
	}
}

func TestDue_Rejects(t *testing.T) {
	cases := map[string]func(c *Conversation, m *[]gaps.Message){
		"staff owns it": func(c *Conversation, _ *[]gaps.Message) { c.Status = "open" },
		"resolved":      func(c *Conversation, _ *[]gaps.Message) { c.Status = "resolved" },
		"muted":         func(c *Conversation, _ *[]gaps.Message) { c.Muted = true },
		"already nudged": func(c *Conversation, _ *[]gaps.Message) {
			c.Labels = []string{"vip", Label}
		},
		"handed off": func(c *Conversation, _ *[]gaps.Message) { c.Labels = []string{"bot-handoff"} },
		"too early": func(_ *Conversation, m *[]gaps.Message) {
			(*m)[0].At = now.Add(-19 * time.Hour)
		},
		"window closing": func(_ *Conversation, m *[]gaps.Message) {
			(*m)[0].At = now.Add(-23 * time.Hour)
		},
		"no price given": func(_ *Conversation, m *[]gaps.Message) {
			(*m)[1].Content = "สนใจคอร์สไหนคะ"
		},
		"customer spoke last": func(_ *Conversation, m *[]gaps.Message) {
			*m = append(*m, gaps.Message{ID: 3, Incoming: true, Content: "ok", At: now.Add(-20*time.Hour - time.Minute)})
		},
		"staff replied in the chat": func(_ *Conversation, m *[]gaps.Message) {
			staff := gaps.Message{ID: 3, Outgoing: true, Content: "สวัสดีค่ะ", At: (*m)[0].At.Add(time.Second)}
			*m = []gaps.Message{(*m)[0], staff, (*m)[1]}
		},
		"staff left a private note": func(_ *Conversation, m *[]gaps.Message) {
			*m = append(*m, gaps.Message{ID: 3, Outgoing: true, Private: true, Content: "will call"})
		},
		"no customer message": func(_ *Conversation, m *[]gaps.Message) { *m = (*m)[1:] },
		"no messages":         func(_ *Conversation, m *[]gaps.Message) { *m = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			c, msgs := pricedChat()
			mutate(&c, &msgs)
			// Act
			got := Due(c, msgs, now, after)
			// Assert
			if got {
				t.Fatalf("Due = true, want false (%s)", name)
			}
		})
	}
}

func TestDue_BotNoteIsFine(t *testing.T) {
	// Arrange: the bot's own private note (e.g. a gap note) after its priced
	// reply is not staff taking over.
	c, msgs := pricedChat()
	msgs = append(msgs, gaps.Message{ID: 3, Outgoing: true, Private: true, FromBot: true, Content: "gap note", At: msgs[1].At})
	// Act
	got := Due(c, msgs, now, after)
	// Assert
	if !got {
		t.Fatal("Due = false, want true: a bot note is not staff taking over")
	}
}

func TestHasPrice(t *testing.T) {
	cases := map[string]bool{
		"ราคา 9,900 บาทค่ะ":  true,
		"เหลือ 4900บาท":      true,
		"only ฿1,290 today":  true,
		"฿ 590":              true,
		"Price 1,500 THB":    true,
		"costs 300 baht":     true,
		"ราคา 12.50 บาท":     true,
		"สนใจคอร์สไหนคะ":     false,
		"เปิดรับ 30 ที่นั่ง": false,
		"โทร 081 234 5678":   false,
		"บาท":                false,
		"ส่วนลด 20%":         false,
		"ลด 2 พันบาท เหลือ ... ทักมาได้ค่ะ": false,
	}
	for text, want := range cases {
		// Act
		got := HasPrice(text)
		// Assert
		if got != want {
			t.Errorf("HasPrice(%q) = %v, want %v", text, got, want)
		}
	}
}

type fakeList struct {
	accounts []int64
	convs    map[int64][]Conversation
	gotFrom  time.Time
	gotTo    time.Time
	err      error
}

func (f *fakeList) Accounts(context.Context) ([]int64, error) { return f.accounts, f.err }
func (f *fakeList) PendingBetween(_ context.Context, acct int64, from, to time.Time) ([]Conversation, error) {
	f.gotFrom, f.gotTo = from, to
	return f.convs[acct], nil
}

type fakeChat struct {
	msgs     map[int64][]gaps.Message
	status   string
	labelErr error
	sendErr  error
	labeled  []int64
	sent     map[int64]string
	calls    []string
}

func (f *fakeChat) LatestMessages(_ context.Context, _, conv int64) ([]gaps.Message, error) {
	return f.msgs[conv], nil
}
func (f *fakeChat) ConversationStatus(context.Context, int64, int64) (string, error) {
	return f.status, nil
}
func (f *fakeChat) AddLabel(_ context.Context, _, conv int64, label string) error {
	f.calls = append(f.calls, "label:"+label)
	if f.labelErr != nil {
		return f.labelErr
	}
	f.labeled = append(f.labeled, conv)
	return nil
}
func (f *fakeChat) SendMessage(_ context.Context, _, conv int64, content string) error {
	f.calls = append(f.calls, "send")
	if f.sendErr != nil {
		return f.sendErr
	}
	if f.sent == nil {
		f.sent = map[int64]string{}
	}
	f.sent[conv] = content
	return nil
}

type fakeHours struct{ s schedule.Schedule }

func (f fakeHours) Get(_ context.Context, acct int64) (schedule.Schedule, error) {
	s := f.s
	s.AccountID = acct
	return s, nil
}

func newSweeper(list *fakeList, chat *fakeChat) *Sweeper {
	return &Sweeper{
		List: list, Chat: chat, After: after, Text: "สวัสดีค่ะ คุณลูกค้า",
		Now: func() time.Time { return now }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func dueSetup() (*fakeList, *fakeChat) {
	c, msgs := pricedChat()
	return &fakeList{accounts: []int64{3}, convs: map[int64][]Conversation{3: {c}}},
		&fakeChat{msgs: map[int64][]gaps.Message{c.ID: msgs}, status: "pending"}
}

func TestSweep_NudgesOnceLabelBeforeSend(t *testing.T) {
	// Arrange
	list, chat := dueSetup()
	s := newSweeper(list, chat)
	// Act
	n := s.Sweep(context.Background())
	// Assert
	if n != 1 || chat.sent[7] != "สวัสดีค่ะ คุณลูกค้า" {
		t.Fatalf("n=%d sent=%v, want one nudge with the configured text", n, chat.sent)
	}
	if len(chat.calls) != 2 || chat.calls[0] != "label:"+Label || chat.calls[1] != "send" {
		t.Fatalf("calls = %v, want label first then send (at most once)", chat.calls)
	}
	if !list.gotFrom.Equal(now.Add(-window)) || !list.gotTo.Equal(now.Add(-after)) {
		t.Fatalf("listed %v..%v, want %v..%v", list.gotFrom, list.gotTo, now.Add(-window), now.Add(-after))
	}
}

func TestSweep_LabelFailsSendsNothing(t *testing.T) {
	// Arrange
	list, chat := dueSetup()
	chat.labelErr = errors.New("chatwoot down")
	// Act
	n := newSweeper(list, chat).Sweep(context.Background())
	// Assert
	if n != 0 || len(chat.sent) != 0 {
		t.Fatalf("n=%d sent=%v, want nothing sent when the label cannot be set", n, chat.sent)
	}
}

func TestSweep_SendFailsIsNotCounted(t *testing.T) {
	// Arrange
	list, chat := dueSetup()
	chat.sendErr = errors.New("meta window closed")
	// Act
	n := newSweeper(list, chat).Sweep(context.Background())
	// Assert
	if n != 0 || len(chat.labeled) != 1 {
		t.Fatalf("n=%d labeled=%v, want 0 counted and the label kept (no retry)", n, chat.labeled)
	}
}

func TestSweep_StaffTookItSinceListing(t *testing.T) {
	// Arrange
	list, chat := dueSetup()
	chat.status = "open"
	// Act
	n := newSweeper(list, chat).Sweep(context.Background())
	// Assert
	if n != 0 || len(chat.calls) != 0 {
		t.Fatalf("n=%d calls=%v, want nothing done once staff own the chat", n, chat.calls)
	}
}

func TestSweep_Off(t *testing.T) {
	cases := map[string]func(s *Sweeper){
		"after zero": func(s *Sweeper) { s.After = 0 },
		"no text":    func(s *Sweeper) { s.Text = "  " },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			list, chat := dueSetup()
			s := newSweeper(list, chat)
			mutate(s)
			// Act
			n := s.Sweep(context.Background())
			// Assert
			if n != 0 || len(chat.calls) != 0 {
				t.Fatalf("n=%d calls=%v, want nothing when off", n, chat.calls)
			}
		})
	}
}

func TestSweep_OnlyListedAccounts(t *testing.T) {
	// Arrange
	list, chat := dueSetup()
	s := newSweeper(list, chat)
	s.Accounts = map[int64]bool{1: true}
	// Act
	n := s.Sweep(context.Background())
	// Assert
	if n != 0 || len(chat.calls) != 0 {
		t.Fatalf("n=%d calls=%v, want account 3 skipped when only 1 is on", n, chat.calls)
	}
}

func TestSweep_BotSwitchedOff(t *testing.T) {
	// Arrange
	list, chat := dueSetup()
	s := newSweeper(list, chat)
	off := schedule.Default(0)
	off.Mode = schedule.ModeOff
	s.Hours = fakeHours{s: off}
	// Act
	n := s.Sweep(context.Background())
	// Assert
	if n != 0 || len(chat.calls) != 0 {
		t.Fatalf("n=%d calls=%v, want no nudge while the bot is switched off", n, chat.calls)
	}
}
