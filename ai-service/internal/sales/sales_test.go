package sales

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/webhook"
)

func TestParseBaht(t *testing.T) {
	cases := []struct {
		raw  string
		want int64
		ok   bool
	}{
		{`3900`, 390000, true},
		{`3900.5`, 390050, true},
		{`"3,900"`, 390000, true},
		{`"3,900.50 บาท"`, 390050, true},
		{`"฿1,300"`, 130000, true},
		{`" 1 300 THB "`, 130000, true},
		{`0`, 0, true},
		{`-5`, 0, false},
		{`"abc"`, 0, false},
		{`""`, 0, false},
		{`true`, 0, false},
		{`1e12`, 0, false},
		{`"1e5"`, 0, false},
		{`"0x1p4"`, 0, false},
		{`"3.900"`, 0, false},
		{`999999999.996`, 0, false},
		{`999999999.99`, 99999999999, true},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			// Act
			got, err := ParseBaht(json.RawMessage(c.raw))

			// Assert
			if (err == nil) != c.ok || got != c.want {
				t.Fatalf("ParseBaht(%s) = %d, %v; want %d ok=%v", c.raw, got, err, c.want, c.ok)
			}
		})
	}
}

func TestSlipAmount(t *testing.T) {
	cases := map[string]*int64{
		"- **จากไฟล์:** สลิปโอน 3,900 บาท วันที่ 15 พ.ย. 14:02 ผู้รับ บจก. ตัวอย่าง": ptr(390000),
		"- **จากไฟล์:** สลิป ยอด ฿1,300.50 เข้า บัญชีร้าน":                           ptr(130050),
		"- **จากไฟล์:** รูปหน้าจอโปรแกรม error":                                      nil,
		"- **ลูกค้าต้องการ:** ราคา 3,900 บาท":                                        nil, // not the file line
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			// Act
			got := SlipAmount(in)

			// Assert
			if (got == nil) != (want == nil) || (got != nil && *got != *want) {
				t.Fatalf("SlipAmount = %v, want %v", deref(got), deref(want))
			}
		})
	}
}

func ptr(n int64) *int64 { return &n }

func deref(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestCreditOf(t *testing.T) {
	payNote := gaps.Message{Private: true, FromBot: true, Outgoing: true, Content: "💳 **" + PaymentNoteTitle + "** · สรุปจากบอท\n\nai-handoff:attachment"}
	fakePay := gaps.Message{Private: true, FromBot: true, Outgoing: true, Content: "🤖 **บอทส่งต่อ**\n- ลูกค้าพิมพ์ว่า " + PaymentNoteTitle + "\nai-handoff:ai-request"}
	ask := gaps.Message{Incoming: true, Content: "ราคาเท่าไหร่"}
	bot := gaps.Message{Outgoing: true, FromBot: true, Content: "3,900 บาทค่ะ"}
	staff := gaps.Message{Outgoing: true, FromStaff: true, Content: "ได้รับแล้วค่ะ"}
	echo := gaps.Message{Outgoing: true, Echo: true, Content: "ตอบจากแอปเพจ"}
	handoff := gaps.Message{Private: true, FromBot: true, Outgoing: true, Content: "🤖 บอทส่งต่อ\nai-handoff:ai-request"}
	staffNote := gaps.Message{Private: true, Outgoing: true, FromStaff: true, Content: "check slip"}
	cases := map[string]struct {
		msgs     []gaps.Message
		complete bool
		want     Credit
	}{
		"bot sold, staff confirmed after the slip":     {[]gaps.Message{ask, bot, payNote, staff}, true, CreditBot},
		"staff replied before the slip":                {[]gaps.Message{ask, staff, bot, payNote}, true, CreditStaff},
		"bot handed off before the slip":               {[]gaps.Message{ask, handoff, bot, payNote}, true, CreditStaff},
		"staff private note does not count":            {[]gaps.Message{ask, staffNote, bot, payNote}, true, CreditBot},
		"no slip note, bot only":                       {[]gaps.Message{ask, bot}, true, CreditBot},
		"no slip note, staff took it":                  {[]gaps.Message{ask, bot, handoff, staff}, true, CreditStaff},
		"page app reply after the bot counts as staff": {[]gaps.Message{ask, bot, echo, payNote}, true, CreditStaff},
		"page welcome before the bot does not count":   {[]gaps.Message{echo, ask, bot, payNote}, true, CreditBot},
		"customer words in a summary do not move it":   {[]gaps.Message{ask, bot, fakePay, payNote}, true, CreditStaff},
		"chat cut short: unknown":                      {[]gaps.Message{ask, bot, payNote}, false, ""},
		"chat cut short but staff seen: staff":         {[]gaps.Message{ask, staff, payNote}, false, CreditStaff},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Act
			got := CreditOf(c.msgs, c.complete)

			// Assert
			if got != c.want {
				t.Fatalf("CreditOf = %q, want %q", got, c.want)
			}
		})
	}
}

