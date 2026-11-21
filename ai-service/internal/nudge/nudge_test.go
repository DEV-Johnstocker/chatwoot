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
		"staff owns it":     func(c *Conversation, _ *[]gaps.Message) { c.Status = "open" },
		"resolved":          func(c *Conversation, _ *[]gaps.Message) { c.Status = "resolved" },
		"muted":             func(c *Conversation, _ *[]gaps.Message) { c.Muted = true },
		"assigned to staff": func(c *Conversation, _ *[]gaps.Message) { c.Assigned = true },
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
			*m = append(*m, gaps.Message{ID: 3, Incoming: true, Content: "ผ่อนได้ไหมคะ", At: now.Add(-20*time.Hour - time.Minute)})
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
	lastTo   map[int64]time.Time
	err      error
}

func (f *fakeList) Accounts(context.Context) ([]int64, error) { return f.accounts, f.err }
func (f *fakeList) PendingBetween(_ context.Context, acct int64, from, to time.Time) ([]Conversation, error) {
	f.gotFrom, f.gotTo = from, to
	if f.lastTo == nil {
		f.lastTo = map[int64]time.Time{}
	}
	f.lastTo[acct] = to
	return f.convs[acct], nil
}

type fakeChat struct {
	msgs     map[int64][]gaps.Message
	status   string
	assignee int64
	hasLabel bool
	labelErr error
	sendErr  error
	labeled  []int64
	sent     map[int64]string
	calls    []string
}

