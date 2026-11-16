package admin

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/nudge"
)

type nudgeTestEnv struct {
	mux   *http.ServeMux
	store *nudge.SettingsStore
	path  string
	ev    *memEvents
}

func nudgeEnv(t *testing.T, withStore bool) nudgeTestEnv {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nudge-settings.json")
	store, err := nudge.OpenSettings(path, nudge.Defaults{ResolveAfter: 22 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	ev := &memEvents{}
	h := &Handler{Password: pw, Events: ev, NudgeReady: true, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	if withStore {
		h.Nudge = store
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	return nudgeTestEnv{mux: mux, store: store, path: path, ev: ev}
}

func nudgeForm(acct string) url.Values {
	return url.Values{"account": {acct}, "on": {"on"}, "after_hours": {"20"}, "mode": {"write"}, "text": {"สวัสดีครับคุณลูกค้า ทักมาได้เลยครับ"}}
}

func npost(v url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/admin/nudge", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.SetBasicAuth(Username, pw)
	return r
}

func TestNudge_DefaultPageIsOffWithSuggestedText(t *testing.T) {
	// Arrange
	e := nudgeEnv(t, true)
	// Act
	res := serve(e.mux, hget("/admin/nudge?account=3"))
	// Assert
	body := res.Body.String()
	if res.Code != http.StatusOK {
		t.Fatalf("status %d", res.Code)
	}
	for _, want := range []string{`href="/admin/nudge" aria-current="page"`, `id="on" name="on" type="checkbox">`,
		`<option value="20" selected>`, `value="fixed" checked`, "ขอบคุณที่สนใจ", "This account uses the server default (off) until you save.",
		"The bot already told this customer a price in the chat."} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	// The wait stays below auto-resolve (22h): 21 is the last choice.
	if strings.Contains(body, `<option value="22"`) || !strings.Contains(body, `<option value="21"`) {
		t.Error("hour choices should stop at 21 with auto-resolve at 22h")
	}
}

func TestNudge_SaveAppliesAndIsAudited(t *testing.T) {
	// Arrange
	e := nudgeEnv(t, true)
	// Act
	res := serve(e.mux, npost(nudgeForm("3")))
	page := serve(e.mux, hget("/admin/nudge?account=3&done=saved")).Body.String()
	// Assert
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/admin/nudge?account=3&done=saved" {
		t.Fatalf("save = %d %q", res.Code, res.Header().Get("Location"))
	}
	want := nudge.Rule{On: true, After: 20 * time.Hour, Text: "สวัสดีครับคุณลูกค้า ทักมาได้เลยครับ", Write: true}
	if got := e.store.For(3); got != want {
		t.Fatalf("rule = %+v, want %+v", got, want)
	}
	if e.store.For(1).Active() {
		t.Error("saving account 3 switched on account 1")
	}
	if len(e.ev.events) != 1 || e.ev.events[0].AccountID != 3 ||
		e.ev.events[0].Summary != "off → on, 20h, AI-written; text changed" {
		t.Fatalf("audit = %+v", e.ev.events)
	}
	if !strings.Contains(page, "Saved. It applies from the next check") || !strings.Contains(page, `value="write" checked`) ||
		!strings.Contains(page, "This account uses its own setting.") {
		t.Error("page does not show the saved setting")
	}
}

func TestNudge_RejectsBadInputAndSavesNothing(t *testing.T) {
	cases := map[string]func(url.Values){
		"no text":          func(v url.Values) { v.Set("text", "  ") },
		"text too long":    func(v url.Values) { v.Set("text", strings.Repeat("ก", nudge.MaxText+1)) },
		"zero hours":       func(v url.Values) { v.Set("after_hours", "0") },
		"not before close": func(v url.Values) { v.Set("after_hours", "22") },
		"over 22 hours":    func(v url.Values) { v.Set("after_hours", "30") },
		"hours not number": func(v url.Values) { v.Set("after_hours", "20h") },
		"huge hours":       func(v url.Values) { v.Set("after_hours", "99999999999") },
		"unknown mode":     func(v url.Values) { v.Set("mode", "magic") },
		"no mode":          func(v url.Values) { v.Del("mode") },
		"script in text":   func(v url.Values) { v.Set("text", strings.Repeat("ก", nudge.MaxText)+"<script>") },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			e := nudgeEnv(t, true)
			form := nudgeForm("3")
			change(form)
			// Act
			res := serve(e.mux, npost(form))
			// Assert
			if res.Code != http.StatusUnprocessableEntity || e.store.Saved(3) || len(e.ev.events) != 0 {
				t.Fatalf("status %d saved=%v events=%d, want 422 and nothing saved", res.Code, e.store.Saved(3), len(e.ev.events))
			}
			if _, err := os.Stat(e.path); err == nil {
				t.Fatal("a rejected form wrote the settings file")
			}
			if strings.Contains(res.Body.String(), "<script>") {
				t.Fatal("input echoed unescaped")
			}
		})
	}
}

func TestNudge_RejectsBadAccountAndBigForm(t *testing.T) {
	for _, acct := range []string{"", "0", "-1", "abc"} {
		// Arrange
		e := nudgeEnv(t, true)
		// Act
		res := serve(e.mux, npost(nudgeForm(acct)))
		// Assert
		if res.Code != http.StatusBadRequest || len(e.ev.events) != 0 {
			t.Errorf("account %q: status %d", acct, res.Code)
		}
	}
	// Arrange
	e := nudgeEnv(t, true)
	big := nudgeForm("3")
	big.Set("pad", strings.Repeat("x", maxNudgeForm))
	// Act
	res := serve(e.mux, npost(big))
	// Assert
	if res.Code != http.StatusBadRequest || e.store.Saved(3) {
		t.Errorf("big form: status %d saved=%v, want 400 and nothing saved", res.Code, e.store.Saved(3))
	}
}

func TestNudge_NeedsAuthAndSameOrigin(t *testing.T) {
	// Arrange
	e := nudgeEnv(t, true)
	noAuth := npost(nudgeForm("3"))
	noAuth.Header.Del("Authorization")
	wrongPw := npost(nudgeForm("3"))
	wrongPw.SetBasicAuth(Username, "wrong")
	crossSite := npost(nudgeForm("3"))
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")
	getNoAuth := httptest.NewRequest(http.MethodGet, "/admin/nudge?account=3", nil)
	// Act
	a, b, c, d := serve(e.mux, noAuth), serve(e.mux, wrongPw), serve(e.mux, crossSite), serve(e.mux, getNoAuth)
	// Assert
	if a.Code != http.StatusUnauthorized || b.Code != http.StatusUnauthorized || c.Code != http.StatusForbidden || d.Code != http.StatusUnauthorized {
		t.Errorf("codes = %d %d %d %d, want 401 401 403 401", a.Code, b.Code, c.Code, d.Code)
	}
	if e.store.Saved(3) || len(e.ev.events) != 0 {
		t.Error("a rejected request saved or recorded a change")
	}
}

func TestNudge_WithoutStore(t *testing.T) {
	// Arrange
	e := nudgeEnv(t, false)
	// Act
	page := serve(e.mux, hget("/admin/nudge?account=1"))
	save := serve(e.mux, npost(nudgeForm("1")))
	// Assert
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Follow-up settings are off.") {
		t.Errorf("page = %d", page.Code)
	}
	if save.Code != http.StatusServiceUnavailable {
		t.Errorf("save = %d, want 503", save.Code)
	}
}