// memStore is a Store in memory for the recorder tests.
type memStore struct {
	mu        sync.Mutex
	sales     map[[2]int64]*Sale
	source    map[[2]int64]time.Time
	failPaid  bool
	creditSet chan struct{}
}

func newMem() *memStore {
	return &memStore{sales: map[[2]int64]*Sale{}, source: map[[2]int64]time.Time{}, creditSet: make(chan struct{}, 8)}
}

func (m *memStore) RecordSlip(_ context.Context, s Sale) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.sales[[2]int64{s.AccountID, s.ConversationID}]
	if !ok {
		s.Status = StatusSlip
		m.sales[[2]int64{s.AccountID, s.ConversationID}] = &s
		return nil
	}
	if cur.Status != StatusPaid {
		cur.Status = StatusSlip
	}
	return nil
}

func (m *memStore) MarkPaid(_ context.Context, p Paid) (bool, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failPaid {
		return false, false, errors.New("db down")
	}
	k := [2]int64{p.AccountID, p.ConversationID}
	cur, ok := m.sales[k]
	if !ok {
		cur = &Sale{AccountID: p.AccountID, ConversationID: p.ConversationID, InboxID: p.InboxID}
		m.sales[k] = cur
	}
	if !p.Source.IsZero() && !m.source[k].IsZero() && p.Source.Before(m.source[k]) {
		return false, false, nil
	}
	if !p.Source.IsZero() {
		m.source[k] = p.Source
	}
	if cur.Status != StatusPaid {
		cur.PaidAt = p.At
	}
	cur.Status = StatusPaid
	if p.Amount != nil && (p.ByStaff || cur.Amount == nil) {
		cur.Amount = p.Amount
	}
	return true, cur.Credit == "", nil
}

func (m *memStore) SetCredit(_ context.Context, acct, conv int64, c Credit) error {
	m.mu.Lock()
	m.sales[[2]int64{acct, conv}].Credit = c
	m.mu.Unlock()
	m.creditSet <- struct{}{}
	return nil
}

func (m *memStore) Cancel(_ context.Context, acct, conv int64, source time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := [2]int64{acct, conv}
	if !source.IsZero() && !m.source[k].IsZero() && source.Before(m.source[k]) {
		return nil
	}
	if cur, ok := m.sales[k]; ok && cur.Status == StatusPaid {
		cur.Status = StatusCancelled
		if !source.IsZero() {
			m.source[k] = source
		}
	}
	return nil
}

func (m *memStore) Totals(context.Context, int64, time.Time, time.Time) (Totals, error) {
	return Totals{}, nil
}
func (m *memStore) Paid(context.Context, int64, time.Time, time.Time, int) ([]Sale, error) {
	return nil, nil
}
func (m *memStore) Slips(context.Context, int64, int) ([]Sale, error) { return nil, nil }

func (m *memStore) get(acct, conv int64) (Sale, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sales[[2]int64{acct, conv}]
	if !ok {
		return Sale{}, false
	}
	return *s, true
}

type fakeMsgs struct{ msgs []gaps.Message }

func (f fakeMsgs) AllMessages(context.Context, int64, int64, int) ([]gaps.Message, error) {
	return f.msgs, nil
}