func (f *fakeChat) ChatMessages(_ context.Context, _, conv int64) ([]gaps.Message, error) {
	return f.msgs[conv], nil
}
func (f *fakeChat) ConversationStatus(context.Context, int64, int64) (string, error) {
	return f.status, nil
}
func (f *fakeChat) Assignee(context.Context, int64, int64) (int64, error) {
	return f.assignee, nil
}
func (f *fakeChat) AddLabelIfMissing(_ context.Context, _, conv int64, label string) (bool, error) {
	f.calls = append(f.calls, "label:"+label)
	if f.labelErr != nil {
		return false, f.labelErr
	}
	if f.hasLabel {
		return false, nil
	}
	f.labeled = append(f.labeled, conv)
	return true, nil
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

// fixedRules gives every account the same rule, or the account's own.
type fixedRules struct {
	all  Rule
	each map[int64]Rule
}

func (f *fixedRules) For(acct int64) Rule {
	if r, ok := f.each[acct]; ok {
		return r
	}
	return f.all
}

func newSweeper(list *fakeList, chat *fakeChat) *Sweeper {
	return &Sweeper{
		List: list, Chat: chat, Rules: &fixedRules{all: Rule{On: true, After: after, Text: "สวัสดีค่ะ คุณลูกค้า"}},
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
		"switched off": func(s *Sweeper) { s.Rules.(*fixedRules).all.On = false },
		"after zero":   func(s *Sweeper) { s.Rules.(*fixedRules).all.After = 0 },
		"no text":      func(s *Sweeper) { s.Rules.(*fixedRules).all.Text = "  " },
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

func TestSweep_UsesEachAccountsRule(t *testing.T) {
	// Arrange: account 3 is off; account 4 waits longer than the chat is old.
	list, chat := dueSetup()
	c := list.convs[3][0]
	list.accounts = []int64{3, 4}
	list.convs[4] = []Conversation{c}
	s := newSweeper(list, chat)
	rules := s.Rules.(*fixedRules)
	rules.each = map[int64]Rule{3: {On: false, After: after, Text: "x"}, 4: {On: true, After: 22 * time.Hour, Text: "x"}}
	// Act
	n := s.Sweep(context.Background())
	// Assert
	if n != 0 || len(chat.calls) != 0 {
		t.Fatalf("n=%d calls=%v, want nothing: 3 is off and 4's wait is not over", n, chat.calls)
	}
	if list.lastTo[4] != now.Add(-22*time.Hour) {
		t.Fatalf("account 4 listed up to %v, want its own 22h wait", list.lastTo[4])
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

func TestSweep_StaffAssignedSinceListing(t *testing.T) {
	// Arrange
	list, chat := dueSetup()
	chat.assignee = 12
	// Act
	n := newSweeper(list, chat).Sweep(context.Background())
	// Assert
	if n != 0 || len(chat.calls) != 0 {
		t.Fatalf("n=%d calls=%v, want nothing done once a staff member is assigned", n, chat.calls)
	}
}

func TestSweep_LabelAlreadyThereSendsNothing(t *testing.T) {
	// Arrange: another sweep labelled the chat between listing and now.
	list, chat := dueSetup()
	chat.hasLabel = true
	// Act
	n := newSweeper(list, chat).Sweep(context.Background())
	// Assert
	if n != 0 || len(chat.sent) != 0 {
		t.Fatalf("n=%d sent=%v, want no second follow-up", n, chat.sent)
	}
}

// The bot no longer answers a like or a second "thank you" (internal/ack),
// so such a message can be the last one; the follow-up still goes out.
// The quiet time counts from it, inside Messenger's 24-hour window.
// Issue: DEV-Johnstocker/chatwoot#1
func TestDue_LooksPastALikeOrThanks(t *testing.T) {
	for _, content := range []string{"ขอบคุณครับ", "👍", ""} {
		// Arrange: a like sticker arrives with no text.
		c, msgs := pricedChat()
		msgs = append(msgs, gaps.Message{ID: 3, Incoming: true, Content: content, At: now.Add(-20*time.Hour - time.Minute)})

		// Act
		got := Due(c, msgs, now, after)

		// Assert
		if !got {
			t.Errorf("%q after the bot's priced reply: Due = false, want true", content)
		}
	}
}

func TestDue_LikeTooRecentWaits(t *testing.T) {
	// Arrange: the like came an hour ago, so the quiet time starts there.
	c, msgs := pricedChat()
	msgs = append(msgs, gaps.Message{ID: 3, Incoming: true, Content: "👍", At: now.Add(-time.Hour)})

	// Act / Assert
	if Due(c, msgs, now, after) {
		t.Error("nudged one hour after the customer's like")
	}
}

// fakePictures records the follow-up picture call, as bot.Bot would send it.
type fakePictures struct {
	chat *fakeChat
	got  []Conversation
	sent bool
	err  error
}

func (f *fakePictures) FollowUpPicture(_ context.Context, _ int64, c Conversation) (bool, error) {
	f.chat.calls = append(f.chat.calls, "picture")
	f.got = append(f.got, c)
	return f.sent, f.err
}

func TestSweep_PictureGoesAfterTheText(t *testing.T) {
	// Arrange
	list, chat := dueSetup()
	list.convs[3][0].InboxID, list.convs[3][0].ContactID, list.convs[3][0].Channel = 2, 55, "Channel::FacebookPage"
	pics := &fakePictures{chat: chat, sent: true}
	s := newSweeper(list, chat)
	s.Pictures = pics

	// Act
	n := s.Sweep(context.Background())

	// Assert
	if n != 1 || len(chat.calls) != 3 || chat.calls[1] != "send" || chat.calls[2] != "picture" {
		t.Fatalf("n=%d calls=%v, want label, text, then the picture", n, chat.calls)
	}
	if got := pics.got[0]; got.ID != 7 || got.InboxID != 2 || got.ContactID != 55 || got.Channel != "Channel::FacebookPage" {
		t.Errorf("picture asked for %+v, want the listed chat with its inbox, contact and channel", got)
	}
}

func TestSweep_PictureErrorStillCountsTheText(t *testing.T) {
	// Arrange
	list, chat := dueSetup()
	s := newSweeper(list, chat)
	s.Pictures = &fakePictures{chat: chat, err: errors.New("chatwoot down")}

	// Act
	n := s.Sweep(context.Background())

	// Assert: the text went out once; the picture is not retried.
	if n != 1 || chat.sent[7] == "" || len(chat.calls) != 3 {
		t.Fatalf("n=%d sent=%v calls=%v, want the follow-up counted", n, chat.sent, chat.calls)
	}
}

func TestSweep_NoPictureWhenTheTextFails(t *testing.T) {
	// Arrange
	list, chat := dueSetup()
	chat.sendErr = errors.New("meta window closed")
	s := newSweeper(list, chat)
	s.Pictures = &fakePictures{chat: chat, sent: true}

	// Act
	s.Sweep(context.Background())

	// Assert
	for _, c := range chat.calls {
		if c == "picture" {
			t.Fatalf("calls=%v: a picture must not follow a text that failed", chat.calls)
		}
	}
}
