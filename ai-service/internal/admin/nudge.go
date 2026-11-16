package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/nudge"
)

// The Follow-up page: one follow-up to a customer who went quiet after the
// bot gave a price. Per account the owner sets it on or off, how long to
// wait, the fixed text, and whether the AI writes it from the chat (the
// fixed text is then the fallback). The safety rules stay fixed in code and
// are shown read-only. A save applies at the next sweep, with no restart.
// Issue: DEV-Johnstocker/chatwoot#1

// NudgeSettings stores the page's values (nudge.SettingsStore).
type NudgeSettings interface {
	For(acct int64) nudge.Rule
	Saved(acct int64) bool
	Defaults() nudge.Defaults
	Save(acct int64, r nudge.Rule) error
}

// maxNudgeForm bounds the form: a switch, a number, a choice and the text
// (1000 Thai characters are 3000 bytes, URL-encoded about 9000).
const maxNudgeForm = 16 << 10

type hourOption struct {
	Hours    int
	Selected bool
}

type nudgeData struct {
	Enabled   bool // a settings store is configured
	Ready     bool // the reader token is set, so the sweeper runs
	AccountID int64
	On        bool
	Write     bool
	Text      string
	TextLen   int
	MaxText   int
	Hours     []hourOption
	Resolve   int  // auto-resolve hours, 0 = off
	Custom    bool // this account saved its own setting
	Default   string
	Saved     bool
	Error     string
}

func (h *Handler) showNudge(w http.ResponseWriter, r *http.Request) {
	acct := accountParam(r.URL.Query().Get("account"))
	data := nudgeData{Enabled: h.Nudge != nil, Saved: r.URL.Query().Get("done") == "saved"}
	var rule nudge.Rule
	if h.Nudge != nil {
		rule = h.Nudge.For(acct)
		data.Custom = h.Nudge.Saved(acct)
	}
	h.renderNudge(w, r, http.StatusOK, acct, rule, data)
}

func (h *Handler) submitNudge(w http.ResponseWriter, r *http.Request) {
	if h.Nudge == nil {
		http.Error(w, "follow-up settings disabled", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxNudgeForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "form too large or invalid", http.StatusBadRequest)
		return
	}
	f := r.PostForm
	// Parse strictly: falling back to account 1 here would change its follow-up.
	acct, err := strconv.ParseInt(strings.TrimSpace(f.Get("account")), 10, 64)
	if err != nil || acct < 1 {
		http.Error(w, "account must be a positive number", http.StatusBadRequest)
		return
	}
	old := h.Nudge.For(acct)
	next, err := nudgeFromForm(f.Get)
	if err == nil {
		err = h.Nudge.Save(acct, next)
	}
	if err != nil {
		h.Logger.Warn("follow-up settings not saved", "account_id", acct, "error", err)
		h.renderNudge(w, r, http.StatusUnprocessableEntity, acct, next,
			nudgeData{Enabled: true, Error: err.Error(), Custom: h.Nudge.Saved(acct)})
		return
	}
	saved := h.Nudge.For(acct)
	summary := old.String() + " → " + saved.String()
	if old.Text != saved.Text {
		summary += "; text changed"
	}
	h.record(r, acct, "follow-up settings saved", summary, map[string]any{"rule": old.String(), "text": old.Text})
	h.Logger.Info("follow-up settings saved", "account_id", acct, "rule", saved.String(), "remote_addr", r.RemoteAddr)
	http.Redirect(w, r, "/admin/nudge?account="+strconv.FormatInt(acct, 10)+"&done=saved", http.StatusSeeOther)
}

// nudgeFromForm reads the form. It returns what the form asked for even when
// a value is wrong, so the page can show it back next to the error.
func nudgeFromForm(get func(string) string) (nudge.Rule, error) {
	r := nudge.Rule{On: get("on") == "on", Text: nudge.CleanText(get("text"))}
	var errs []error
	switch get("mode") {
	case "write":
		r.Write = true
	case "fixed":
	default:
		errs = append(errs, errors.New("choose who writes the follow-up"))
	}
	if n, err := strconv.Atoi(strings.TrimSpace(get("after_hours"))); err == nil && n > 0 && n <= 1000 {
		r.After = time.Duration(n) * time.Hour
	} else {
		errs = append(errs, errors.New("pick how many hours to wait"))
	}
	return r, errors.Join(errs...)
}

func (h *Handler) renderNudge(w http.ResponseWriter, r *http.Request, status int, acct int64, rule nudge.Rule, data nudgeData) {
	lang := langOf(r)
	data.AccountID, data.Ready, data.MaxText = acct, h.NudgeReady, nudge.MaxText
	data.On, data.Write, data.Text = rule.On, rule.Write, rule.Text
	if strings.TrimSpace(data.Text) == "" && data.Error == "" {
		data.Text = nudge.SuggestedText
	}
	data.TextLen = utf8.RuneCountInString(data.Text)
	maxHours := int(nudge.MaxAfter / time.Hour)
	if h.Nudge != nil {
		d := h.Nudge.Defaults()
		data.Default = nudgeWords(lang, d.For(acct))
		if d.ResolveAfter > 0 {
			data.Resolve = int(d.ResolveAfter / time.Hour)
			maxHours = min(maxHours, int((d.ResolveAfter-time.Nanosecond)/time.Hour))
		}
	}
	pick := int(rule.After / time.Hour)
	if pick < 1 {
		pick = 20 // the owner's choice for the first version (2026-11-14)
	}
	pick = min(pick, max(maxHours, 1))
	for n := 1; n <= maxHours; n++ {
		data.Hours = append(data.Hours, hourOption{Hours: n, Selected: n == pick})
	}
	h.renderPage(w, r, status, "nudge.html", data)
}

// nudgeWords says a rule in words: "on, after 20 hours, written by AI" or "off".
func nudgeWords(lang string, r nudge.Rule) string {
	if !r.Active() {
		return tr(lang, "off")
	}
	if r.Write {
		return tr(lang, "on, after %d hours, written by AI", int(r.After/time.Hour))
	}
	return tr(lang, "on, after %d hours, fixed text", int(r.After/time.Hour))
}
