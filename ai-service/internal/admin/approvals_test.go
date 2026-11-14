package admin

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/pending"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/settings"
)

type approveCall struct {
	op       string
	acct, id int64
	version  int
	approver string
}

type fakeApprover struct {
	calls []approveCall
	err   error
}

func (f *fakeApprover) Approve(_ context.Context, acct, id int64, version int, approver string) error {
	f.calls = append(f.calls, approveCall{"approve", acct, id, version, approver})
	return f.err
}

func (f *fakeApprover) Reject(_ context.Context, acct, id int64, version int, approver string) error {
	f.calls = append(f.calls, approveCall{"reject", acct, id, version, approver})
	return f.err
}

type approvalsEnv struct {
	mux      *http.ServeMux
	pend     *pending.Memory
	approver *fakeApprover
	profiles *memProfiles
}

func newApprovalsEnv(t *testing.T, enabled bool) *approvalsEnv {
	t.Helper()
	store, err := settings.Open(t.TempDir()+"/s.json", settings.LLM{})
	if err != nil {
		t.Fatal(err)
	}
	e := &approvalsEnv{pend: &pending.Memory{}, approver: &fakeApprover{},
		profiles: &memProfiles{saved: map[int64]profile.Profile{2: {AccountID: 2, Tone: "Polite."}}}}
	ms := &memStore{docs: []knowledge.Document{{ID: 1, AccountID: 2, Kind: knowledge.KindFAQ, Title: "Delivery", Content: "Old delivery answer", SourceKey: "delivery"}}}
	h := &Handler{Password: pw, Store: store, Switch: &llm.Switch{}, Timeout: time.Second, Profiles: e.profiles,
		Knowledge: &knowledge.Service{Store: ms, Embedder: constEmbedder{"m1"}, TopK: 4, ChunkRunes: 800},
		Logger:    slog.New(slog.NewJSONHandler(io.Discard, nil))}
	if enabled {
		h.Pending, h.Approver = e.pend, e.approver
	}
	e.mux = http.NewServeMux()
	h.Routes(e.mux)
	return e
}

func (e *approvalsEnv) propose(t *testing.T, acct int64, kind pending.Kind, target string, v any) pending.Change {
	t.Helper()
	b, _ := json.Marshal(v)
	id, err := e.pend.Propose(context.Background(), pending.Change{AccountID: acct, Kind: kind, Target: target, Proposed: b, Actor: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := e.pend.Get(context.Background(), acct, id)
	return c
}

func approvalsGet(query string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/admin/approvals?"+query, nil)
	r.SetBasicAuth(Username, pw)
	return r
}

func approvalsPost(v url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/admin/approvals", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.SetBasicAuth(Username, pw)
	return r
}

func TestApprovals_ListsChangesOfTheAccountOnly(t *testing.T) {
	// Arrange
	e := newApprovalsEnv(t, true)
	e.propose(t, 2, pending.KindProfile, pending.ProfileTarget, map[string]string{"tone": "Warm <b>and</b> short."})
	e.propose(t, 2, pending.KindDocument, "hours", map[string]any{"key": "hours", "kind": "faq", "inbox_id": 7, "title": "Opening hours", "content": "9 to 5"})
	e.propose(t, 2, pending.KindDocument, "delivery", map[string]any{"key": "delivery", "title": "Delivery", "content": "New delivery answer"})
	e.propose(t, 3, pending.KindProfile, pending.ProfileTarget, map[string]string{"tone": "Other account secret"})

	// Act
	page := serve(e.mux, approvalsGet("account=2"))
	body := page.Body.String()

	// Assert
	if page.Code != http.StatusOK {
		t.Fatalf("status %d", page.Code)
	}
	for _, want := range []string{"Tone and style", "Polite.", "Warm &lt;b&gt;and&lt;/b&gt; short.", "Opening hours", "9 to 5", "claude", "New document", "Old delivery answer", "New delivery answer", "Only inbox 7", "FAQ (question and answer)", "All inboxes"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "Other account secret") || strings.Contains(body, "<b>and</b>") {
		t.Error("page shows another account's change or unescaped text")
	}
}

func TestApprovals_EmptyState(t *testing.T) {
	// Arrange
	e := newApprovalsEnv(t, true)

	// Act
	body := serve(e.mux, approvalsGet("account=2")).Body.String()

	// Assert
	if !strings.Contains(body, "Nothing is waiting") {
		t.Fatalf("no empty state: %s", body)
	}
}

func TestApprovals_ApproveAndReject(t *testing.T) {
	for _, op := range []string{"approve", "reject"} {
		t.Run(op, func(t *testing.T) {
			// Arrange
			e := newApprovalsEnv(t, true)
			c := e.propose(t, 2, pending.KindProfile, pending.ProfileTarget, map[string]string{"tone": "Warm."})

			// Act
			res := serve(e.mux, approvalsPost(url.Values{"account": {"2"}, "id": {"1"}, "version": {"1"}, "decision": {op}}))

			// Assert
			want := approveCall{op, 2, c.ID, c.Version, "admin password"}
			if len(e.approver.calls) != 1 || e.approver.calls[0] != want {
				t.Fatalf("calls = %+v, want %+v", e.approver.calls, want)
			}
			done := map[string]string{"approve": "approved", "reject": "rejected"}[op]
			if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/admin/approvals?account=2&done="+done {
				t.Fatalf("status %d location %q", res.Code, res.Header().Get("Location"))
			}
			// The page it returns to confirms the decision.
			page := httptest.NewRequest(http.MethodGet, res.Header().Get("Location"), nil)
			page.SetBasicAuth(Username, pw)
			notice := map[string]string{"approve": "Approved. The bot uses it from the next message.", "reject": "Rejected. Nothing changed."}[op]
			if body := serve(e.mux, page).Body.String(); !strings.Contains(body, notice) {
				t.Fatalf("no %q notice after %s", notice, op)
			}
		})
	}
}

func TestApprovals_ChangedSinceOpenedAsksToLookAgain(t *testing.T) {
	// Arrange
	e := newApprovalsEnv(t, true)
	e.propose(t, 2, pending.KindProfile, pending.ProfileTarget, map[string]string{"tone": "Newer text."})
	e.approver.err = pending.ErrChanged

	// Act
	res := serve(e.mux, approvalsPost(url.Values{"account": {"2"}, "id": {"1"}, "version": {"1"}, "decision": {"approve"}}))

	// Assert
	if res.Code != http.StatusConflict || !strings.Contains(res.Body.String(), "changed this after you opened") ||
		!strings.Contains(res.Body.String(), "Newer text.") {
		t.Fatalf("status %d body %s", res.Code, res.Body.String())
	}
}

func TestApprovals_BadPostsChangeNothing(t *testing.T) {
	cases := map[string]url.Values{
		"no account":   {"id": {"1"}, "version": {"1"}, "decision": {"approve"}},
		"bad id":       {"account": {"2"}, "id": {"x"}, "version": {"1"}, "decision": {"approve"}},
		"no version":   {"account": {"2"}, "id": {"1"}, "decision": {"approve"}},
		"bad decision": {"account": {"2"}, "id": {"1"}, "version": {"1"}, "decision": {"delete"}},
	}
	for name, form := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			e := newApprovalsEnv(t, true)

			// Act
			res := serve(e.mux, approvalsPost(form))

			// Assert
			if res.Code != http.StatusBadRequest || len(e.approver.calls) != 0 {
				t.Fatalf("status %d calls %+v", res.Code, e.approver.calls)
			}
		})
	}
}

