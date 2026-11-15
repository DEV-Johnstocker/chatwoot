package alerts

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeNotifier struct {
	mu   sync.Mutex
	sent []string
	err  error
}

func (f *fakeNotifier) Notify(_ context.Context, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, text)
	return nil
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }
func quiet() *slog.Logger            { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }
func newMonitor(n Notifier, c *clock) *Monitor {
	return New(Options{Notifier: n, Label: "STAGING", Now: c.now, Logger: quiet()})
}

func TestCheck_SendFailuresAlertOnceThenClear(t *testing.T) {
	// Arrange
	n, c := &fakeNotifier{}, &clock{time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)}
	m := newMonitor(n, c)
	for _, acct := range []int64{3, 1, 3} {
		m.Record(SendFailed, acct)
	}

	// Act: three checks in a row, then one after a quiet window
	m.Check(context.Background())
	c.add(time.Minute)
	m.Record(SendFailed, 1)
	m.Check(context.Background())
	firing := m.Active()
	c.add(Window + time.Minute)
	m.Check(context.Background())

	// Assert
	if len(n.sent) != 2 {
		t.Fatalf("sent %d messages, want alert + recovery: %q", len(n.sent), n.sent)
	}
	if want := ":warning: [STAGING] Bot alert: Bot replies are not reaching Chatwoot. 3 in the last 15 min (accounts 1, 3)."; !strings.HasPrefix(n.sent[0], want) {
		t.Errorf("alert = %q, want prefix %q", n.sent[0], want)
	}
	if !strings.HasPrefix(n.sent[1], ":white_check_mark: [STAGING] Cleared: Bot replies are not reaching Chatwoot") {
		t.Errorf("recovery = %q", n.sent[1])
	}
	if len(firing) != 1 || firing[0].Kind != SendFailed {
		t.Errorf("active while firing = %+v", firing)
	}
	if got := m.Active(); len(got) != 0 {
		t.Errorf("active after recovery = %+v", got)
	}
}

func TestCheck_BelowThresholdSendsNothing(t *testing.T) {
	// Arrange
	n, c := &fakeNotifier{}, &clock{time.Unix(0, 0)}
	m := newMonitor(n, c)
	m.Record(LLMError, 1)
	m.Record(LLMError, 1)
	m.Record(SendFailed, 1)

	// Act
	m.Check(context.Background())

	// Assert
	if len(n.sent) != 0 || len(m.Active()) != 0 {
		t.Errorf("sent %q, active %+v", n.sent, m.Active())
	}
}

func TestCheck_OldEventsDoNotCount(t *testing.T) {
	// Arrange
	n, c := &fakeNotifier{}, &clock{time.Unix(0, 0)}
	m := newMonitor(n, c)
	m.Record(LLMError, 1)
	m.Record(LLMError, 1)
	c.add(Window + time.Second)
	m.Record(LLMError, 1)

	// Act
	m.Check(context.Background())

	// Assert
	if len(n.sent) != 0 {
		t.Errorf("sent %q for events spread past the window", n.sent)
	}
}

func TestCheck_OneQueueFullAlerts(t *testing.T) {
	// Arrange
	n, c := &fakeNotifier{}, &clock{time.Unix(0, 0)}
	m := newMonitor(n, c)
	m.Record(QueueFull, 2)

	// Act
	m.Check(context.Background())

	// Assert
	if len(n.sent) != 1 || !strings.Contains(n.sent[0], "overloaded") || !strings.Contains(n.sent[0], "(account 2)") {
		t.Errorf("sent %q", n.sent)
	}
}

func TestCheck_RemindsEverySixHoursWhileItLasts(t *testing.T) {
	// Arrange
	n, c := &fakeNotifier{}, &clock{time.Unix(0, 0)}
	m := newMonitor(n, c)
	m.Record(QueueFull, 1)
	m.Check(context.Background())

	// Act: still overloaded every 10 minutes for 6 hours
	for i := 0; i < 36; i++ {
		c.add(10 * time.Minute)
		m.Record(QueueFull, 1)
		m.Check(context.Background())
	}

	// Assert
	if len(n.sent) != 2 || !strings.Contains(n.sent[1], "Still going on: The bot is overloaded") {
		t.Errorf("sent %q, want alert + one reminder", n.sent)
	}
}

