package admin

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/leads"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/settings"
)

type fakeLeadList struct {
	rows    []leads.Lead
	filters []leads.Filter
	accts   []int64
	err     error
}

func (f *fakeLeadList) List(_ context.Context, acct int64, flt leads.Filter) ([]leads.Lead, error) {
	f.accts, f.filters = append(f.accts, acct), append(f.filters, flt)
	if f.err != nil {
		return nil, f.err
	}
	var out []leads.Lead
	for _, l := range f.rows {
		if l.AccountID == acct && (flt.Stage == "" || l.Stage == flt.Stage) {
			out = append(out, l)
		}
	}
	return out, nil
}

func (f *fakeLeadList) Counts(_ context.Context, acct int64) (map[leads.Stage]int, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := map[leads.Stage]int{}
	for _, l := range f.rows {
		if l.AccountID == acct {
			out[l.Stage]++
		}
	}
	return out, nil
}

func sampleLeads() *fakeLeadList {
	at := time.Date(2026, 11, 15, 3, 0, 0, 0, time.UTC)
	return &fakeLeadList{rows: []leads.Lead{
		{AccountID: 3, Source: leads.SourceChat, ConversationID: 41, InboxID: 2, ContactID: 9, Name: "Somchai <b>", Interest: "AutoCut course", Stage: leads.StageQuoted, FirstAt: at, LastAt: at},
		{AccountID: 3, Source: leads.SourceChat, ConversationID: 42, Name: "=HYPERLINK(\"http://x\")", Stage: leads.StageAsked, LastAt: at},
		{AccountID: 3, Source: leads.SourceComment, CommentID: 5, Name: "Commenter", Interest: "asks the price", Stage: leads.StageCommented, LastAt: at},
		{AccountID: 4, Source: leads.SourceChat, ConversationID: 1, Name: "Other account", Stage: leads.StagePaid, LastAt: at},
	}}
}

func leadsEnv(t *testing.T, fl LeadList, logs io.Writer) (*http.ServeMux, *memStore) {
	t.Helper()
	store, err := settings.Open(t.TempDir()+"/s.json", settings.LLM{})
	if err != nil {
		t.Fatal(err)
	}
	if logs == nil {
		logs = io.Discard
	}
	ms := &memStore{}
	h := &Handler{Password: pw, Store: store, Switch: &llm.Switch{}, Timeout: time.Second,
		Logger:      slog.New(slog.NewJSONHandler(logs, nil)),
		Knowledge:   &knowledge.Service{Store: ms, Embedder: constEmbedder{"m1"}, TopK: 4, ChunkRunes: 800},
		ChatwootURL: "https://pronto.example", Leads: fl}
	mux := http.NewServeMux()
	h.Routes(mux)
	return mux, ms
}

func TestLeads_ListShowsNameStageAndChatLink(t *testing.T) {
	// Arrange
	mux, _ := leadsEnv(t, sampleLeads(), nil)

	// Act
	rec := serve(mux, gapGet("/admin/leads?account=3"))

	// Assert
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, body)
	}
	for _, want := range []string{"Somchai &lt;b&gt;", "AutoCut course", "Got price or link", "Asked price", "Commented", "Facebook comment",
		`href="https://pronto.example/app/accounts/3/conversations/41"`, `href="/admin/comments?account=3"`, `href="/admin/leads.csv?account=3"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "Somchai <b>") || strings.Contains(body, "Other account") {
		t.Error("unescaped name or another account's lead on the page")
	}
}

func TestLeads_StageFilterAndUnknownStage(t *testing.T) {
	// Arrange
	fl := sampleLeads()
	mux, _ := leadsEnv(t, fl, nil)

	// Act
	quoted := serve(mux, gapGet("/admin/leads?account=3&stage=quoted"))
	bogus := serve(mux, gapGet("/admin/leads?account=3&stage=won';--"))

	// Assert
	if quoted.Code != http.StatusOK || !strings.Contains(quoted.Body.String(), "AutoCut course") || strings.Contains(quoted.Body.String(), "Commenter") {
		t.Fatalf("quoted filter: %d", quoted.Code)
	}
	if bogus.Code != http.StatusOK || fl.filters[len(fl.filters)-1].Stage != "" {
		t.Fatalf("unknown stage: status %d filter %+v", bogus.Code, fl.filters[len(fl.filters)-1])
	}
}

func TestLeads_StoreErrorShowsMessageAndLogsNoNames(t *testing.T) {
	// Arrange
	var logs bytes.Buffer
	mux, _ := leadsEnv(t, &fakeLeadList{err: errors.New("db down")}, &logs)

	// Act
	rec := serve(mux, gapGet("/admin/leads?account=3"))

	// Assert
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Could not read the lead list") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logs.String(), "list leads") {
		t.Fatalf("error not logged: %s", logs.String())
	}
}

func TestLeads_OffWithoutDatabase(t *testing.T) {
	// Arrange
	mux, _ := leadsEnv(t, nil, nil)

	// Act
	page := serve(mux, gapGet("/admin/leads"))
	file := serve(mux, gapGet("/admin/leads.csv?account=3"))

	// Assert
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "The lead list is off.") {
		t.Fatalf("page: %d", page.Code)
	}
	if file.Code != http.StatusServiceUnavailable {
		t.Fatalf("csv: %d", file.Code)
	}
}

func TestLeads_CSVExportNeutralisesFormulasAndKeepsThai(t *testing.T) {
	// Arrange
	fl := sampleLeads()
	fl.rows[0].Name = "สมชาย"
	var logs bytes.Buffer
	mux, _ := leadsEnv(t, fl, &logs)

	// Act
	rec := serve(mux, gapGet("/admin/leads.csv?account=3"))

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if ct, cd := rec.Header().Get("Content-Type"), rec.Header().Get("Content-Disposition"); !strings.HasPrefix(ct, "text/csv") ||
		!strings.HasPrefix(cd, `attachment; filename="leads-account-3-`) || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers: %v", rec.Header())
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "\uFEFF") {
		t.Fatal("no byte-order mark for Excel")
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\uFEFF"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || rows[0][0] != "name" || rows[0][7] != "chat_link" {
		t.Fatalf("rows = %v", rows)
	}
	if rows[1][0] != "สมชาย" || rows[1][3] != "quoted" || rows[1][6] != "9" || rows[1][7] != "https://pronto.example/app/accounts/3/conversations/41" {
		t.Errorf("row 1 = %v", rows[1])
	}
	if !strings.HasPrefix(rows[2][0], "'=") {
		t.Errorf("formula not neutralised: %q", rows[2][0])
	}
	if rows[3][1] != "Facebook comment" || rows[3][7] != "" {
		t.Errorf("comment row = %v", rows[3])
	}
	if strings.Contains(logs.String(), "สมชาย") || !strings.Contains(logs.String(), "leads exported") {
		t.Errorf("logs: %s", logs.String())
	}
}