func TestApprovals_CrossOriginAndNoAuthRefused(t *testing.T) {
	// Arrange
	e := newApprovalsEnv(t, true)
	form := url.Values{"account": {"2"}, "id": {"1"}, "version": {"1"}, "decision": {"approve"}}
	cross := approvalsPost(form)
	cross.Header.Set("Sec-Fetch-Site", "cross-site")
	noAuth := approvalsPost(form)
	noAuth.Header.Del("Authorization")

	// Act
	crossRes, noAuthRes := serve(e.mux, cross), serve(e.mux, noAuth)

	// Assert
	if crossRes.Code != http.StatusForbidden || noAuthRes.Code != http.StatusUnauthorized || len(e.approver.calls) != 0 {
		t.Fatalf("cross %d noauth %d calls %+v", crossRes.Code, noAuthRes.Code, e.approver.calls)
	}
}

func TestApprovals_OffWithoutStore(t *testing.T) {
	// Arrange
	e := newApprovalsEnv(t, false)

	// Act
	page := serve(e.mux, approvalsGet("account=2"))
	res := serve(e.mux, approvalsPost(url.Values{"account": {"2"}, "id": {"1"}, "version": {"1"}, "decision": {"approve"}}))

	// Assert
	if !strings.Contains(page.Body.String(), "Approvals are off") || res.Code != http.StatusServiceUnavailable {
		t.Fatalf("page %q post %d", page.Body.String(), res.Code)
	}
}

func TestApprovals_HomeShowsWaitingCount(t *testing.T) {
	// Arrange
	e := newApprovalsEnv(t, true)
	e.propose(t, 2, pending.KindProfile, pending.ProfileTarget, map[string]string{"tone": "Warm."})
	e.propose(t, 2, pending.KindDocument, "hours", map[string]any{"key": "hours", "title": "Hours", "content": "9 to 5"})
	home := func(acct string) string {
		r := httptest.NewRequest(http.MethodGet, "/admin?account="+acct, nil)
		r.SetBasicAuth(Username, pw)
		return serve(e.mux, r).Body.String()
	}

	// Act
	two, other := home("2"), home("3")

	// Assert
	if !strings.Contains(two, "2 AI changes wait for your approval") || !strings.Contains(two, `href="/admin/approvals?account=2"`) {
		t.Fatalf("home lacks the waiting notice: %s", two)
	}
	if strings.Contains(other, "wait for your approval") {
		t.Fatal("another account's changes counted")
	}
}

