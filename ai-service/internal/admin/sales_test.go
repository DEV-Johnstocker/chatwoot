package admin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/sales"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/settings"
)

type fakeSalesStore struct {
	totals      sales.Totals
	paid, slips []sales.Sale
	err         error
	acct        int64
	from, to    time.Time
}

func (f *fakeSalesStore) RecordSlip(context.Context, sales.Sale) error { return nil }
func (f *fakeSalesStore) MarkPaid(context.Context, sales.Paid) (bool, bool, error) {
	return false, false, nil
}
func (f *fakeSalesStore) SetCredit(context.Context, int64, int64, sales.Credit) error { return nil }
func (f *fakeSalesStore) Cancel(context.Context, int64, int64, time.Time) error       { return nil }
func (f *fakeSalesStore) Totals(_ context.Context, acct int64, from, to time.Time) (sales.Totals, error) {
	f.acct, f.from, f.to = acct, from, to
	return f.totals, f.err
}
func (f *fakeSalesStore) Paid(context.Context, int64, time.Time, time.Time, int) ([]sales.Sale, error) {
	return f.paid, f.err
}
func (f *fakeSalesStore) Slips(context.Context, int64, int) ([]sales.Sale, error) {
	return f.slips, f.err
}

func salesEnv(t *testing.T, st sales.Store) *http.ServeMux {
	t.Helper()
	store, err := settings.Open(t.TempDir()+"/s.json", settings.LLM{})
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Asia/Bangkok")
	h := &Handler{Password: pw, Store: store, Switch: &llm.Switch{}, Timeout: time.Second, Location: loc,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), ChatwootURL: "https://pronto.example", Sales: st}
	mux := http.NewServeMux()
	h.Routes(mux)
	return mux
}

func salesGet(q string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/admin/sales?"+q, nil)
	r.SetBasicAuth(Username, pw)
	return r
}

func i64(n int64) *int64 { return &n }

func sampleSales() *fakeSalesStore {
	at := time.Date(2026, 11, 15, 3, 0, 0, 0, time.UTC)
	return &fakeSalesStore{
		totals: sales.Totals{Paid: 3, Bot: 2, Staff: 1, Revenue: 1170050, BotRevenue: 780050, StaffRevenue: 390000},
		paid: []sales.Sale{
			{AccountID: 1, ConversationID: 52, InboxID: 2, Status: sales.StatusPaid, Amount: i64(390050), Credit: sales.CreditBot, PaidAt: at},
			{AccountID: 1, ConversationID: 50, Status: sales.StatusPaid, Credit: sales.CreditStaff, PaidAt: at},
		},
		slips: []sales.Sale{{AccountID: 1, ConversationID: 61, Status: sales.StatusSlip, SlipAmount: i64(130000), SlipAt: at}},
	}
}

func TestSales_ShowsTotalsShareAndLists(t *testing.T) {
	// Arrange
	mux := salesEnv(t, sampleSales())

	// Act
	res := serve(mux, salesGet("account=1"))
	body := res.Body.String()

	// Assert
	if res.Code != http.StatusOK {
		t.Fatalf("status %d", res.Code)
	}
	for _, want := range []string{"฿11,700.50", "3 paid chats", "฿7,800.50", "67% of sales", `style="width: 67%"`, "฿3,900",
		"฿3,900.50", "🤖 Bot", "🙋 Staff", "no amount", "฿1,300?", "Open chat #61", "https://pronto.example/app/accounts/1/conversations/52",
		`aria-current="true">This month`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestSales_PeriodsInTheBusinessTimeZone(t *testing.T) {
	cases := map[string]time.Duration{"": 0, "last-month": 0, "30d": 30 * 24 * time.Hour, "junk": 0}
	for period := range cases {
		t.Run(period, func(t *testing.T) {
			// Arrange
			st := sampleSales()
			mux := salesEnv(t, st)

			// Act
			serve(mux, salesGet("account=1&period="+period))

			// Assert
			loc, _ := time.LoadLocation("Asia/Bangkok")
			if st.acct != 1 || !st.from.Before(st.to) {
				t.Fatalf("acct=%d from=%v to=%v", st.acct, st.from, st.to)
			}
			if period != "30d" {
				f := st.from.In(loc)
				if f.Day() != 1 || f.Hour() != 0 || f.Minute() != 0 {
					t.Fatalf("month does not start at Bangkok midnight on the 1st: %v", f)
				}
			}
		})
	}
}

func TestSales_OffWithoutDatabase(t *testing.T) {
	// Arrange
	mux := salesEnv(t, nil)

	// Act
	body := serve(mux, salesGet("account=1")).Body.String()

	// Assert
	if !strings.Contains(body, "Sales are off.") {
		t.Fatal("no off state")
	}
}

func TestSales_DatabaseErrorIsShown(t *testing.T) {
	// Arrange
	mux := salesEnv(t, &fakeSalesStore{err: errors.New("db down")})

	// Act
	body := serve(mux, salesGet("account=1")).Body.String()

	// Assert
	if !strings.Contains(body, "Could not load the sales") || strings.Contains(body, "db down") {
		t.Fatal("error not shown, or internal error leaked")
	}
}

func TestSales_NeedsLogin(t *testing.T) {
	// Arrange
	mux := salesEnv(t, sampleSales())
	r := httptest.NewRequest(http.MethodGet, "/admin/sales?account=1", nil)

	// Act
	res := serve(mux, r)

	// Assert
	if res.Code != http.StatusUnauthorized || strings.Contains(res.Body.String(), "฿") {
		t.Fatalf("status %d", res.Code)
	}
}

func TestBaht(t *testing.T) {
	cases := map[int64]string{0: "฿0", 5: "฿0.05", 390000: "฿3,900", 390050: "฿3,900.50", 123456789012: "฿1,234,567,890.12", -100: "-฿1"}
	for in, want := range cases {
		if got := baht(in); got != want {
			t.Errorf("baht(%d) = %q, want %q", in, got, want)
		}
	}
}
