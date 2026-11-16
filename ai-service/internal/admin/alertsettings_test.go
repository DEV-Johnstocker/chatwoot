package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/alerts"
)

type alertEnv struct {
	mux    *http.ServeMux
	store  *alerts.SettingsStore
	events *memEvents
	path   string
}

func newAlertEnv(t *testing.T, slack bool) *alertEnv {
	t.Helper()
	path := filepath.Join(t.TempDir(), "alert-settings.json")
	store, err := alerts.OpenSettings(path, alerts.Defaults{SpikeShare: 0.5, ReplyLimit: alerts.Limit{Max: 30, Window: time.Hour}})
	if err != nil {
		t.Fatal(err)
	}
	ev := &memEvents{}
	h := &Handler{AlertSettings: store, SlackConfigured: slack, Events: ev}
	return &alertEnv{mux: newHandlerMux(t, h), store: store, events: ev, path: path}
}

func (e *alertEnv) do(r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, r)
	return rec
}

func alertForm() url.Values {
	return url.Values{
		"account": {"3"}, "limit_on": {"on"}, "limit_max": {"10"}, "limit_window": {"30"},
		"alert_send_failed": {"on"}, "alert_llm_error": {"on"}, "alert_handoff_spike": {"on"}, "alert_reply_limit": {"on"},
		"spike_share": {"60"}, "spike_min": {"20"},
	}
}

func alertPost(form url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/admin/alerts", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.SetBasicAuth(Username, pw)
	return r
}