func TestApprovals_UnchangedFieldsAndNewDocumentsStayShort(t *testing.T) {
	// Arrange: the delivery title stays the same; "hours" is new.
	e := newApprovalsEnv(t, true)
	e.propose(t, 2, pending.KindDocument, "delivery", map[string]any{"key": "delivery", "title": "Delivery", "content": "New answer"})
	e.propose(t, 2, pending.KindDocument, "hours", map[string]any{"key": "hours", "title": "Hours", "content": "9 to 5"})

	// Act
	body := serve(e.mux, approvalsGet("account=2")).Body.String()

	// Assert
	if !strings.Contains(body, "Unchanged") {
		t.Error("an unchanged field is not marked")
	}
	if strings.Contains(body, "(empty)") {
		t.Error("a new document shows empty current values")
	}
}

func TestApprovals_SourceProposalShowsWhatWillBeAdded(t *testing.T) {
	// Arrange: an AI app proposes a sheet tab and a web page.
	e := newApprovalsEnv(t, true)
	sheet := "https://docs.google.com/spreadsheets/d/abc/export?format=csv&gid=166"
	e.propose(t, 2, pending.KindSource, sheet, map[string]any{"url": sheet, "kind": "feed", "rows": 103, "format": "csv", "notes": []string{"first 500 rows used"}})
	e.propose(t, 2, pending.KindSource, "https://shop.example/faq", map[string]any{"url": "https://shop.example/faq", "kind": "page", "title": "FAQ <script>"})

	// Act
	body := serve(e.mux, approvalsGet("account=2")).Body.String()

	// Assert
	for _, want := range []string{"New source: docs.google.com/spreadsheets/d/abc/export", "format=csv&amp;gid=166",
		"Data file (CSV or JSON Lines)", "103 rows", "first 500 rows used", "New source: shop.example/faq", "Web page",
		"FAQ &lt;script&gt;", "After approval"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "New document") || strings.Contains(body, "(empty)") || strings.Contains(body, "FAQ <script>") {
		t.Error("a source shows as a document, shows empty values, or is not escaped")
	}
}

func TestApprovals_SourceProposalInThai(t *testing.T) {
	// Arrange
	e := newApprovalsEnv(t, true)
	e.propose(t, 2, pending.KindSource, "https://shop.example/faq", map[string]any{"url": "https://shop.example/faq", "kind": "page"})
	r := approvalsGet("account=2")
	r.AddCookie(&http.Cookie{Name: langCookie, Value: langTH})

	// Act
	body := serve(e.mux, r).Body.String()

	// Assert
	for _, want := range []string{"แหล่งข้อมูลใหม่: shop.example/faq", "หน้าเว็บ"} {
		if !strings.Contains(body, want) {
			t.Errorf("Thai page lacks %q", want)
		}
	}
}

func TestApprovals_DeleteProposalShowsWhatWillBeRemoved(t *testing.T) {
	// Arrange: an AI app asks to delete the delivery document (QA F-D-02).
	e := newApprovalsEnv(t, true)
	e.propose(t, 2, pending.KindDocument, "delivery", map[string]any{"key": "delivery", "title": "Delivery", "delete": true, "id": 1})

	// Act
	body := serve(e.mux, approvalsGet("account=2")).Body.String()

	// Assert
	for _, want := range []string{"Delete document", "Knowledge: delivery", "Old delivery answer", "After approval",
		"The document is deleted and the bot stops using it."} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "New document") || strings.Contains(body, "Unchanged") || strings.Contains(body, `class="code diff-new"`) {
		t.Error("a delete shows as a new or unchanged document")
	}
}

func TestApprovals_DeleteProposalInThai(t *testing.T) {
	// Arrange
	e := newApprovalsEnv(t, true)
	e.propose(t, 2, pending.KindDocument, "delivery", map[string]any{"key": "delivery", "title": "Delivery", "delete": true, "id": 1})
	r := approvalsGet("account=2")
	r.Header.Set("Accept-Language", "th")

	// Act
	body := serve(e.mux, r).Body.String()

	// Assert
	for _, english := range []string{"Delete document", "The document is deleted and the bot stops using it."} {
		if strings.Contains(body, english) {
			t.Errorf("page still says %q", english)
		}
	}
}

func TestApprovals_DocumentChangedSinceProposalIsExplained(t *testing.T) {
	// Arrange: the owner edited the document after the app proposed (QA F-C-07).
	e := newApprovalsEnv(t, true)
	e.propose(t, 2, pending.KindDocument, "delivery", map[string]any{"key": "delivery", "title": "Delivery", "content": "Older AI text"})
	e.approver.err = pending.ErrTargetChanged

	// Act
	res := serve(e.mux, approvalsPost(url.Values{"account": {"2"}, "id": {"1"}, "version": {"1"}, "decision": {"approve"}}))

	// Assert
	if res.Code != http.StatusConflict || !strings.Contains(res.Body.String(), "changed since the app proposed this") {
		t.Fatalf("status %d body %s", res.Code, res.Body.String())
	}
}
