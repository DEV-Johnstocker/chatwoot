package admin

import (
	"bytes"
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
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/referral"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/settings"
)

type fakeAds struct {
	chats  map[int64][]referral.Referral
	rows   []referral.Row
	err    error
	acct   int64
	asked  []int64
	credit referral.Credit
}

func (f *fakeAds) ForChats(_ context.Context, acct int64, convs []int64, _ int) (map[int64][]referral.Referral, error) {
	f.acct, f.asked = acct, convs
	return f.chats, f.err
}

func (f *fakeAds) Report(_ context.Context, acct int64, _, _ time.Time, c referral.Credit) ([]referral.Row, error) {
	f.acct, f.credit = acct, c
	return f.rows, f.err
}

func adsEnv(t *testing.T, ads *fakeAds, logs io.Writer) *http.ServeMux {
	t.Helper()
	store, err := settings.Open(t.TempDir()+"/s.json", settings.LLM{})
	if err != nil {
		t.Fatal(err)
	}
	if logs == nil {
		logs = io.Discard
	}
	loc, _ := time.LoadLocation("Asia/Bangkok")
	h := &Handler{Password: pw, Store: store, Switch: &llm.Switch{}, Timeout: time.Second, Location: loc,
		Logger: slog.New(slog.NewJSONHandler(logs, nil)), ChatwootURL: "https://pronto.example",
		Leads: sampleLeads(), Sales: sampleSales()}
	if ads != nil {
		h.Ads = ads
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	return mux
}

func adGet(path string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.SetBasicAuth(Username, pw)
	return r
}

func TestLeads_AdColumnShowsLatestAndEarlierAds(t *testing.T) {
	// Arrange
	t1 := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	ads := &fakeAds{chats: map[int64][]referral.Referral{
		41: {{Source: "ADS", AdID: "ws", AdTitle: "Workshop <12 ธ.ค.>", ReferredAt: t1.Add(24 * time.Hour)},
			{Source: "ADS", AdID: "jac", AdTitle: "John Auto Cut", ReferredAt: t1}},
		42: {{Source: "SHORTLINK", Ref: "adtest1", ReferredAt: t1}},
	}}
	mux := adsEnv(t, ads, nil)

	// Act
	rec := serve(mux, adGet("/admin/leads?account=3"))

	// Assert
	body := rec.Body.String()
	for _, want := range []string{">Ad<", "Workshop &lt;12 ธ.ค.&gt;", "John Auto Cut", "Link: adtest1"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if ads.acct != 3 || len(ads.asked) != 2 {
		t.Errorf("asked account %d chats %v; want account 3, its two chat leads", ads.acct, ads.asked)
	}
}

func TestLeads_NoAdColumnWithoutAdTracking(t *testing.T) {
	// Arrange
	mux := adsEnv(t, nil, nil)

	// Act
	rec := serve(mux, adGet("/admin/leads?account=3"))

	// Assert
	if strings.Contains(rec.Body.String(), ">Ad<") {
		t.Error("Ad column shown while ad tracking is off")
	}
}

func TestLeads_AdLookupErrorStillShowsLeads(t *testing.T) {
	// Arrange
	var logs bytes.Buffer
	mux := adsEnv(t, &fakeAds{err: errors.New("db down")}, &logs)

	// Act
	rec := serve(mux, adGet("/admin/leads?account=3"))

	// Assert
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "AutoCut course") {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(logs.String(), "lead ads") {
		t.Errorf("error not logged: %s", logs.String())
	}
}

func TestSales_AdsSectionWithCreditSwitch(t *testing.T) {
	// Arrange
	ads := &fakeAds{rows: []referral.Row{
		{Key: "ad:ws", Label: "Workshop", IsAd: true, Chats: 4, Leads: 3, Paid: 2, Revenue: 1780000},
		{Key: "ref:SHORTLINK:adtest1", Label: "adtest1", Chats: 1},
		{Key: referral.NoSource, Paid: 1, Revenue: 390000},
	}}
	mux := adsEnv(t, ads, nil)

	// Act
	last := serve(mux, adGet("/admin/sales?account=3")).Body.String()
	lastCredit := ads.credit
	first := serve(mux, adGet("/admin/sales?account=3&credit=first")).Body.String()

	// Assert
	for _, want := range []string{`id="ads-title"`, "Workshop", "฿17,800", "Link: adtest1", "No ad", "฿3,900", "Ads Manager counts more",
		`href="/admin/sales?account=3&amp;credit=first"`} {
		if !strings.Contains(last, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if lastCredit != referral.CreditLast || ads.credit != referral.CreditFirst || !strings.Contains(first, `aria-current="true">First ad`) {
		t.Errorf("credit default %q then %q", lastCredit, ads.credit)
	}
}

func TestSales_AdsEmptyStateAndUnknownCredit(t *testing.T) {
	// Arrange
	ads := &fakeAds{}
	mux := adsEnv(t, ads, nil)

	// Act
	body := serve(mux, adGet("/admin/sales?account=3&credit=bogus")).Body.String()

	// Assert
	if ads.credit != referral.CreditLast || !strings.Contains(body, "No ad taps yet") {
		t.Errorf("credit %q; empty state shown %v", ads.credit, strings.Contains(body, "No ad taps yet"))
	}
}

func TestSales_NoAdsSectionWithoutAdTracking(t *testing.T) {
	// Arrange
	mux := adsEnv(t, nil, nil)

	// Act
	body := serve(mux, adGet("/admin/sales?account=3")).Body.String()

	// Assert
	if strings.Contains(body, `id="ads-title"`) {
		t.Error("Ads section shown while ad tracking is off")
	}
}