func TestCheck_FailedSendRetriesNextCheck(t *testing.T) {
	// Arrange
	n, c := &fakeNotifier{err: errors.New("slack down")}, &clock{time.Unix(0, 0)}
	m := newMonitor(n, c)
	m.Record(QueueFull, 1)
	m.Check(context.Background())
	n.err = nil

	// Act
	c.add(time.Minute)
	m.Check(context.Background())

	// Assert
	if len(n.sent) != 1 || !strings.Contains(n.sent[0], "Bot alert") {
		t.Errorf("sent %q, want the alert on the retry", n.sent)
	}
}

func TestCheck_HandoffSpike(t *testing.T) {
	cases := []struct {
		name          string
		handed, total int
		want          bool
	}{
		{"half of ten", 5, 10, true},
		{"too few messages", 9, 9, false},
		{"under the share", 4, 10, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			n, c := &fakeNotifier{}, &clock{time.Unix(0, 0)}
			m := newMonitor(n, c)
			for i := 0; i < tc.total; i++ {
				m.Turn(i < tc.handed)
			}

			// Act
			m.Check(context.Background())

			// Assert
			if got := len(n.sent) == 1; got != tc.want {
				t.Errorf("alerted = %v, want %v (%q)", got, tc.want, n.sent)
			}
		})
	}
}

func TestCheck_HandoffSpikeClearsWhenShareFalls(t *testing.T) {
	// Arrange
	n, c := &fakeNotifier{}, &clock{time.Unix(0, 0)}
	m := newMonitor(n, c)
	for i := 0; i < 10; i++ {
		m.Turn(true)
	}
	m.Check(context.Background())

	// Act
	for i := 0; i < 15; i++ {
		m.Turn(false)
	}
	m.Check(context.Background())

	// Assert
	if len(n.sent) != 2 || !strings.Contains(n.sent[0], "10 of the last 10 answered messages (100%)") ||
		!strings.Contains(n.sent[1], "Cleared: Unusually many chats") {
		t.Errorf("sent %q", n.sent)
	}
}

func TestCheck_NoNotifierStillShowsOnAdmin(t *testing.T) {
	// Arrange
	c := &clock{time.Unix(0, 0)}
	m := New(Options{Now: c.now, Logger: quiet()})
	m.Record(ReplyLimit, 1)

	// Act
	m.Check(context.Background())

	// Assert
	if got := m.Active(); len(got) != 1 || got[0].Title != "Chats hit the reply limit" {
		t.Errorf("active = %+v", got)
	}
}

func TestNilMonitorIsSafe(t *testing.T) {
	// Arrange
	var m *Monitor

	// Act
	m.Record(QueueFull, 1)
	m.Turn(true)

	// Assert
	if m.Active() != nil {
		t.Error("nil monitor reported problems")
	}
}

func TestMessages_HoldNoCustomerData(t *testing.T) {
	// Arrange: every kind fires; the monitor never sees message text, but
	// the messages must not grow anything beyond counts and account ids.
	n, c := &fakeNotifier{}, &clock{time.Unix(0, 0)}
	m := newMonitor(n, c)
	for _, k := range []Kind{SendFailed, LLMError, QueueFull, ReplyLimit} {
		for i := 0; i < 3; i++ {
			m.Record(k, 42)
		}
	}
	for i := 0; i < 10; i++ {
		m.Turn(true)
	}

	// Act
	m.Check(context.Background())

	// Assert
	if len(n.sent) != 5 {
		t.Fatalf("sent %d, want one per kind", len(n.sent))
	}
	for _, s := range n.sent {
		if strings.ContainsAny(s, "@\n") || len(s) > 300 {
			t.Errorf("message has more than a short status line: %q", s)
		}
	}
}
