package spend

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// memStore is an in-memory Store.
type memStore struct {
	mu     sync.Mutex
	rows   []Row
	caps   []Cap
	alerts map[Alert]bool
	pruned int
}

func key(a Alert) Alert { a.Spent = 0; return a }

func (s *memStore) Rows(context.Context, time.Time, time.Time, string, int64) ([]Row, error) {
	return s.rows, nil
}
func (s *memStore) Caps(context.Context) ([]Cap, error)    { return s.caps, nil }
func (s *memStore) SaveCap(_ context.Context, c Cap) error { s.caps = append(s.caps, c); return nil }
func (s *memStore) MarkAlert(_ context.Context, a Alert) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.alerts == nil {
		s.alerts = map[Alert]bool{}
	}
	if s.alerts[key(a)] {
		return false, nil
	}
	s.alerts[key(a)] = true
	return true, nil
}
func (s *memStore) UnmarkAlert(_ context.Context, a Alert) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.alerts, key(a))
	return nil
}
func (s *memStore) Prune(context.Context, time.Time) (int64, error) { s.pruned++; return 0, nil }

type memNotify struct {
	texts []string
	err   error
}

func (n *memNotify) Notify(_ context.Context, text string) error {
	if n.err != nil {
		return n.err
	}
	n.texts = append(n.texts, text)
	return nil
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func monitorAt(store *memStore, n Notifier) *Monitor {
	return &Monitor{Store: store, Prices: testPrices, Loc: time.UTC, Notify: n, Label: "pronto.example", Logger: quiet(),
		Now: func() time.Time { return time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC) }}
}

func TestCheck_SendsEachLevelOncePerPeriod(t *testing.T) {
	// Arrange: $0.85 of a $1 daily cap
	store := &memStore{caps: []Cap{{AccountID: 0, Daily: 1_000_000}},
		rows: []Row{{Day: day("2026-11-16"), AccountID: 1, Model: "chat", Usage: Usage{InputTokens: 850_000}}}}
	n := &memNotify{}
	m := monitorAt(store, n)

	// Act: two checks at 85%, then spend passes 100%
	err1 := m.Check(context.Background())
	err2 := m.Check(context.Background())
	store.rows[0].Usage.InputTokens = 1_200_000
	err3 := m.Check(context.Background())
	err4 := m.Check(context.Background())

	// Assert
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		t.Fatal(err1, err2, err3, err4)
	}
	if len(n.texts) != 2 || !strings.Contains(n.texts[0], "85% of the daily cap") || !strings.Contains(n.texts[1], "🚨") ||
		!strings.Contains(n.texts[1], "120%") || !strings.Contains(n.texts[0], "whole service") || !strings.Contains(n.texts[0], "pronto.example") {
		t.Fatalf("texts = %q", n.texts)
	}
	if w := m.Warnings(); len(w) != 1 || w[0].Level != 100 {
		t.Fatalf("warnings = %+v", w)
	}
}

func TestCheck_JumpPastBothLevelsSendsOneMessage(t *testing.T) {
	// Arrange: from nothing straight to 150%
	store := &memStore{caps: []Cap{{AccountID: 1, Monthly: 1_000_000}},
		rows: []Row{{Day: day("2026-11-03"), AccountID: 1, Model: "chat", Usage: Usage{InputTokens: 1_500_000}}}}
	n := &memNotify{}

	// Act
	err := monitorAt(store, n).Check(context.Background())

	// Assert
	if err != nil || len(n.texts) != 1 || !strings.Contains(n.texts[0], "monthly") || !strings.Contains(n.texts[0], "account 1") || len(store.alerts) != 2 {
		t.Fatalf("err=%v texts=%q alerts=%d", err, n.texts, len(store.alerts))
	}
}

func TestCheck_FailedSlackPostIsRetried(t *testing.T) {
	// Arrange
	store := &memStore{caps: []Cap{{Daily: 1_000_000}},
		rows: []Row{{Day: day("2026-11-16"), Model: "chat", Usage: Usage{InputTokens: 900_000}}}}
	n := &memNotify{err: errors.New("slack down")}
	m := monitorAt(store, n)

	// Act
	first := m.Check(context.Background())
	n.err = nil
	second := m.Check(context.Background())

	// Assert
	if first == nil || second != nil || len(n.texts) != 1 {
		t.Fatalf("first=%v second=%v texts=%q", first, second, n.texts)
	}
}

