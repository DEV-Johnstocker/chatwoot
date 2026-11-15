package admin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/settings"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/spend"
)

type fakeSpend struct {
	rep      spend.Report
	repErr   error
	scopes   []int64
	caps     []spend.Cap
	saved    []spend.Cap
	saveErr  error
	warnings []spend.Status
	statuses []spend.Status
	slack    bool
}

func (f *fakeSpend) Report(_ context.Context, acct int64) (spend.Report, error) {
	f.scopes = append(f.scopes, acct)
	return f.rep, f.repErr
}
func (f *fakeSpend) Caps(context.Context) ([]spend.Cap, error) { return f.caps, nil }
func (f *fakeSpend) SaveCap(_ context.Context, c spend.Cap) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, c)
	return nil
}
func (f *fakeSpend) Warnings() []spend.Status { return f.warnings }
func (f *fakeSpend) Statuses() []spend.Status { return f.statuses }
func (f *fakeSpend) Alerting() bool           { return f.slack }

func spendEnv(t *testing.T, fs *fakeSpend) *http.ServeMux {
	t.Helper()
	store, err := settings.Open(t.TempDir()+"/s.json", settings.LLM{})
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{Password: pw, Store: store, Switch: &llm.Switch{}, Timeout: time.Second,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	if fs != nil {
		h.Spend = fs
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	return mux
}

func spendPost(v url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/admin/spend", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.SetBasicAuth(Username, pw)
	return r
}

func sampleSpend() *fakeSpend {
	day := func(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC) }
	return &fakeSpend{
		rep: spend.Report{Today: 1_250_000, Month: 8_500_000, TodayCalls: 42, MonthCalls: 900,
			ByPurpose: []spend.Line{{Name: spend.PurposeReply, Calls: 800, Cost: 8_000_000, Priced: true}},
			ByModel:   []spend.Line{{Name: "gpt-6-luna", Calls: 800, Usage: spend.Usage{InputTokens: 1_234_567, OutputTokens: 89}, Cost: 8_000_000, Priced: true}},
			Days:      []spend.DayCost{{Day: day(15), Cost: 500_000}, {Day: day(16), Cost: 1_250_000}},
			Unpriced:  []string{"mystery-<model>"}},
		caps:     []spend.Cap{{AccountID: 0, Daily: 5_000_000, UpdatedBy: "owner@example.com", UpdatedAt: day(16)}},
		statuses: []spend.Status{{AccountID: 0, Period: spend.PeriodDay, Cap: 5_000_000, Spent: 1_250_000, Percent: 25}},
	}
}

func TestSpend_PageShowsTotalsBreakdownCapsAndTrend(t *testing.T) {
	// Arrange
	fs := sampleSpend()
	mux := spendEnv(t, fs)

	// Act
	rec := serve(mux, gapGet("/admin/spend"))

	// Assert
	body := rec.Body.String()
	if rec.Code != http.StatusOK || len(fs.scopes) != 1 || fs.scopes[0] != -1 {
		t.Fatalf("code=%d scopes=%v", rec.Code, fs.scopes)
	}
	for _, want := range []string{"$1.25", "$8.50", "42 model calls", "Replies to customers", "gpt-6-luna", "1,234,567",
		"Whole service", "$5.00", "25%", "mystery-&lt;model&gt;", "SPEND_SLACK_WEBHOOK_URL", `style="height: 100%"`, `style="height: 40%"`,
		"owner@example.com", `aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "mystery-<model>") {
		t.Error("model name must be escaped")
	}
}

func TestSpend_ScopeParam(t *testing.T) {
	cases := map[string]int64{"/admin/spend?account=3": 3, "/admin/spend?account=all": -1, "/admin/spend?account=-2": -1, "/admin/spend?account=x": -1}
	for path, want := range cases {
		// Arrange
		fs := sampleSpend()
		mux := spendEnv(t, fs)

		// Act
		serve(mux, gapGet(path))

		// Assert
		if len(fs.scopes) != 1 || fs.scopes[0] != want {
			t.Errorf("%s → %v, want %d", path, fs.scopes, want)
		}
	}
}

func TestSpend_OffWithoutDatabase(t *testing.T) {
	// Arrange
	mux := spendEnv(t, nil)

	// Act
	page := serve(mux, gapGet("/admin/spend"))
	post := serve(mux, spendPost(url.Values{"action": {"save_cap"}, "account": {"0"}, "daily": {"5"}}))

	// Assert
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "AI spend is off.") || post.Code != http.StatusServiceUnavailable {
		t.Fatalf("page=%d post=%d", page.Code, post.Code)
	}
}

func TestSpend_SaveCap(t *testing.T) {
	// Arrange
	fs := sampleSpend()
	mux := spendEnv(t, fs)

	// Act
	rec := serve(mux, spendPost(url.Values{"action": {"save_cap"}, "account": {"2"}, "daily": {" $4.5 "}, "monthly": {"100"}, "scope": {"2"}}))
	removed := serve(mux, spendPost(url.Values{"action": {"save_cap"}, "account": {"0"}, "scope": {"all"}}))

	// Assert
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/spend?done=saved&account=2" {
		t.Fatalf("code=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if removed.Code != http.StatusSeeOther || removed.Header().Get("Location") != "/admin/spend?done=saved" {
		t.Fatalf("remove code=%d location=%q", removed.Code, removed.Header().Get("Location"))
	}
	want := spend.Cap{AccountID: 2, Daily: 4_500_000, Monthly: 100_000_000, UpdatedBy: "admin password"}
	if len(fs.saved) != 2 || fs.saved[0] != want || fs.saved[1] != (spend.Cap{AccountID: 0, UpdatedBy: "admin password"}) {
		t.Fatalf("saved = %+v", fs.saved)
	}
}

func TestSpend_BadCapChangesNothing(t *testing.T) {
	bad := []url.Values{
		{"action": {"save_cap"}, "account": {"-1"}, "daily": {"5"}},
		{"action": {"save_cap"}, "account": {"abc"}, "daily": {"5"}},
		{"action": {"save_cap"}, "account": {"1"}, "daily": {"0"}},
		{"action": {"save_cap"}, "account": {"1"}, "daily": {"-3"}},
		{"action": {"save_cap"}, "account": {"1"}, "monthly": {"1.005"}},
		{"action": {"save_cap"}, "account": {"1"}, "monthly": {"1e9"}},
		{"action": {"save_cap"}, "account": {"1"}, "monthly": {"NaN"}},
		{"action": {"delete_everything"}, "account": {"1"}},
	}
	for _, v := range bad {
		// Arrange
		fs := sampleSpend()
		mux := spendEnv(t, fs)

		// Act
		rec := serve(mux, spendPost(v))

		// Assert
		if rec.Code != http.StatusBadRequest || len(fs.saved) != 0 {
			t.Errorf("%v → code=%d saved=%v", v, rec.Code, fs.saved)
		}
	}
}

func TestSpend_PostNeedsAuthAndSameOrigin(t *testing.T) {
	// Arrange
	fs := sampleSpend()
	mux := spendEnv(t, fs)
	v := url.Values{"action": {"save_cap"}, "account": {"0"}, "daily": {"1"}}
	noAuth := spendPost(v)
	noAuth.Header.Del("Authorization")
	wrongPw := spendPost(v)
	wrongPw.SetBasicAuth(Username, "wrong-password")
	crossSite := spendPost(v)
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")

	// Act
	a := serve(mux, noAuth)
	b := serve(mux, wrongPw)
	c := serve(mux, crossSite)

	// Assert
	if a.Code != http.StatusUnauthorized || b.Code != http.StatusUnauthorized || c.Code != http.StatusForbidden || len(fs.saved) != 0 {
		t.Fatalf("no auth=%d wrong pw=%d cross-site=%d saved=%v", a.Code, b.Code, c.Code, fs.saved)
	}
}

func TestSpend_SaveFailureShowsError(t *testing.T) {
	// Arrange
	fs := sampleSpend()
	fs.saveErr = errors.New("db down")
	mux := spendEnv(t, fs)

	// Act
	rec := serve(mux, spendPost(url.Values{"action": {"save_cap"}, "account": {"0"}, "daily": {"5"}}))

	// Assert
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "The cap was not saved.") {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestSpend_BannerOnHomeAndBadgeInNav(t *testing.T) {
	// Arrange
	fs := sampleSpend()
	fs.warnings = []spend.Status{
		{AccountID: 0, Period: spend.PeriodMonth, Cap: 100_000_000, Spent: 85_000_000, Percent: 85, Level: 80},
		{AccountID: 2, Period: spend.PeriodDay, Cap: 1_000_000, Spent: 1_300_000, Percent: 130, Level: 100},
	}
	mux := spendEnv(t, fs)
	quiet := spendEnv(t, sampleSpend())

	// Act
	home := serve(mux, gapGet("/admin")).Body.String()
	gaps := serve(mux, gapGet("/admin/gaps")).Body.String()
	calm := serve(quiet, gapGet("/admin")).Body.String()

	// Assert
	for _, want := range []string{"AI spend is over the cap today", "Account 2 has used $1.30 of $1.00 (130%).",
		"AI spend is near the cap this month", "The bot keeps answering.", `class="badge nav-alert">100%+`} {
		if !strings.Contains(home, want) {
			t.Errorf("home lacks %q", want)
		}
	}
	if !strings.Contains(gaps, `class="badge nav-alert">100%+`) || strings.Contains(gaps, "AI spend is over") {
		t.Error("other pages carry the nav badge only")
	}
	if strings.Contains(calm, `class="badge nav-alert"`) || strings.Contains(calm, "AI spend is") {
		t.Error("no warning without a cap at 80%")
	}
}

func TestParseCapUSD(t *testing.T) {
	cases := map[string]struct {
		want spend.Micros
		ok   bool
	}{"": {0, true}, "5": {5_000_000, true}, "$0.01": {10_000, true}, "12.34": {12_340_000, true}, "100000": {100_000_000_000, true},
		"0": {0, false}, "-1": {0, false}, "1.234": {0, false}, "100000.01": {0, false}, "Inf": {0, false}, "five": {0, false}}
	for in, c := range cases {
		// Act
		got, ok := parseCapUSD(in)

		// Assert
		if got != c.want || ok != c.ok {
			t.Errorf("parseCapUSD(%q) = %d, %v; want %d, %v", in, got, ok, c.want, c.ok)
		}
	}
}
