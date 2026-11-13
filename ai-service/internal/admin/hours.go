package admin

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/schedule"
)

// The bot on/off page: one switch per account (on, off, or on during weekly
// hours). Off sends new customer chats straight to staff in Chatwoot.
// Issue: DEV-Johnstocker/chatwoot#1

// maxHoursForm bounds the on/off form: a mode, a time zone and 21 day fields.
const maxHoursForm = 8 << 10

// timeZones are offered on the page; the account's saved zone is added when
// it is not one of them.
var timeZones = []string{"Asia/Bangkok", "Asia/Ho_Chi_Minh", "Asia/Jakarta", "Asia/Kuala_Lumpur", "Asia/Singapore", "Asia/Manila", "Asia/Tokyo", "UTC"}

// weekOrder lists the days Monday first, as people in Thailand read a week.
var weekOrder = []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday}

type hourRow struct {
	Index     int
	Name      string
	On        bool
	From, To  string
	Overnight bool
}

type hoursData struct {
	Enabled   bool // a database is configured
	AccountID int64
	Mode      schedule.Mode
	TimeZone  string
	Zones     []string
	Days      []hourRow
	ActiveNow bool
	// StateUnknown: after a failed save the saved switch could not be read.
	StateUnknown bool
	stateSet     bool   // ActiveNow already holds the saved state
	Now          string // the current time in the schedule's zone
	Updated      string
	Saved        bool
	Error        string
}

func (h *Handler) showHours(w http.ResponseWriter, r *http.Request) {
	acct := accountParam(r.URL.Query().Get("account"))
	s := schedule.Default(acct)
	data := hoursData{Enabled: h.Hours != nil, Saved: r.URL.Query().Get("done") == "saved"}
	if h.Hours != nil {
		got, err := h.Hours.Get(r.Context(), acct)
		if err != nil {
			h.Logger.Error("load bot hours", "account_id", acct, "error", err)
			data.Error = tr(langOf(r), "Could not load the bot switch. Check the database connection.")
		} else {
			s = got
		}
	}
	h.renderHours(w, r, http.StatusOK, s, data)
}

func (h *Handler) submitHours(w http.ResponseWriter, r *http.Request) {
	if h.Hours == nil {
		http.Error(w, "bot switch disabled: set DATABASE_URL", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxHoursForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "form too large or invalid", http.StatusBadRequest)
		return
	}
	f := r.PostForm
	// Parse strictly: falling back to account 1 here would switch its bot.
	acct, err := strconv.ParseInt(strings.TrimSpace(f.Get("account")), 10, 64)
	if err != nil || acct < 1 {
		http.Error(w, "account must be a positive number", http.StatusBadRequest)
		return
	}
	s, err := hoursFromForm(acct, f.Get)
	if err != nil {
		h.renderRejected(w, r, http.StatusUnprocessableEntity, s, err.Error())
		return
	}
	if err := h.Hours.Save(r.Context(), s); err != nil {
		// The form was valid, so this is the database: keep its text in the log.
		h.Logger.Error("save bot hours failed", "account_id", acct, "error", err)
		h.renderRejected(w, r, http.StatusInternalServerError, s,
			tr(langOf(r), "Could not save the bot switch. Check the database connection."))
		return
	}
	h.record(r, acct, "bot switch saved", hoursSummary(s), nil)
	h.Logger.Info("bot switch saved", "account_id", acct, "mode", string(s.Mode), "remote_addr", r.RemoteAddr)
	http.Redirect(w, r, "/admin/hours?account="+strconv.FormatInt(acct, 10)+"&done=saved", http.StatusSeeOther)
}

// renderRejected shows a form that was not saved: the fields keep what the
// owner typed, but the status bar shows what is saved (and in force), never
// the rejected form.
func (h *Handler) renderRejected(w http.ResponseWriter, r *http.Request, status int, form schedule.Schedule, msg string) {
	data := hoursData{Enabled: true, Error: msg}
	saved, err := h.Hours.Get(r.Context(), form.AccountID)
	if err != nil {
		data.StateUnknown = true
	} else {
		data.ActiveNow, data.stateSet = saved.Active(time.Now()), true
	}
	h.renderHours(w, r, status, form, data)
}

// hoursFromForm reads the switch and the seven days. A day left unticked
// keeps its times, so ticking it again later restores them.
func hoursFromForm(acct int64, get func(string) string) (schedule.Schedule, error) {
	s := schedule.Schedule{AccountID: acct, Mode: schedule.Mode(get("mode")), TimeZone: strings.TrimSpace(get("time_zone"))}
	var errs []string
	for i := range s.Days {
		d := schedule.Day{On: get(fmt.Sprintf("day%d_on", i)) == "on"}
		var e1, e2 error
		d.From, e1 = schedule.ParseClock(get(fmt.Sprintf("day%d_from", i)))
		d.To, e2 = schedule.ParseClock(get(fmt.Sprintf("day%d_to", i)))
		if e1 != nil || e2 != nil {
			if !d.On {
				// An unticked day's times are unused: keep the usual ones.
				d.From, d.To = schedule.Default(acct).Days[i].From, schedule.Default(acct).Days[i].To
				s.Days[i] = d
				continue
			}
			errs = append(errs, fmt.Sprintf("%s: times must look like 09:00", time.Weekday(i)))
		}
		s.Days[i] = d
	}
	if len(errs) > 0 {
		return s, fmt.Errorf("%s", strings.Join(errs, "\n"))
	}
	return s, s.Validate()
}

// hoursSummary is the audit line: the mode, and the hours when scheduled.
func hoursSummary(s schedule.Schedule) string {
	if s.Mode != schedule.ModeScheduled {
		return string(s.Mode)
	}
	var parts []string
	for _, wd := range weekOrder {
		d := s.Days[wd]
		if d.On {
			parts = append(parts, wd.String()[:3]+" "+schedule.Clock(d.From)+"–"+schedule.Clock(d.To))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "no days")
	}
	return "scheduled (" + s.TimeZone + "): " + strings.Join(parts, ", ")
}

func (h *Handler) renderHours(w http.ResponseWriter, r *http.Request, status int, s schedule.Schedule, data hoursData) {
	lang := langOf(r)
	data.AccountID, data.Mode, data.TimeZone = s.AccountID, s.Mode, s.TimeZone
	data.Zones = slices.Clone(timeZones)
	if s.TimeZone != "" && !slices.Contains(data.Zones, s.TimeZone) {
		data.Zones = append(data.Zones, s.TimeZone)
	}
	for _, wd := range weekOrder {
		d := s.Days[wd]
		data.Days = append(data.Days, hourRow{Index: int(wd), Name: tr(lang, wd.String()), On: d.On,
			From: schedule.Clock(d.From), To: schedule.Clock(d.To), Overnight: d.From > d.To})
	}
	now := time.Now()
	if !data.stateSet && !data.StateUnknown {
		data.ActiveNow = s.Active(now)
	}
	if loc, err := time.LoadLocation(s.TimeZone); err == nil {
		data.Now = tr(lang, now.In(loc).Weekday().String()) + " " + now.In(loc).Format("15:04")
	}
	if !s.UpdatedAt.IsZero() {
		data.Updated = h.whenIn(lang, s.UpdatedAt)
	}
	h.renderPage(w, r, status, "hours.html", data)
}