func TestCheck_NoWebhookStillWarnsOnPage(t *testing.T) {
	// Arrange
	store := &memStore{caps: []Cap{{Daily: 1_000_000}},
		rows: []Row{{Day: day("2026-11-16"), Model: "chat", Usage: Usage{InputTokens: 2_000_000}}}}
	m := monitorAt(store, nil)

	// Act
	err := m.Check(context.Background())

	// Assert
	if err != nil || len(m.Warnings()) != 1 {
		t.Fatalf("err=%v warnings=%+v", err, m.Warnings())
	}
}

func TestCheck_UnderCapAndNoCaps(t *testing.T) {
	// Arrange
	under := &memStore{caps: []Cap{{Daily: 1_000_000}}, rows: []Row{{Day: day("2026-11-16"), Model: "chat", Usage: Usage{InputTokens: 100}}}}
	none := &memStore{}
	n := &memNotify{}

	// Act
	m := monitorAt(under, n)
	_, before := m.Statuses()
	err1 := m.Check(context.Background())
	err2 := monitorAt(none, n).Check(context.Background())

	// Assert
	statuses, after := m.Statuses()
	if err1 != nil || err2 != nil || len(n.texts) != 0 || len(m.Warnings()) != 0 || before || !after || len(statuses) != 1 {
		t.Fatalf("errs=%v %v texts=%q statuses=%+v", err1, err2, n.texts, statuses)
	}
}

func TestNewSlack_OnlyHooksSlackCom(t *testing.T) {
	bad := []string{"", "http://hooks.slack.com/services/x", "https://evil.example/services/x",
		"https://hooks.slack.com.evil.example/x", "https://user:pw@hooks.slack.com/services/x", "https://hooks.slack.com", "https://hooks.slack.com/"}
	for _, raw := range bad {
		// Act
		_, err := NewSlack(raw)

		// Assert
		if !errors.Is(err, ErrBadWebhook) {
			t.Errorf("NewSlack(%q) err = %v", raw, err)
		}
	}
	if _, err := NewSlack(" https://hooks.slack.com/services/T0/B0/abc "); err != nil {
		t.Errorf("valid webhook refused: %v", err)
	}
}

func TestSlack_PostsTextAndHidesURLOnError(t *testing.T) {
	// Arrange
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		if strings.Contains(got, "fail") {
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()
	s := &Slack{URL: srv.URL + "/services/SECRETPART", Client: srv.Client()}
	dead := &Slack{URL: "http://127.0.0.1:1/services/SECRETPART", Client: &http.Client{Timeout: time.Second}}

	// Act
	okErr := s.Notify(context.Background(), "hello")
	statusErr := s.Notify(context.Background(), "fail")
	deadErr := dead.Notify(context.Background(), "x")

	// Assert
	if okErr != nil || got != `{"text":"fail"}` {
		t.Fatalf("ok=%v body=%s", okErr, got)
	}
	for _, err := range []error{statusErr, deadErr} {
		if err == nil || strings.Contains(err.Error(), "SECRETPART") {
			t.Errorf("err = %v", err)
		}
	}
}

func TestMessage_HasNoCustomerData(t *testing.T) {
	// Arrange
	s := Status{AccountID: 3, Period: PeriodMonth, PeriodStart: day("2026-11-01"), Cap: 50_000_000, Spent: 40_500_000, Percent: 81, Level: 80}

	// Act
	text := Message(s, "pronto.example", "https://pronto.example/admin/spend", time.UTC)

	// Assert
	want := "⚠️ AI spend is at 81% of the monthly cap on pronto.example (account 3): $40.50 of $50.00, November 2026, UTC time.\n" +
		"The bot keeps answering; caps only alert. <https://pronto.example/admin/spend|Open AI spend>"
	if text != want {
		t.Fatalf("text = %q", text)
	}
}
