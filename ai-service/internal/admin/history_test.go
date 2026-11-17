package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/audit"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/feed"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/images"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/settings"
)

// memHistory is an audit store that also reads entries back (HistoryLog),
// like audit.Postgres.
type memHistory struct{ entries []audit.Entry }

func (m *memHistory) Record(_ context.Context, e audit.Event) error {
	var before []byte
	if e.Before != nil {
		before, _ = json.Marshal(e.Before)
	}
	m.add(audit.Entry{Source: audit.SourceAdmin, Actor: e.Actor, Action: e.Action, Summary: e.Summary, Before: before}, e.AccountID)
	return nil
}

func (m *memHistory) add(e audit.Entry, acct int64) {
	e.ID, e.At = int64(len(m.entries)+1), time.Date(2026, 11, 16, 9, 0, len(m.entries), 0, time.UTC)
	e.Summary = strconv.FormatInt(acct, 10) + "|" + e.Summary // the account rides along for the fake
	m.entries = append(m.entries, e)
}

func (m *memHistory) acctOf(e audit.Entry) (int64, audit.Entry) {
	a, s, _ := strings.Cut(e.Summary, "|")
	n, _ := strconv.ParseInt(a, 10, 64)
	e.Summary = s
	return n, e
}

func (m *memHistory) Recent(context.Context, int64, int) ([]audit.Event, error) { return nil, nil }

