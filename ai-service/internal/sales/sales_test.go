package sales

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
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
	ask := gaps.Message{Incoming: true, Content: "ราคาเท่าไหร่"}
	bot := gaps.Message{Outgoing: true, FromBot: true, Content: "3,900 บาทค่ะ"}
	staff := gaps.Message{Outgoing: true, FromStaff: true, Content: "ได้รับแล้วค่ะ"}
	handoff := gaps.Message{Private: true, FromBot: true, Outgoing: true, Content: "🤖 บอทส่งต่อ\nai-handoff:ai-request"}
	staffNote := gaps.Message{Private: true, Outgoing: true, FromStaff: true, Content: "check slip"}
	cases := map[string]struct {
		msgs []gaps.Message
		want Credit
	}{
		"bot sold, staff confirmed after the slip": {[]gaps.Message{ask, bot, payNote, staff}, CreditBot},
		"staff replied before the slip":            {[]gaps.Message{ask, staff, bot, payNote}, CreditStaff},
		"bot handed off before the slip":           {[]gaps.Message{ask, handoff, bot, payNote}, CreditStaff},
		"staff private note does not count":        {[]gaps.Message{ask, staffNote, bot, payNote}, CreditBot},
		"no slip note, bot only":                   {[]gaps.Message{ask, bot}, CreditBot},
		"no slip note, staff took it":              {[]gaps.Message{ask, bot, handoff, staff}, CreditStaff},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Act
			got := CreditOf(c.msgs)

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
	failPaid  bool
	creditSet chan struct{}
}

func newMem() *memStore {
	return &memStore{sales: map[[2]int64]*Sale{}, creditSet: make(chan struct{}, 8)}
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

func (m *memStore) MarkPaid(_ context.Context, acct, conv, inbox int64, amount *int64, at time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failPaid {
		return false, errors.New("db down")
	}
	k := [2]int64{acct, conv}
	cur, ok := m.sales[k]
	if !ok {
		cur = &Sale{AccountID: acct, ConversationID: conv, InboxID: inbox}
		m.sales[k] = cur
	}
	if cur.Status != StatusPaid {
		cur.PaidAt = at
	}
	cur.Status = StatusPaid
	if amount != nil {
		cur.Amount = amount
	}
	return cur.Credit == "", nil
}

func (m *memStore) SetCredit(_ context.Context, acct, conv int64, c Credit) error {
	m.mu.Lock()
	m.sales[[2]int64{acct, conv}].Credit = c
	m.mu.Unlock()
	m.creditSet <- struct{}{}
	return nil
}

func (m *memStore) Cancel(_ context.Context, acct, conv int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cur, ok := m.sales[[2]int64{acct, conv}]; ok && cur.Status == StatusPaid {
		cur.Status = StatusCancelled
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

const paidEvent = `{"event":"conversation_updated","id":52,"inbox_id":2,"account":{"id":1},
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
	r.Observe(updated(t, `{"event":"conversation_updated","id":52,"account":{"id":1},"labels":["bot-handoff"],
		"changed_attributes":[{"label_list":{"previous_value":["paid"],"current_value":[]}}]}`))

	// Assert
	waitFor(t, func() bool { s, _ := st.get(1, 52); return s.Status == StatusCancelled })
}

func TestRecorder_IgnoresUnrelatedEvents(t *testing.T) {
	cases := map[string]string{
		"assignee change":  `{"event":"conversation_updated","id":52,"account":{"id":1},"labels":["paid"],"changed_attributes":[{"assignee_id":{"previous_value":1,"current_value":2}}]}`,
		"message event":    `{"event":"message_created","id":9,"content":"paid","account":{"id":1},"conversation":{"id":52}}`,
		"status change":    `{"event":"conversation_status_changed","id":52,"status":"open","account":{"id":1},"labels":["paid"]}`,
		"no account":       `{"event":"conversation_updated","id":52,"labels":["paid"]}`,
		"malformed labels": `{"event":"conversation_updated","id":53,"account":{"id":1},"labels":[{"title":"paid"}],"changed_attributes":[{"label_list":{}}]}`,
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
	r.Observe(updated(t, `{"event":"conversation_updated","id":52,"account":{"id":1},"labels":["paid"],
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