func TestLeads_CSVStageFilterAndBadAccount(t *testing.T) {
	// Arrange
	fl := sampleLeads()
	mux, _ := leadsEnv(t, fl, nil)

	// Act
	paidOnly := serve(mux, gapGet("/admin/leads.csv?account=4&stage=paid"))
	bad := serve(mux, gapGet("/admin/leads.csv?account=-1"))
	missing := serve(mux, gapGet("/admin/leads.csv"))

	// Assert
	rows, _ := csv.NewReader(strings.NewReader(strings.TrimPrefix(paidOnly.Body.String(), "\uFEFF"))).ReadAll()
	if paidOnly.Code != http.StatusOK || len(rows) != 2 || rows[1][0] != "Other account" {
		t.Fatalf("paid only: %d %v", paidOnly.Code, rows)
	}
	if bad.Code != http.StatusBadRequest || missing.Code != http.StatusBadRequest {
		t.Fatalf("bad account: %d, missing: %d", bad.Code, missing.Code)
	}
	if len(fl.accts) != 1 {
		t.Fatalf("store read for a rejected request: %v", fl.accts)
	}
}

func TestLeads_RequireAdminPassword(t *testing.T) {
	// Arrange
	fl := sampleLeads()
	mux, _ := leadsEnv(t, fl, nil)
	var codes []int

	// Act
	for _, path := range []string{"/admin/leads?account=3", "/admin/leads.csv?account=3", "/admin/promotions?account=3"} {
		anon := httptest.NewRequest(http.MethodGet, path, nil)
		wrong := httptest.NewRequest(http.MethodGet, path, nil)
		wrong.SetBasicAuth(Username, "wrong-password")
		codes = append(codes, serve(mux, anon).Code, serve(mux, wrong).Code)
	}

	// Assert
	for i, c := range codes {
		if c != http.StatusUnauthorized {
			t.Errorf("request %d: status %d, want 401", i, c)
		}
	}
	if len(fl.accts) != 0 {
		t.Fatalf("lead store read without a password: %v", fl.accts)
	}
}

func TestLeadCell_NeutralisesFormulaStarts(t *testing.T) {
	// Arrange: cells a spreadsheet could run as a formula, including after
	// leading spaces and with full-width signs, plus plain values.
	risky := []string{"=1+1", "+1", "-1", "@SUM(A1)", "\t=1", "\r=1", " =HYPERLINK(\"x\")", "　=1", "＝1", "＋1", "－1", "＠x"}
	plain := []string{"", "สมชาย", "Napat K.", "ถามราคา 2,900 บาท", "(no name)"}

	// Act, Assert
	for _, s := range risky {
		if got := leadCell(s); got != "'"+s {
			t.Errorf("leadCell(%q) = %q, want a leading apostrophe", s, got)
		}
	}
	for _, s := range plain {
		if got := leadCell(s); got != s {
			t.Errorf("leadCell(%q) = %q, want unchanged", s, got)
		}
	}
}
