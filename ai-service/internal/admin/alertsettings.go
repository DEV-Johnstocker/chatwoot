package admin

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/alerts"
)

// The Alerts page: the owner's per-customer reply limit for an account, which
// bot alerts are on, and when a hand-off spike counts. The thresholds for
// errors are fixed in code and shown read-only; the Slack link stays a
// server secret and shows only as set or not set. A save applies at once.
// Issue: DEV-Johnstocker/chatwoot#1

// AlertSettings stores the page's values (alerts.SettingsStore).
type AlertSettings interface {
	Get() alerts.Settings
	ReplyLimitFor(acct int64) alerts.Limit
	DefaultReplyLimit() alerts.Limit
	Save(alerts.Settings) error
}

// maxAlertForm bounds the form: a few numbers and five switches.
const maxAlertForm = 4 << 10

// replyWindows are offered on the page; a saved window outside the list is added.
var replyWindows = []time.Duration{15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour,
	3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour}

// alertRule is one row of the alerts table.
type alertRule struct {
	Kind  string
	Title string
	When  string // the fixed rule, or "" for the spike (edited below the table)
	On    bool
}

type windowOption struct {
	Minutes  int
	Label    string
	Selected bool
}

type alertSettingsData struct {
	Enabled    bool // a settings store is configured
	AccountID  int64
	SlackSet   bool
	LimitOn    bool
	LimitMax   int
	Windows    []windowOption
	Default    string // the server default limit, in words
	Custom     bool   // this account saved its own limit
	Rules      []alertRule
	SharePct   int
	MinTurns   int
	MinTurnsLo int
	MinTurnsHi int
	MaxLimit   int
	Saved      bool
	Error      string
}

func (h *Handler) showAlertSettings(w http.ResponseWriter, r *http.Request) {
	acct := accountParam(r.URL.Query().Get("account"))
	data := alertSettingsData{Enabled: h.AlertSettings != nil, Saved: r.URL.Query().Get("done") == "saved"}
	if h.AlertSettings == nil {
		h.renderAlertSettings(w, r, http.StatusOK, acct, alerts.Settings{}, alerts.Limit{}, data)
		return
	}
	s := h.AlertSettings.Get()
	_, data.Custom = s.ReplyLimits[acct]
	h.renderAlertSettings(w, r, http.StatusOK, acct, s, h.AlertSettings.ReplyLimitFor(acct), data)
}

func (h *Handler) submitAlertSettings(w http.ResponseWriter, r *http.Request) {
	if h.AlertSettings == nil {
		http.Error(w, "alert settings disabled", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAlertForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "form too large or invalid", http.StatusBadRequest)
		return
	}
	f := r.PostForm
	// Parse strictly: falling back to account 1 here would change its limit.
	acct, err := strconv.ParseInt(strings.TrimSpace(f.Get("account")), 10, 64)
	if err != nil || acct < 1 {
		http.Error(w, "account must be a positive number", http.StatusBadRequest)
		return
	}
	old := h.AlertSettings.Get()
	oldLimit := h.AlertSettings.ReplyLimitFor(acct)
	next, limit, err := alertSettingsFromForm(old, acct, f.Get)
	if err == nil {
		err = h.AlertSettings.Save(next)
	}
	if err != nil {
		h.Logger.Warn("alert settings not saved", "account_id", acct, "error", err)
		data := alertSettingsData{Enabled: true, Error: err.Error()}
		_, data.Custom = old.ReplyLimits[acct]
		h.renderAlertSettings(w, r, http.StatusUnprocessableEntity, acct, next, limit, data)
		return
	}
	if limit != oldLimit {
		h.record(r, acct, "reply limit saved", oldLimit.String()+" → "+limit.String(), nil)
	}
	if was, now := serverRules(old), serverRules(next); was != now {
		h.record(r, 0, "alert settings saved", was+" → "+now, nil)
	}
	h.Logger.Info("alert settings saved", "account_id", acct, "reply_limit", limit.String(), "remote_addr", r.RemoteAddr)
	http.Redirect(w, r, "/admin/alerts?account="+strconv.FormatInt(acct, 10)+"&done=saved", http.StatusSeeOther)
}

