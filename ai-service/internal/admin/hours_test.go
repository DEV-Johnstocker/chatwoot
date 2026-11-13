package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/schedule"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/settings"
)

type memHours struct {
	saved   map[int64]schedule.Schedule
	saves   int
	saveErr error
	getErr  error
}

func (m *memHours) Get(_ context.Context, acct int64) (schedule.Schedule, error) {
	if m.getErr != nil {
		return schedule.Schedule{}, m.getErr
	}
	if s, ok := m.saved[acct]; ok {
		return s, nil
	}
	return schedule.Default(acct), nil
}

func (m *memHours) Save(_ context.Context, s schedule.Schedule) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saves++
	s.UpdatedAt = time.Now()
	m.saved[s.AccountID] = s
	return nil
}

func hoursEnv(t *testing.T, withDB bool) (*http.ServeMux, *memHours) {
	t.Helper()
	store, err := settings.Open(t.TempDir()+"/s.json", settings.LLM{})
	if err != nil {
		t.Fatal(err)
	}
	mh := &memHours{saved: map[int64]schedule.Schedule{}}
	h := &Handler{Password: pw, Store: store, Switch: &llm.Switch{}, Timeout: time.Second,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	if withDB {
		h.Hours = mh
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	return mux, mh
}

// hoursForm is a full form: every day 09:00–18:00, ticked.
func hoursForm(acct, mode string) url.Values {
	v := url.Values{"account": {acct}, "mode": {mode}, "time_zone": {"Asia/Bangkok"}}
	for i := 0; i < 7; i++ {
		v.Set(fmt.Sprintf("day%d_on", i), "on")
		v.Set(fmt.Sprintf("day%d_from", i), "09:00")
		v.Set(fmt.Sprintf("day%d_to", i), "18:00")
	}
	return v
}

func hpost(v url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/admin/hours", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.SetBasicAuth(Username, pw)
	return r
}

func hget(path string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.SetBasicAuth(Username, pw)
	return r
}

func TestHours_DefaultPageShowsBotOn(t *testing.T) {
	// Arrange
	mux, _ := hoursEnv(t, true)

	// Act
	res := serve(mux, hget("/admin/hours?account=3"))

	// Assert
	body := res.Body.String()
	if res.Code != http.StatusOK {
		t.Fatalf("status %d", res.Code)
	}
	for _, want := range []string{`value="on" checked`, "Right now the bot is answering.", `name="day1_from" value="09:00"`, `href="/admin/hours" aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestHours_SaveScheduledOvernight(t *testing.T) {
	// Arrange
	mux, mh := hoursEnv(t, true)
	form := hoursForm("2", "scheduled")
	form.Set("day5_from", "18:00") // Friday night
	form.Set("day5_to", "09:00")
	form.Del("day0_on") // Sunday unticked

	// Act
	res := serve(mux, hpost(form))
	page := serve(mux, hget("/admin/hours?account=2&done=saved")).Body.String()

	// Assert
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/admin/hours?account=2&done=saved" {
		t.Fatalf("save = %d %q", res.Code, res.Header().Get("Location"))
	}
	got := mh.saved[2]
	if got.Mode != schedule.ModeScheduled || got.Days[5] != (schedule.Day{On: true, From: 18 * 60, To: 9 * 60}) || got.Days[0].On {
		t.Errorf("saved %+v", got)
	}
	if !strings.Contains(page, "Saved. It applies from the next customer message.") || !strings.Contains(page, "next day") {
		t.Error("page lacks the saved notice or the overnight mark")
	}
}

func TestHours_SaveOffShowsBotOff(t *testing.T) {
	// Arrange
	mux, _ := hoursEnv(t, true)

	// Act
	serve(mux, hpost(hoursForm("1", "off")))
	page := serve(mux, hget("/admin/hours?account=1")).Body.String()

	// Assert
	if !strings.Contains(page, "Right now the bot is off.") || !strings.Contains(page, `value="off" checked`) {
		t.Error("page does not show the bot as off")
	}
}

func TestHours_RejectsBadInputAndSavesNothing(t *testing.T) {
	cases := map[string]func(url.Values){
		"unknown mode":   func(v url.Values) { v.Set("mode", "sometimes") },
		"bad time":       func(v url.Values) { v.Set("day3_from", "25:00") },
		"missing time":   func(v url.Values) { v.Del("day4_to") },
		"bad time zone":  func(v url.Values) { v.Set("time_zone", "Mars/Olympus") },
		"script in zone": func(v url.Values) { v.Set("time_zone", "<script>") },
	}
	for name, change := range cases {
		// Arrange
		mux, mh := hoursEnv(t, true)
		form := hoursForm("1", "scheduled")
		change(form)

		// Act
		res := serve(mux, hpost(form))

		// Assert
		if res.Code != http.StatusUnprocessableEntity || mh.saves != 0 {
			t.Errorf("%s: status %d, saves %d", name, res.Code, mh.saves)
		}
		if strings.Contains(res.Body.String(), "<script>") {
			t.Errorf("%s: input echoed unescaped", name)
		}
	}
}

func TestHours_RejectsBadAccount(t *testing.T) {
	for _, acct := range []string{"", "0", "-1", "abc"} {
		// Arrange
		mux, mh := hoursEnv(t, true)

		// Act
		res := serve(mux, hpost(hoursForm(acct, "off")))

		// Assert
		if res.Code != http.StatusBadRequest || mh.saves != 0 {
			t.Errorf("account %q: status %d, saves %d", acct, res.Code, mh.saves)
		}
	}
}

func TestHours_NeedsAuthAndSameOrigin(t *testing.T) {
	// Arrange
	mux, mh := hoursEnv(t, true)
	noAuth := hpost(hoursForm("1", "off"))
	noAuth.Header.Del("Authorization")
	wrongPw := hpost(hoursForm("1", "off"))
	wrongPw.SetBasicAuth(Username, "wrong")
	crossSite := hpost(hoursForm("1", "off"))
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")

	// Act
	a, b, c := serve(mux, noAuth), serve(mux, wrongPw), serve(mux, crossSite)

	// Assert
	if a.Code != http.StatusUnauthorized || b.Code != http.StatusUnauthorized || c.Code != http.StatusForbidden {
		t.Errorf("codes = %d %d %d, want 401 401 403", a.Code, b.Code, c.Code)
	}
	if mh.saves != 0 {
		t.Errorf("a rejected request saved %d times", mh.saves)
	}
}

func TestHours_WithoutDatabase(t *testing.T) {
	// Arrange
	mux, _ := hoursEnv(t, false)

	// Act
	page := serve(mux, hget("/admin/hours"))
	save := serve(mux, hpost(hoursForm("1", "off")))

	// Assert
	if !strings.Contains(page.Body.String(), "The bot switch is off.") || save.Code != http.StatusServiceUnavailable {
		t.Errorf("page %d, save %d", page.Code, save.Code)
	}
}

func TestHours_InThai(t *testing.T) {
	// Arrange
	mux, _ := hoursEnv(t, true)
	r := hget("/admin/hours")
	r.Header.Set("Accept-Language", "th")

	// Act
	body := serve(mux, r).Body.String()

	// Assert
	for _, want := range []string{"เปิด/ปิดบอต · Pronto", "เปิดตามเวลาที่ตั้ง", "วันจันทร์", "ตอนนี้บอตกำลังตอบลูกค้า"} {
		if !strings.Contains(body, want) {
			t.Errorf("Thai page lacks %q", want)
		}
	}
	for _, en := range []string{"Bot hours", "Answers at any time.", "Monday"} {
		if strings.Contains(body, en) {
			t.Errorf("Thai page still says %q", en)
		}
	}
}

func TestHoursSummary(t *testing.T) {
	// Arrange
	s := schedule.Default(1)
	s.Mode = schedule.ModeScheduled
	for i := range s.Days {
		s.Days[i].On = false
	}
	s.Days[time.Friday] = schedule.Day{On: true, From: 18 * 60, To: 9 * 60}

	// Act / Assert
	if got := hoursSummary(s); got != "scheduled (Asia/Bangkok): Fri 18:00–09:00" {
		t.Errorf("summary = %q", got)
	}
	if got := hoursSummary(schedule.Default(1)); got != "on" {
		t.Errorf("summary = %q", got)
	}
}

func TestHours_DatabaseErrorIsNotShownRaw(t *testing.T) {
	// Arrange
	mux, mh := hoursEnv(t, true)
	mh.saveErr = errors.New(`pq: connection to 10.0.0.5 refused`)

	// Act
	res := serve(mux, hpost(hoursForm("1", "off")))

	// Assert
	body := res.Body.String()
	if res.Code != http.StatusInternalServerError || strings.Contains(body, "10.0.0.5") || !strings.Contains(body, "Could not save the bot switch.") {
		t.Errorf("status %d, body shows raw error or lacks the notice", res.Code)
	}
}