func updated(t *testing.T, body string) webhook.Event {
	t.Helper()
	ev, err := webhook.Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func startRecorder(t *testing.T, st Store, msgs Messages) *Recorder {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := &Recorder{Store: st, Messages: msgs, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now: func() time.Time { return time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC) }}
	r.Start(ctx)
	return r
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

const paidEvent = `{"event":"conversation_updated","id":52,"inbox_id":2,"account":{"id":1},"channel":"Channel::FacebookPage","updated_at":1794700000.1,
	"labels":["bot-handoff","Paid"],"custom_attributes":{"paid_amount":"3,900"},
	"changed_attributes":[{"label_list":{"previous_value":[],"current_value":["paid"]}}]}`

func TestRecorder_PaidLabelRecordsAmountAndCredit(t *testing.T) {
	// Arrange
	st := newMem()
	r := startRecorder(t, st, fakeMsgs{msgs: []gaps.Message{{Incoming: true, Content: "ราคา"}, {Outgoing: true, FromBot: true, Content: "3,900"}}})

	// Act
	r.Observe(updated(t, paidEvent))
	<-st.creditSet

	// Assert
	s, ok := st.get(1, 52)
	if !ok || s.Status != StatusPaid || s.Amount == nil || *s.Amount != 390000 || s.InboxID != 2 || s.Credit != CreditBot {
		t.Fatalf("sale = %+v", s)
	}
}

func TestRecorder_RemovingPaidCancels(t *testing.T) {
	// Arrange
	st := newMem()
	r := startRecorder(t, st, nil)
	r.Observe(updated(t, paidEvent))
	waitFor(t, func() bool { s, ok := st.get(1, 52); return ok && s.Status == StatusPaid })

	// Act
	r.Observe(updated(t, `{"event":"conversation_updated","id":52,"account":{"id":1},"labels":["bot-handoff"],"channel":"Channel::FacebookPage","updated_at":1794700001,
		"changed_attributes":[{"label_list":{"previous_value":["paid"],"current_value":[]}}]}`))

	// Assert
	waitFor(t, func() bool { s, _ := st.get(1, 52); return s.Status == StatusCancelled })
}

func TestRecorder_IgnoresUnrelatedEvents(t *testing.T) {
	cases := map[string]string{
		"assignee change":  `{"event":"conversation_updated","id":52,"account":{"id":1},"channel":"Channel::FacebookPage","labels":["paid"],"changed_attributes":[{"assignee_id":{"previous_value":1,"current_value":2}}]}`,
		"website widget":   `{"event":"conversation_updated","id":52,"account":{"id":1},"channel":"Channel::WebWidget","labels":["paid"],"custom_attributes":{"paid_amount":999999},"changed_attributes":[{"label_list":{}}]}`,
		"api channel":      `{"event":"conversation_updated","id":52,"account":{"id":1},"channel":"Channel::Api","labels":["paid"],"changed_attributes":[{"label_list":{}}]}`,
		"no channel":       `{"event":"conversation_updated","id":52,"account":{"id":1},"labels":["paid"],"changed_attributes":[{"label_list":{}}]}`,
		"customer #paid":   `{"event":"message_created","id":9,"content":"#paid 1","message_type":"incoming","account":{"id":1},"conversation":{"id":52},"sender":{"type":"contact"}}`,
		"public #paid":     `{"event":"message_created","id":9,"content":"#paid 1","message_type":"outgoing","account":{"id":1},"conversation":{"id":52},"sender":{"type":"user"}}`,
		"bot note #paid":   `{"event":"message_created","id":9,"content":"#paid 1","message_type":"outgoing","private":true,"account":{"id":1},"conversation":{"id":52},"sender":{"type":"agent_bot"}}`,
		"message event":    `{"event":"message_created","id":9,"content":"paid","account":{"id":1},"conversation":{"id":52}}`,
		"status change":    `{"event":"conversation_status_changed","id":52,"status":"open","account":{"id":1},"labels":["paid"]}`,
		"no account":       `{"event":"conversation_updated","id":52,"labels":["paid"]}`,
		"malformed labels": `{"event":"conversation_updated","id":53,"account":{"id":1},"channel":"Channel::FacebookPage","labels":[{"title":"paid"}],"changed_attributes":[{"label_list":{}}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			st := newMem()
			r := startRecorder(t, st, nil)

			// Act
			r.Observe(updated(t, body))
			time.Sleep(50 * time.Millisecond)

			// Assert
			if s, ok := st.get(1, 52); ok && s.Status == StatusPaid {
				t.Fatalf("recorded a sale: %+v", s)
			}
			if s, ok := st.get(1, 53); ok && s.Status == StatusPaid {
				t.Fatalf("recorded a sale from malformed labels: %+v", s)
			}
		})
	}
}

func TestRecorder_BadAmountStillRecordsThePaidChat(t *testing.T) {
	// Arrange
	st := newMem()
	r := startRecorder(t, st, nil)

	// Act
	r.Observe(updated(t, `{"event":"conversation_updated","id":52,"account":{"id":1},"labels":["paid"],"channel":"Channel::Line",
		"custom_attributes":{"paid_amount":"สามพัน"},"changed_attributes":[{"custom_attributes":{}}]}`))

	// Assert
	waitFor(t, func() bool { s, ok := st.get(1, 52); return ok && s.Status == StatusPaid })
	if s, _ := st.get(1, 52); s.Amount != nil {
		t.Fatalf("amount = %d", *s.Amount)
	}
}

func TestRecorder_ObserveBeforeStartAndNilAreSafe(t *testing.T) {
	// Arrange
	var nilRec *Recorder
	notStarted := &Recorder{Store: newMem(), Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}

	// Act and Assert: no panic, no block
	nilRec.Observe(updated(t, paidEvent))
	notStarted.Observe(updated(t, paidEvent))
}

type fakeNotes struct {
	mu  sync.Mutex
	got []string
}

func (f *fakeNotes) SendPrivateNote(_ context.Context, _, _ int64, c string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, c)
	return nil
}

func (f *fakeNotes) list() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}

func staffNote(id int64, content string) string {
	b, _ := json.Marshal(content)
	return fmt.Sprintf(`{"event":"message_created","id":%d,"content":%s,"message_type":"outgoing","private":true,
		"account":{"id":1},"inbox":{"id":3},"conversation":{"id":52},"sender":{"type":"user","name":"Nok"}}`, id, b)
}

func TestRecorder_StaffPaidNoteWorksOnEveryChannelAndConfirms(t *testing.T) {
	// Arrange
	st, notes := newMem(), &fakeNotes{}
	r := startRecorder(t, st, nil)
	r.Notes = notes

	// Act
	r.Observe(updated(t, staffNote(900, "#paid 3,900 บาท\nโอนเข้า SCB")))
	waitFor(t, func() bool { return len(notes.list()) == 1 })

	// Assert
	s, _ := st.get(1, 52)
	if s.Status != StatusPaid || s.Amount == nil || *s.Amount != 390000 || s.InboxID != 3 {
		t.Fatalf("sale = %+v", s)
	}
	if got := notes.list()[0]; !strings.Contains(got, "฿3,900") || !strings.HasPrefix(got, "✅") {
		t.Fatalf("note = %q", got)
	}
}

func TestRecorder_StaffNoteFixesAnAmountTheAttributeCannot(t *testing.T) {
	// Arrange: paid with 3,900; later the attribute says 999,999 (e.g. set by someone else).
	st, notes := newMem(), &fakeNotes{}
	r := startRecorder(t, st, nil)
	r.Notes = notes
	r.Observe(updated(t, paidEvent))
	waitFor(t, func() bool { s, ok := st.get(1, 52); return ok && s.Amount != nil })

	// Act
	r.Observe(updated(t, `{"event":"conversation_updated","id":52,"account":{"id":1},"channel":"Channel::FacebookPage","updated_at":1794700009,
		"labels":["paid"],"custom_attributes":{"paid_amount":999999},"changed_attributes":[{"custom_attributes":{}}]}`))
	time.Sleep(50 * time.Millisecond)
	afterAttr, _ := st.get(1, 52)
	r.Observe(updated(t, staffNote(901, "#paid 4,200")))
	waitFor(t, func() bool { return len(notes.list()) == 1 })
	afterNote, _ := st.get(1, 52)

	// Assert
	if *afterAttr.Amount != 390000 {
		t.Fatalf("the attribute overwrote a stored amount: %d", *afterAttr.Amount)
	}
	if *afterNote.Amount != 420000 {
		t.Fatalf("a staff note should replace it: %d", *afterNote.Amount)
	}
}

func TestRecorder_UnpaidNoteAndBadAmount(t *testing.T) {
	// Arrange
	st, notes := newMem(), &fakeNotes{}
	r := startRecorder(t, st, nil)
	r.Notes = notes
	r.Observe(updated(t, staffNote(900, "#paid")))
	waitFor(t, func() bool { return len(notes.list()) == 1 })

	// Act
	r.Observe(updated(t, staffNote(901, "#paid สามพัน")))
	r.Observe(updated(t, staffNote(902, "#ยกเลิกชำระ")))
	r.Observe(updated(t, staffNote(902, "#ยกเลิกชำระ"))) // redelivered
	waitFor(t, func() bool { return len(notes.list()) >= 3 })
	time.Sleep(50 * time.Millisecond)

	// Assert
	got := notes.list()
	if len(got) != 3 || !strings.HasPrefix(got[1], "⚠️") || !strings.HasPrefix(got[2], "↩️") {
		t.Fatalf("notes = %q", got)
	}
	if s, _ := st.get(1, 52); s.Status != StatusCancelled {
		t.Fatalf("sale = %+v", s)
	}
}

func TestRecorder_OlderEventDoesNotUndoANewerOne(t *testing.T) {
	// Arrange: staff removed "paid" (t=2), then the "paid" event (t=1) arrives late.
	st := newMem()
	r := startRecorder(t, st, nil)
	r.Observe(updated(t, `{"event":"conversation_updated","id":52,"account":{"id":1},"channel":"Channel::FacebookPage","updated_at":1794700000,
		"labels":["paid"],"changed_attributes":[{"label_list":{}}]}`))
	waitFor(t, func() bool { s, ok := st.get(1, 52); return ok && s.Status == StatusPaid })
	r.Observe(updated(t, `{"event":"conversation_updated","id":52,"account":{"id":1},"channel":"Channel::FacebookPage","updated_at":1794700002,
		"labels":[],"changed_attributes":[{"label_list":{}}]}`))
	waitFor(t, func() bool { s, _ := st.get(1, 52); return s.Status == StatusCancelled })

	// Act
	r.Observe(updated(t, `{"event":"conversation_updated","id":52,"account":{"id":1},"channel":"Channel::FacebookPage","updated_at":1794700001,
		"labels":["paid"],"changed_attributes":[{"label_list":{}}]}`))
	time.Sleep(50 * time.Millisecond)

	// Assert
	if s, _ := st.get(1, 52); s.Status != StatusCancelled {
		t.Fatalf("a late event brought the sale back: %+v", s)
	}
}

func TestRecorder_MissingLabelsNeverCancel(t *testing.T) {
	// Arrange
	st := newMem()
	r := startRecorder(t, st, nil)
	r.Observe(updated(t, paidEvent))
	waitFor(t, func() bool { s, ok := st.get(1, 52); return ok && s.Status == StatusPaid })

	// Act
	r.Observe(updated(t, `{"event":"conversation_updated","id":52,"account":{"id":1},"channel":"Channel::FacebookPage","updated_at":1794700005,
		"changed_attributes":[{"label_list":{}}]}`))
	time.Sleep(50 * time.Millisecond)

	// Assert
	if s, _ := st.get(1, 52); s.Status != StatusPaid {
		t.Fatalf("sale = %+v", s)
	}
}

func TestParseNote(t *testing.T) {
	cases := map[string]struct {
		paid bool
		rest string
		ok   bool
	}{
		"#paid 3,900":          {true, "3,900", true},
		"#PAID: 3900 บาท\nSCB": {true, "3900 บาท", true},
		`\#paid`:               {true, "", true},
		"#ชำระแล้ว 1,300":      {true, "1,300", true},
		"#unpaid":              {false, "", true},
		"#ยกเลิกชำระ":          {false, "", true},
		"#paidx":               {false, "", false},
		"ลูกค้า #paid แล้ว":    {false, "", false},
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			// Act
			paid, rest, ok := parseNote(in)

			// Assert
			if paid != want.paid || rest != want.rest || ok != want.ok {
				t.Fatalf("parseNote(%q) = %v %q %v", in, paid, rest, ok)
			}
		})
	}
}