func TestAlertSettings_ShowsDefaultsAndSlackState(t *testing.T) {
	// Arrange
	e := newAlertEnv(t, false)
	r := httptest.NewRequest(http.MethodGet, "/admin/alerts?account=3", nil)
	r.SetBasicAuth(Username, pw)

	// Act
	res := e.do(r)

	// Assert
	body := res.Body.String()
	if res.Code != http.StatusOK {
		t.Fatalf("code %d", res.Code)
	}
	for _, want := range []string{`name="limit_max" type="number" min="1" max="1000" step="1" inputmode="numeric" value="30"`,
		"Slack: not set", "uses the server default (30 replies per 1 hour)", "3 or more in 15 minutes", `value="50"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestAlertSettings_SaveAppliesAtOnceAndIsAudited(t *testing.T) {
	// Arrange: queue_full left unticked
	e := newAlertEnv(t, true)

	// Act
	res := e.do(alertPost(alertForm()))

	// Assert
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/admin/alerts?account=3&done=saved" {
		t.Fatalf("code %d location %q", res.Code, res.Header().Get("Location"))
	}
	if got := e.store.ReplyLimitFor(3); got != (alerts.Limit{Max: 10, Window: 30 * time.Minute}) {
		t.Errorf("account 3 limit = %+v", got)
	}
	if got := e.store.ReplyLimitFor(1); got.Max != 30 {
		t.Errorf("account 1 changed: %+v", got)
	}
	if share, turns := e.store.Spike(); share != 0.6 || turns != 20 || e.store.Enabled(alerts.QueueFull) {
		t.Errorf("spike %v/%d, queue alert on %v", share, turns, e.store.Enabled(alerts.QueueFull))
	}
	if len(e.events.events) != 2 {
		t.Fatalf("events %+v, want the account limit and the server rules", e.events.events)
	}
	acct, server := e.events.events[0], e.events.events[1]
	if acct.AccountID != 3 || acct.Summary != "30 per 1h0m0s → 10 per 30m0s" {
		t.Errorf("account event %+v", acct)
	}
	if server.AccountID != 0 || server.Summary != "spike 50% of 10 → spike 60% of 20; off: queue_full" {
		t.Errorf("server event %+v", server)
	}
}

func TestAlertSettings_SwitchOffKeepsNumbers(t *testing.T) {
	// Arrange
	e := newAlertEnv(t, false)
	f := alertForm()
	f.Del("limit_on")

	// Act
	res := e.do(alertPost(f))

	// Assert
	got := e.store.ReplyLimitFor(3)
	if res.Code != http.StatusSeeOther || got.On() || got.Max != 10 || got.Window != 30*time.Minute {
		t.Errorf("code %d limit %+v", res.Code, got)
	}
}

func TestAlertSettings_RejectsBadValuesAndChangesNothing(t *testing.T) {
	cases := map[string]func(url.Values){
		"limit zero":        func(f url.Values) { f.Set("limit_max", "0") },
		"limit too high":    func(f url.Values) { f.Set("limit_max", "1001") },
		"limit not number":  func(f url.Values) { f.Set("limit_max", "lots") },
		"window too long":   func(f url.Values) { f.Set("limit_window", "1500") },
		"window missing":    func(f url.Values) { f.Del("limit_window") },
		"share too low":     func(f url.Values) { f.Set("spike_share", "5") },
		"share over 100":    func(f url.Values) { f.Set("spike_share", "150") },
		"share NaN":         func(f url.Values) { f.Set("spike_share", "NaN") },
		"min turns too low": func(f url.Values) { f.Set("spike_min", "1") },
		"min turns text":    func(f url.Values) { f.Set("spike_min", "ten") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			e := newAlertEnv(t, false)
			f := alertForm()
			mutate(f)

			// Act
			res := e.do(alertPost(f))

			// Assert
			if res.Code != http.StatusUnprocessableEntity || !strings.Contains(res.Body.String(), "Not saved.") {
				t.Errorf("code %d", res.Code)
			}
			assertAlertsUnchanged(t, e)
		})
	}
}

func TestAlertSettings_PostNeedsAuthSameOriginAndAnAccount(t *testing.T) {
	cases := map[string]struct {
		req  func() *http.Request
		code int
	}{
		"no password": {func() *http.Request {
			r := alertPost(alertForm())
			r.Header.Del("Authorization")
			return r
		}, http.StatusUnauthorized},
		"wrong password": {func() *http.Request {
			r := alertPost(alertForm())
			r.SetBasicAuth(Username, "wrong")
			return r
		}, http.StatusUnauthorized},
		"cross-site (CSRF)": {func() *http.Request {
			r := alertPost(alertForm())
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			return r
		}, http.StatusForbidden},
		"no account": {func() *http.Request {
			f := alertForm()
			f.Del("account")
			return alertPost(f)
		}, http.StatusBadRequest},
		"account zero": {func() *http.Request {
			f := alertForm()
			f.Set("account", "0")
			return alertPost(f)
		}, http.StatusBadRequest},
		"form too large": {func() *http.Request {
			f := alertForm()
			f.Set("pad", strings.Repeat("x", 8<<10))
			return alertPost(f)
		}, http.StatusBadRequest},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			e := newAlertEnv(t, false)

			// Act
			res := e.do(tc.req())

			// Assert
			if res.Code != tc.code {
				t.Errorf("code %d, want %d", res.Code, tc.code)
			}
			assertAlertsUnchanged(t, e)
		})
	}
}

func TestAlertSettings_PageIsTranslated(t *testing.T) {
	// Arrange
	e := newAlertEnv(t, true)
	r := httptest.NewRequest(http.MethodGet, "/admin/alerts?account=1", nil)
	r.SetBasicAuth(Username, pw)
	r.AddCookie(&http.Cookie{Name: langCookie, Value: langTH})

	// Act
	body := e.do(r).Body.String()

	// Assert
	for _, want := range []string{"การแจ้งเตือน", "Slack: ตั้งค่าแล้ว", "30 ครั้งต่อ 1 ชั่วโมง", "ตั้งแต่ 3 ครั้งใน 15 นาที"} {
		if !strings.Contains(body, want) {
			t.Errorf("Thai page lacks %q", want)
		}
	}
}

func assertAlertsUnchanged(t *testing.T, e *alertEnv) {
	t.Helper()
	if got := e.store.ReplyLimitFor(3); got != (alerts.Limit{Max: 30, Window: time.Hour}) {
		t.Errorf("limit changed: %+v", got)
	}
	if share, turns := e.store.Spike(); share != 0.5 || turns != 10 || !e.store.Enabled(alerts.QueueFull) {
		t.Errorf("rules changed: %v/%d", share, turns)
	}
	if _, err := os.Stat(e.path); !os.IsNotExist(err) {
		t.Error("the settings file was written")
	}
	if len(e.events.events) != 0 {
		t.Errorf("audit events %+v", e.events.events)
	}
}