// alertSettingsFromForm reads the form over the saved settings. It returns
// what the form asked for even when a value is out of range, so the page can
// show it back next to the error.
func alertSettingsFromForm(old alerts.Settings, acct int64, get func(string) string) (alerts.Settings, alerts.Limit, error) {
	next := old
	next.ReplyLimits = map[int64]alerts.Limit{}
	for k, v := range old.ReplyLimits {
		next.ReplyLimits[k] = v
	}
	var errs []error
	limit := alerts.Limit{Off: get("limit_on") != "on"}
	if n, err := strconv.Atoi(strings.TrimSpace(get("limit_max"))); err == nil {
		limit.Max = n
	} else if !limit.Off {
		errs = append(errs, errors.New("replies per customer must be a whole number"))
	}
	if m, err := strconv.Atoi(strings.TrimSpace(get("limit_window"))); err == nil {
		limit.Window = time.Duration(m) * time.Minute
	} else {
		errs = append(errs, errors.New("pick a time window for the reply limit"))
	}
	next.ReplyLimits[acct] = limit

	next.Off = nil
	for _, k := range alertKinds() {
		if get("alert_"+string(k)) != "on" {
			next.Off = append(next.Off, k)
		}
	}
	if pct, err := strconv.ParseFloat(strings.TrimSpace(get("spike_share")), 64); err == nil && !math.IsNaN(pct) {
		next.SpikeShare = pct / 100
	} else {
		errs = append(errs, errors.New("the hand-off share must be a number"))
	}
	if n, err := strconv.Atoi(strings.TrimSpace(get("spike_min"))); err == nil {
		next.SpikeMinTurns = n
	} else {
		errs = append(errs, errors.New("the minimum answered messages must be a whole number"))
	}
	if len(errs) == 0 {
		if err := next.Validate(); err != nil {
			errs = append(errs, err)
		}
	}
	return next, limit, errors.Join(errs...)
}

// alertKinds lists the kinds in page order.
func alertKinds() []alerts.Kind {
	return []alerts.Kind{alerts.SendFailed, alerts.LLMError, alerts.QueueFull, alerts.HandoffSpike, alerts.ReplyLimit}
}

// serverRules is the audit form of the server-wide part, e.g.
// "spike 50% of 10; off: queue_full".
func serverRules(s alerts.Settings) string {
	out := fmt.Sprintf("spike %d%% of %d", int(math.Round(s.SpikeShare*100)), s.SpikeMinTurns)
	if len(s.Off) > 0 {
		names := make([]string, len(s.Off))
		for i, k := range s.Off {
			names[i] = string(k)
		}
		slices.Sort(names)
		out += "; off: " + strings.Join(names, ", ")
	}
	return out
}

// fixedRules says when each error alert fires, in the page's language.
func fixedRules(lang string) map[alerts.Kind]string {
	mins := int(alerts.Window.Minutes())
	return map[alerts.Kind]string{
		alerts.SendFailed: tr(lang, "%d or more in %d minutes", alerts.MinEvents(alerts.SendFailed), mins),
		alerts.LLMError:   tr(lang, "%d or more in %d minutes", alerts.MinEvents(alerts.LLMError), mins),
		alerts.QueueFull:  tr(lang, "Any, at once"),
		alerts.ReplyLimit: tr(lang, "Any, at once"),
	}
}

func (h *Handler) renderAlertSettings(w http.ResponseWriter, r *http.Request, status int, acct int64, s alerts.Settings, limit alerts.Limit, data alertSettingsData) {
	lang := langOf(r)
	data.AccountID, data.SlackSet, data.MaxLimit = acct, h.SlackConfigured, alerts.MaxReplyLimit
	data.MinTurnsLo, data.MinTurnsHi = alerts.MinSpikeTurns, alerts.MaxSpikeTurns
	data.LimitOn, data.LimitMax = limit.On(), limit.Max
	if data.LimitMax == 0 {
		data.LimitMax = 30 // REPLY_LIMIT=0 default: suggest a number for switching it on
	}
	if h.AlertSettings != nil {
		data.Default = limitWords(lang, h.AlertSettings.DefaultReplyLimit())
	}
	windows := slices.Clone(replyWindows)
	if limit.Window > 0 && !slices.Contains(windows, limit.Window) {
		windows = append(windows, limit.Window)
		slices.Sort(windows)
	}
	pick := limit.Window
	if pick == 0 {
		pick = time.Hour
	}
	for _, d := range windows {
		data.Windows = append(data.Windows, windowOption{Minutes: int(d / time.Minute), Label: windowWords(lang, d), Selected: d == pick})
	}
	fixed := fixedRules(lang)
	for _, k := range alertKinds() {
		data.Rules = append(data.Rules, alertRule{Kind: string(k), Title: tr(lang, alerts.Titles[k]), When: fixed[k], On: s.Enabled(k)})
	}
	data.SharePct, data.MinTurns = int(math.Round(s.SpikeShare*100)), s.SpikeMinTurns
	h.renderPage(w, r, status, "alertsettings.html", data)
}

// windowWords names a window: "15 minutes", "1 hour", "24 hours".
func windowWords(lang string, d time.Duration) string {
	if d%time.Hour == 0 {
		if d == time.Hour {
			return tr(lang, "1 hour")
		}
		return tr(lang, "%d hours", int(d/time.Hour))
	}
	return tr(lang, "%d minutes", int(d/time.Minute))
}

// limitWords says a limit in words: "30 replies per 1 hour" or "off".
func limitWords(lang string, l alerts.Limit) string {
	if !l.On() {
		return tr(lang, "off")
	}
	return tr(lang, "%d replies per %s", l.Max, windowWords(lang, l.Window))
}