func (m *memHistory) History(_ context.Context, acct int64, limit int) ([]audit.Entry, error) {
	var out []audit.Entry
	for i := len(m.entries) - 1; i >= 0 && len(out) < limit; i-- {
		if a, e := m.acctOf(m.entries[i]); a == acct {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *memHistory) Entry(_ context.Context, acct int64, source string, id int64) (audit.Entry, error) {
	for _, raw := range m.entries {
		if a, e := m.acctOf(raw); a == acct && e.Source == source && e.ID == id {
			return e, nil
		}
	}
	return audit.Entry{}, audit.ErrNotFound
}

// actions lists the recorded actions, oldest first.
func (m *memHistory) actions() []string {
	var out []string
	for _, e := range m.entries {
		out = append(out, e.Action)
	}
	return out
}

type historyEnv struct {
	mux  *http.ServeMux
	h    *Handler
	log  *memHistory
	prof *memProfiles
	kb   *memStore
	img  *images.Memory
}

func newHistoryEnv(t *testing.T) *historyEnv {
	t.Helper()
	store, err := settings.Open(t.TempDir()+"/s.json", settings.LLM{})
	if err != nil {
		t.Fatal(err)
	}
	bkk, _ := time.LoadLocation("Asia/Bangkok")
	e := &historyEnv{log: &memHistory{}, prof: &memProfiles{saved: map[int64]profile.Profile{}}, kb: &memStore{}, img: &images.Memory{}}
	e.h = &Handler{Password: pw, Store: store, Switch: &llm.Switch{}, Timeout: time.Second, Location: bkk,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Events: e.log, Profiles: e.prof, Images: e.img,
		Knowledge: &knowledge.Service{Store: e.kb, Embedder: constEmbedder{"m1"}, TopK: 4, ChunkRunes: 800}}
	e.mux = http.NewServeMux()
	e.h.Routes(e.mux)
	return e
}

func (e *historyEnv) post(path string, v url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.SetBasicAuth(Username, pw)
	return serve(e.mux, r)
}

func (e *historyEnv) page(acct int64) string {
	r := httptest.NewRequest(http.MethodGet, "/admin/history?account="+strconv.FormatInt(acct, 10), nil)
	r.SetBasicAuth(Username, pw)
	return serve(e.mux, r).Body.String()
}

func (e *historyEnv) saveProfile(t *testing.T, name, rules, bank string) {
	t.Helper()
	rec := e.post("/admin/business", url.Values{"account": {"1"}, "business_name": {name}, "about": {"สอนตัดต่อ"},
		"rules": {rules}, "bank_transfer": {bank}, "feature_voice": {"on"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save profile: %d %s", rec.Code, rec.Body.String())
	}
}

// lastEntry is the newest entry of account 1 with this action.
func (e *historyEnv) lastEntry(t *testing.T, action string) audit.Entry {
	t.Helper()
	list, _ := e.log.History(context.Background(), 1, 100)
	for _, x := range list {
		if x.Action == action {
			return x
		}
	}
	t.Fatalf("no %q entry in %v", action, e.log.actions())
	return audit.Entry{}
}

func restoreForm(action string, x audit.Entry) url.Values {
	return url.Values{"account": {"1"}, "action": {action}, "source": {x.Source}, "id": {strconv.FormatInt(x.ID, 10)}}
}

func TestHistory_ShowsProfileChangeBeforeAndAfter(t *testing.T) {
	// Arrange
	e := newHistoryEnv(t)
	e.saveProfile(t, "AutoCut", "ตอบสุภาพ\nไม่ให้ส่วนลด", "กสิกร 123-4-56789-0")
	e.saveProfile(t, "AutoCut", "ตอบสุภาพ\nให้ส่วนลด 10%", "กสิกร 123-4-56789-0")

	// Act
	body := e.page(1)

	// Assert
	for _, want := range []string{"business profile saved", `class="chg">ไม่ให้ส่วนลด`, `class="chg">ให้ส่วนลด 10%`,
		"Restore the version before this change", "Download profile and answers", `href="/admin/history?account=`} {
		if !strings.Contains(body, want) {
			t.Fatalf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "56789") {
		t.Fatal("the history page must not show the bank account number")
	}
}

func TestHistory_RestoreProfileVersionKeepsBankAndIsLogged(t *testing.T) {
	// Arrange
	e := newHistoryEnv(t)
	e.saveProfile(t, "AutoCut", "กฎเดิม", "กสิกร 111-1-11111-1")
	e.saveProfile(t, "AutoCut ใหม่", "กฎใหม่", "กสิกร 222-2-22222-2")
	x := e.lastEntry(t, "business profile saved")
	form := restoreForm("restore_profile", x)
	form.Set("version", profileVersion(e.prof.saved[1]))

	// Act
	rec := e.post("/admin/history", form)

	// Assert
	p := e.prof.saved[1]
	if rec.Code != http.StatusSeeOther || p.Rules != "กฎเดิม" || p.BusinessName != "AutoCut" || !p.Features.Voice {
		t.Fatalf("code %d profile %+v", rec.Code, p)
	}
	if p.BankTransfer != "กสิกร 222-2-22222-2" {
		t.Fatalf("restore changed the bank account to %q; it must keep the current one", p.BankTransfer)
	}
	undo := e.lastEntry(t, "business profile restored")
	if snap, ok := profileSnapshot(undo.Before); !ok || snap.Rules != "กฎใหม่" || strings.Contains(string(undo.Before), "22222") {
		t.Fatalf("restore must log the replaced version, bank masked: %s", undo.Before)
	}
}

func TestHistory_RestoreProfileRefusedWhenPageIsStale(t *testing.T) {
	// Arrange
	e := newHistoryEnv(t)
	e.saveProfile(t, "AutoCut", "กฎเดิม", "")
	e.saveProfile(t, "AutoCut", "กฎใหม่", "")
	x := e.lastEntry(t, "business profile saved")
	form := restoreForm("restore_profile", x)
	form.Set("version", "12345")
	saves := e.prof.saves

	// Act
	rec := e.post("/admin/history", form)

	// Assert
	if rec.Code != http.StatusConflict || e.prof.saves != saves || e.prof.saved[1].Rules != "กฎใหม่" {
		t.Fatalf("code %d saves %d→%d rules %q", rec.Code, saves, e.prof.saves, e.prof.saved[1].Rules)
	}
}

func TestHistory_RestoreOtherAccountsChangeIsRefused(t *testing.T) {
	// Arrange: account 2's change, restored through account 1
	e := newHistoryEnv(t)
	e.log.add(audit.Entry{Source: audit.SourceAdmin, Action: "business profile saved", Before: []byte(`{"BusinessName":"Other","Rules":"x"}`)}, 2)
	e.saveProfile(t, "AutoCut", "กฎ", "")
	saves, n := e.prof.saves, len(e.log.entries)

	// Act
	rec := e.post("/admin/history", url.Values{"account": {"1"}, "action": {"restore_profile"}, "source": {"admin"}, "id": {"1"}})
	bad := e.post("/admin/history", url.Values{"account": {"0"}, "action": {"restore_profile"}, "source": {"admin"}, "id": {"1"}})
	unknown := e.post("/admin/history", url.Values{"account": {"1"}, "action": {"drop_everything"}})

	// Assert
	if rec.Code != http.StatusNotFound || e.prof.saves != saves || e.prof.saved[1].BusinessName != "AutoCut" || len(e.log.entries) != n {
		t.Fatalf("code %d saves %d→%d name %q", rec.Code, saves, e.prof.saves, e.prof.saved[1].BusinessName)
	}
	if bad.Code != http.StatusBadRequest || unknown.Code != http.StatusBadRequest {
		t.Fatalf("account 0 → %d, unknown action → %d", bad.Code, unknown.Code)
	}
}

func TestHistory_RestoreDeletedAndEditedAnswers(t *testing.T) {
	// Arrange
	e := newHistoryEnv(t)
	e.kb.docs = []knowledge.Document{
		{ID: 1, AccountID: 1, Kind: knowledge.KindFAQ, Title: "ราคาเท่าไหร่", Content: "2,900 บาท", SourceKey: "price"},
		{ID: 2, AccountID: 1, Kind: knowledge.KindText, Title: "เวลาเปิด", Content: "9:00-18:00"},
	}
	e.post("/admin/knowledge", url.Values{"account": {"1"}, "action": {"update"}, "id": {"2"}, "title": {"เวลาเปิด"}, "content": {"10:00-20:00"}})
	e.post("/admin/knowledge", url.Values{"account": {"1"}, "action": {"delete"}, "id": {"1"}})
	e.kb.docs = e.kb.docs[1:] // memStore.Delete only records the ID
	edited, deleted := e.lastEntry(t, "knowledge edited"), e.lastEntry(t, "knowledge deleted")
	page := e.page(1)

	// Act
	r1 := e.post("/admin/history", restoreForm("restore_doc", deleted))
	r2 := e.post("/admin/history", restoreForm("restore_doc", edited))

	// Assert
	for _, want := range []string{"Restore this answer", "Restore the text before this change", "What was deleted", `class="chg">10:00-20:00`} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %q", want)
		}
	}
	if r1.Code != http.StatusSeeOther || r2.Code != http.StatusSeeOther {
		t.Fatalf("restore codes %d %d: %s", r1.Code, r2.Code, r2.Body.String())
	}
	back, err := e.kb.GetByKey(context.Background(), 1, "price")
	if err != nil || back.Kind != knowledge.KindFAQ || back.Content != "2,900 บาท" {
		t.Fatalf("deleted FAQ not back: %+v %v", back, err)
	}
	if d, _ := e.kb.Get(context.Background(), 1, 2); d.Content != "9:00-18:00" {
		t.Fatalf("edited text not restored: %q", d.Content)
	}
	if acts := strings.Join(e.log.actions(), ","); strings.Count(acts, "knowledge restored") != 2 {
		t.Fatalf("restores not logged: %s", acts)
	}
}

func TestHistory_RestoreAIToolDeletion(t *testing.T) {
	// Arrange: an AI tool deleted a document (kb_audit copy)
	e := newHistoryEnv(t)
	e.log.add(audit.Entry{Source: audit.SourceAI, Actor: "Claude (AI tool)", Action: "knowledge deleted", DocumentID: 9,
		SourceKey: "promo-dec", Before: []byte(`{"id":9,"key":"promo-dec","kind":"text","title":"โปรธันวา","content":"ลด 20%","inbox_id":0,"valid_from":null,"valid_until":"2026-12-31T17:00:00Z"}`)}, 1)

	// Act
	rec := e.post("/admin/history", url.Values{"account": {"1"}, "action": {"restore_doc"}, "source": {"ai"}, "id": {"1"}})

	// Assert
	d, err := e.kb.GetByKey(context.Background(), 1, "promo-dec")
	if rec.Code != http.StatusSeeOther || err != nil || d.Content != "ลด 20%" || !d.ValidUntil.Equal(time.Date(2026, 12, 31, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("code %d doc %+v err %v", rec.Code, d, err)
	}
}

func TestHistory_SyncedDocumentIsNotRestored(t *testing.T) {
	// Arrange
	e := newHistoryEnv(t)
	e.log.add(audit.Entry{Source: audit.SourceAI, Action: "knowledge deleted", DocumentID: 4,
		Before: []byte(`{"id":4,"key":"web:https://autocut.example/price","kind":"text","title":"ราคา","content":"x"}`)}, 1)
	n := len(e.kb.docs)

	// Act
	rec := e.post("/admin/history", url.Values{"account": {"1"}, "action": {"restore_doc"}, "source": {"ai"}, "id": {"1"}})
	page := e.page(1)

	// Assert
	if rec.Code != http.StatusUnprocessableEntity || len(e.kb.docs) != n {
		t.Fatalf("code %d docs %d→%d", rec.Code, n, len(e.kb.docs))
	}
	if strings.Contains(page, "Restore this answer") || !strings.Contains(page, "came from a website or data file") {
		t.Fatal("a synced document must explain instead of offering Restore")
	}
}

func TestHistory_RemovedPictureCanBePutBack(t *testing.T) {
	// Arrange
	e := newHistoryEnv(t)
	id, err := e.img.Add(context.Background(), images.Image{Meta: images.Meta{AccountID: 1, Name: "รีวิว AutoCut"}, Data: testPNG(t)})
	if err != nil {
		t.Fatal(err)
	}
	_ = e.img.Delete(context.Background(), 1, id)
	removed, _ := e.img.Removed(context.Background(), 1)
	page := e.page(1)

	// Act
	other := e.post("/admin/history", url.Values{"account": {"2"}, "action": {"restore_image"}, "id": {strconv.FormatInt(removed[0].ID, 10)}})
	rec := e.post("/admin/history", url.Values{"account": {"1"}, "action": {"restore_image"}, "id": {strconv.FormatInt(removed[0].ID, 10)}})
	list, _ := e.img.List(context.Background(), 1)

	// Assert
	if !strings.Contains(page, "รีวิว AutoCut") || !strings.Contains(page, `value="restore_image"`) {
		t.Fatal("removed picture not offered")
	}
	if other.Code != http.StatusNotFound || rec.Code != http.StatusSeeOther || len(list) != 1 || list[0].Name != "รีวิว AutoCut" {
		t.Fatalf("other %d restore %d list %+v", other.Code, rec.Code, list)
	}
	if acts := strings.Join(e.log.actions(), ","); !strings.Contains(acts, "bot image restored") {
		t.Fatalf("not logged: %s", acts)
	}
}

func TestHistory_ExportJSONHasProfileAndAnswersNoSecrets(t *testing.T) {
	// Arrange
	e := newHistoryEnv(t)
	e.saveProfile(t, "AutoCut", "กฎ", "กสิกร 123-4-56789-0")
	if err := e.h.Store.Save(settings.LLM{Primary: settings.Provider{BaseURL: "https://api.openai.com/v1", APIKey: "fake SECRET provider key", Model: "m"}}); err != nil {
		t.Fatal(err)
	}
	e.kb.docs = []knowledge.Document{{ID: 3, AccountID: 1, Kind: knowledge.KindFAQ, Title: "ส่งไหม", Content: "ส่งฟรี", SourceKey: "feed:7:ship"},
		{ID: 4, AccountID: 2, Kind: knowledge.KindText, Title: "other", Content: "OTHER-ACCOUNT"}}

	// Act
	rec := e.post("/admin/history", url.Values{"account": {"1"}, "action": {"export_json"}})

	// Assert
	var f exportFile
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &f) != nil {
		t.Fatalf("code %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Disposition"), `attachment; filename="pronto-account-1-`) || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers %v", rec.Header())
	}
	if f.Profile == nil || f.Profile.BusinessName != "AutoCut" || f.Profile.BankTransfer != "กสิกร 123-4-56789-0" || !f.Profile.VoiceNotes {
		t.Fatalf("profile %+v", f.Profile)
	}
	if len(f.Documents) != 1 || f.Documents[0].ID != "ship" || f.Documents[0].Question != "ส่งไหม" || f.Documents[0].Answer != "ส่งฟรี" {
		t.Fatalf("documents %+v", f.Documents)
	}
	if body := rec.Body.String(); strings.Contains(body, "SECRET") || strings.Contains(body, "OTHER-ACCOUNT") || strings.Contains(body, pw) {
		t.Fatal("the download must hold no keys, passwords or other accounts' data")
	}
}

func TestHistory_ExportCSVReimportsWithTheFeedParser(t *testing.T) {
	// Arrange
	e := newHistoryEnv(t)
	bkk, _ := time.LoadLocation("Asia/Bangkok")
	e.kb.docs = []knowledge.Document{
		{ID: 1, AccountID: 1, Kind: knowledge.KindFAQ, Title: "ราคา", Content: "2,900 บาท"},
		{ID: 2, AccountID: 1, Kind: knowledge.KindText, Title: "โปร", Content: "=HYPERLINK(\"x\")\nลด 20%", SourceKey: "promo",
			ValidFrom: time.Date(2026, 12, 1, 0, 0, 0, 0, bkk), ValidUntil: time.Date(2027, 1, 1, 0, 0, 0, 0, bkk)},
		{ID: 3, AccountID: 1, Kind: knowledge.KindText, Title: "ซ้ำ", Content: "x", SourceKey: "feed:2:promo"},
		{ID: 4, AccountID: 1, Kind: knowledge.KindText, Title: "key", Content: "y", SourceKey: "=cmd|calc"},
	}

	// Act
	rec := e.post("/admin/history", url.Values{"account": {"1"}, "action": {"export_csv"}})
	parsed, err := feed.Parse(rec.Body.Bytes())

	// Assert
	if rec.Code != http.StatusOK || !bytes.HasPrefix(rec.Body.Bytes(), []byte("\xef\xbb\xbfid,question,answer,title,content,start_date,last_day")) {
		t.Fatalf("code %d body %q", rec.Code, rec.Body.String())
	}
	if err != nil || len(parsed.Rows) != 4 || parsed.Skipped != 0 {
		t.Fatalf("re-import: %v rows %+v problems %v", err, parsed.Rows, parsed.Problems)
	}
	faq, promo, dup := parsed.Rows[0], parsed.Rows[1], parsed.Rows[2]
	if faq.ID != "doc-1" || !faq.FAQ || faq.Title != "ราคา" || faq.Content != "2,900 บาท" {
		t.Fatalf("faq row %+v", faq)
	}
	if promo.ID != "promo" || promo.StartDate != "2026-12-01" || promo.LastDay != "2026-12-31" || !strings.HasPrefix(promo.Content, "'=HYPERLINK") {
		t.Fatalf("promo row %+v: dates must match the importer and a formula must be neutralised", promo)
	}
	if dup.ID != "promo-3" {
		t.Fatalf("duplicate id %q, want promo-3", dup.ID)
	}
	if formulaKey := parsed.Rows[3]; formulaKey.ID != "'=cmd|calc" || strings.Contains(rec.Body.String(), ",=cmd") {
		t.Fatalf("a key starting with = must be neutralised in every column: id %q", formulaKey.ID)
	}
}

func TestHistory_RefusesCrossSiteAndAnonymous(t *testing.T) {
	// Arrange
	e := newHistoryEnv(t)
	e.saveProfile(t, "AutoCut", "กฎเดิม", "")
	e.saveProfile(t, "AutoCut", "กฎใหม่", "")
	x := e.lastEntry(t, "business profile saved")
	n := len(e.log.entries)
	cross := func(v url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/admin/history", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.SetBasicAuth(Username, pw)
		return serve(e.mux, r)
	}
	anon := func(v url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/admin/history", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.SetBasicAuth(Username, "wrong")
		return serve(e.mux, r)
	}

	// Act
	c1 := cross(restoreForm("restore_profile", x))
	c2 := cross(url.Values{"account": {"1"}, "action": {"export_json"}})
	a1 := anon(restoreForm("restore_profile", x))
	a2 := anon(url.Values{"account": {"1"}, "action": {"export_csv"}})
	get := httptest.NewRequest(http.MethodGet, "/admin/history?account=1", nil)
	a3 := serve(e.mux, get)

	// Assert
	if c1.Code != http.StatusForbidden || c2.Code != http.StatusForbidden || strings.Contains(c2.Body.String(), "AutoCut") {
		t.Fatalf("cross-site: restore %d export %d", c1.Code, c2.Code)
	}
	if a1.Code != http.StatusUnauthorized || a2.Code != http.StatusUnauthorized || a3.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d %d %d", a1.Code, a2.Code, a3.Code)
	}
	if e.prof.saved[1].Rules != "กฎใหม่" || len(e.log.entries) != n {
		t.Fatal("a refused request changed something")
	}
}

func TestHistory_OffWithoutDatabase(t *testing.T) {
	// Arrange
	store, _ := settings.Open(t.TempDir()+"/s.json", settings.LLM{})
	h := &Handler{Password: pw, Store: store, Switch: &llm.Switch{}, Timeout: time.Second, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	mux := http.NewServeMux()
	h.Routes(mux)
	r := httptest.NewRequest(http.MethodGet, "/admin/history", nil)
	r.SetBasicAuth(Username, pw)

	// Act
	rec := serve(mux, r)

	// Assert
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "The history is off.") || strings.Contains(rec.Body.String(), "export_json") {
		t.Fatalf("code %d", rec.Code)
	}
}

func TestLineDiff_MarksOnlyChangedLines(t *testing.T) {
	// Arrange
	before, after := "a\nb\nc", "a\nB\nc\nd"

	// Act
	l, r := lineDiff(before, after)

	// Assert
	mark := func(ls []diffLine) string {
		var b strings.Builder
		for _, x := range ls {
			if x.Changed {
				b.WriteString("*")
			}
			b.WriteString(x.Text + " ")
		}
		return b.String()
	}
	if mark(l) != "a *b c " || mark(r) != "a *B c *d " {
		t.Fatalf("before %q after %q", mark(l), mark(r))
	}
}

func TestProfileSnapshot_ReadsAdminAndAIToolCopies(t *testing.T) {
	// Arrange
	admin := []byte(`{"BusinessName":"AutoCut","Rules":"r","Images":null,"Features":{"Voice":true,"Images":false,"Stickers":true}}`)
	ai := []byte(`{"business_name":"AutoCut","rules":"r","voice_notes":true,"images":false,"stickers":true}`)

	// Act
	p1, ok1 := profileSnapshot(admin)
	p2, ok2 := profileSnapshot(ai)
	_, ok3 := profileSnapshot([]byte(`{"title":"not a profile"}`))

	// Assert
	if !ok1 || !ok2 || ok3 || p1.BusinessName != p2.BusinessName || p1.Rules != p2.Rules || p1.Features != p2.Features {
		t.Fatalf("admin %+v ai %+v other %v", p1, p2, ok3)
	}
}

// The first save's "before" is an empty profile: restoring it would only
// wipe the profile, so the page explains instead and a forged post is refused.
func TestHistory_EmptyFirstVersionIsNotRestored(t *testing.T) {
	// Arrange
	e := newHistoryEnv(t)
	e.saveProfile(t, "AutoCut", "กฎ", "")
	x := e.lastEntry(t, "business profile saved")
	saves := e.prof.saves

	// Act
	page := e.page(1)
	rec := e.post("/admin/history", restoreForm("restore_profile", x))

	// Assert
	if strings.Contains(page, "Restore the version before this change") || !strings.Contains(page, "Before this change the profile was empty.") {
		t.Fatal("an empty first version must not offer Restore")
	}
	if rec.Code != http.StatusNotFound || e.prof.saves != saves || e.prof.saved[1].Rules != "กฎ" {
		t.Fatalf("code %d saves %d→%d", rec.Code, saves, e.prof.saves)
	}
}

// Restoring an answer from a page opened before someone else changed it is
// refused, like a stale edit on the Knowledge page (QA F-C-01).
func TestHistory_RestoreAnswerRefusedWhenPageIsStale(t *testing.T) {
	// Arrange
	e := newHistoryEnv(t)
	e.kb.docs = []knowledge.Document{{ID: 2, AccountID: 1, Kind: knowledge.KindText, Title: "เวลาเปิด", Content: "9:00-18:00", UpdatedAt: time.Now()}}
	e.post("/admin/knowledge", url.Values{"account": {"1"}, "action": {"update"}, "id": {"2"}, "title": {"เวลาเปิด"}, "content": {"10:00-20:00"}})
	edited := e.lastEntry(t, "knowledge edited")
	form := restoreForm("restore_doc", edited)
	form.Set("version", "123") // the page saw an older version
	n := len(e.log.entries)

	// Act
	rec := e.post("/admin/history", form)

	// Assert
	if d, _ := e.kb.Get(context.Background(), 1, 2); rec.Code != http.StatusConflict || d.Content != "10:00-20:00" || len(e.log.entries) != n {
		t.Fatalf("code %d content %q entries %d→%d", rec.Code, d.Content, n, len(e.log.entries))
	}
}

// A removed picture can be erased for good, only with the confirmation and
// only by its own account; then it is gone from the page.
func TestHistory_DeleteRemovedPictureForGood(t *testing.T) {
	// Arrange
	e := newHistoryEnv(t)
	id, _ := e.img.Add(context.Background(), images.Image{Meta: images.Meta{AccountID: 1, Name: "รีวิวคุณสมชาย"}, Data: testPNG(t)})
	_ = e.img.Delete(context.Background(), 1, id)
	removed, _ := e.img.Removed(context.Background(), 1)
	rid := strconv.FormatInt(removed[0].ID, 10)

	// Act
	noConfirm := e.post("/admin/history", url.Values{"account": {"1"}, "action": {"forget_image"}, "id": {rid}})
	other := e.post("/admin/history", url.Values{"account": {"2"}, "action": {"forget_image"}, "id": {rid}, "confirm": {"yes"}})
	kept, _ := e.img.Removed(context.Background(), 1)
	rec := e.post("/admin/history", url.Values{"account": {"1"}, "action": {"forget_image"}, "id": {rid}, "confirm": {"yes"}})
	after, _ := e.img.Removed(context.Background(), 1)

	// Assert
	if noConfirm.Code != http.StatusBadRequest || other.Code != http.StatusNotFound || len(kept) != 1 {
		t.Fatalf("no confirm %d, other account %d, kept %d", noConfirm.Code, other.Code, len(kept))
	}
	if rec.Code != http.StatusSeeOther || len(after) != 0 || !strings.Contains(strings.Join(e.log.actions(), ","), "removed bot image deleted for good") {
		t.Fatalf("code %d left %d actions %v", rec.Code, len(after), e.log.actions())
	}
	if strings.Contains(e.page(1), "รีวิวคุณสมชาย") {
		t.Fatal("an erased picture's name is still on the page")
	}
}
