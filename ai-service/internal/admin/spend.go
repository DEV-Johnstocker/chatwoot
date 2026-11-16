package admin

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/spend"
)

// The AI spend page: what the model calls cost today, this month and over
// 30 days, by purpose and by model, and the owner's caps. A cap only alerts
// (Slack and a banner here); the bot keeps answering past it.
// Issue: DEV-Johnstocker/chatwoot#1

// SpendBoard reads usage and keeps caps (spend.Board).
type SpendBoard interface {
	Report(ctx context.Context, accountID int64) (spend.Report, error)
	Caps(ctx context.Context) ([]spend.Cap, error)
	SaveCap(ctx context.Context, c spend.Cap) error
	Warnings() []spend.Status
	Statuses() []spend.Status
	Alerting() bool
}

const (
	maxSpendForm = 2 << 10
	maxCapUSD    = 100_000 // a typo guard, far above any real cap
	spendTimeout = 15 * time.Second
)

type spendLine struct {
	Name          string
	Calls         string
	Input, Output string // token counts, grouped
	Cost          string
	Share         int // % of the month's cost
	Priced        bool
}

type spendBar struct {
	Label, Cost string
	Height      int // % of the highest day
	Today       bool
}

type capView struct {
	Who, Period, Spent, Cap string
	Percent, Fill           int // Fill is capped at 100 for the meter
	Level                   int
	AccountID               int64
}

type capRow struct {
	AccountID      int64
	Who            string
	Daily, Monthly string // dollars, "" = none
	UpdatedBy      string
	UpdatedAt      string
}

type spendData struct {
	Enabled     bool
	Scope       int64 // -1: every business
	ScopeName   string
	Today       string
	Month       string
	TodayCalls  string
	MonthCalls  string
	MonthName   string
	Bars        []spendBar
	MaxDay      string
	ByPurpose   []spendLine
	ByModel     []spendLine
	Unpriced    []string
	Meters      []capView
	Caps        []capRow
	Alerting    bool
	PricesAsOf  string
	Notice      string
	Error       string
	FormAccount string
	FormDaily   string
	FormMonthly string
}

// spendScope reads ?account=: empty or "all" is every business.
func spendScope(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "all" {
		return -1
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n > 0 {
		return n
	}
	return -1
}

func (h *Handler) showSpend(w http.ResponseWriter, r *http.Request) {
	lang := langOf(r)
	data := spendData{Enabled: h.Spend != nil, Scope: spendScope(r.URL.Query().Get("account"))}
	if r.URL.Query().Get("done") == "saved" {
		data.Notice = tr(lang, "Cap saved.")
	}
	h.renderSpend(w, r, http.StatusOK, data)
}

// renderSpend fills the page from the board and shows it.
func (h *Handler) renderSpend(w http.ResponseWriter, r *http.Request, status int, data spendData) {
	lang := langOf(r)
	data.ScopeName = h.scopeName(lang, data.Scope)
	if h.Spend == nil {
		h.renderPage(w, r, status, "spend.html", data)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), spendTimeout)
	defer cancel()
	rep, err := h.Spend.Report(ctx, data.Scope)
	if err != nil {
		h.Logger.Error("ai spend report failed", "account_id", data.Scope, "error", err)
		data.Error = tr(lang, "Could not read AI usage from the database. Try again.")
	}
	h.fillReport(lang, &data, rep)
	caps, err := h.Spend.Caps(ctx)
	if err != nil {
		h.Logger.Error("ai spend caps read failed", "error", err)
		data.Error = tr(lang, "Could not read AI usage from the database. Try again.")
	}
	for _, c := range caps {
		data.Caps = append(data.Caps, capRow{AccountID: c.AccountID, Who: h.scopeName(lang, c.AccountID),
			Daily: dollars(c.Daily), Monthly: dollars(c.Monthly), UpdatedBy: c.UpdatedBy, UpdatedAt: h.whenIn(lang, c.UpdatedAt)})
	}
	data.Meters = h.capViews(lang, h.Spend.Statuses())
	data.Alerting = h.Spend.Alerting()
	h.renderPage(w, r, status, "spend.html", data)
}

func (h *Handler) fillReport(lang string, d *spendData, rep spend.Report) {
	d.Today, d.Month = spend.Money(rep.Today), spend.Money(rep.Month)
	d.TodayCalls, d.MonthCalls = grouped(rep.TodayCalls), grouped(rep.MonthCalls)
	d.Unpriced = rep.Unpriced
	d.PricesAsOf = "2026-11-16"
	if n := len(rep.Days); n > 0 {
		d.MonthName = monthName(lang, rep.Days[n-1].Day)
	}
	var top spend.Micros
	for _, day := range rep.Days {
		top = max(top, day.Cost)
	}
	d.MaxDay = spend.Money(top)
	for i, day := range rep.Days {
		b := spendBar{Label: shortDate(lang, day.Day), Cost: spend.Money(day.Cost), Today: i == len(rep.Days)-1}
		if top > 0 {
			b.Height = int(math.Ceil(float64(day.Cost) * 100 / float64(top)))
		}
		d.Bars = append(d.Bars, b)
	}
	for _, l := range rep.ByPurpose {
		d.ByPurpose = append(d.ByPurpose, lineView(l, rep.Month, purposeName(lang, l.Name)))
	}
	for _, l := range rep.ByModel {
		name := l.Name
		if name == "" {
			name = tr(lang, "(unknown)")
		}
		d.ByModel = append(d.ByModel, lineView(l, rep.Month, name))
	}
}

func lineView(l spend.Line, month spend.Micros, name string) spendLine {
	v := spendLine{Name: name, Calls: grouped(l.Calls), Input: grouped(l.Usage.InputTokens), Output: grouped(l.Usage.OutputTokens),
		Cost: spend.Money(l.Cost), Priced: l.Priced}
	if month > 0 {
		v.Share = int(math.Round(float64(l.Cost) * 100 / float64(month)))
	}
	return v
}

func (h *Handler) capViews(lang string, statuses []spend.Status) []capView {
	var out []capView
	for _, s := range statuses {
		out = append(out, capView{Who: h.scopeName(lang, s.AccountID), Period: periodName(lang, s.Period),
			Spent: spend.Money(s.Spent), Cap: spend.Money(s.Cap), Percent: s.Percent, Fill: min(s.Percent, 100),
			Level: s.Level, AccountID: s.AccountID})
	}
	return out
}

// spendWarnings are the banner lines for caps at 80% or more.
func (h *Handler) spendWarnings(lang string) []capView {
	if h.Spend == nil {
		return nil
	}
	out := h.capViews(lang, h.Spend.Warnings())
	sort.SliceStable(out, func(i, j int) bool { return out[i].Level > out[j].Level }) // the nav badge shows the first
	return out
}

func (h *Handler) scopeName(lang string, acct int64) string {
	switch {
	case acct < 0:
		return tr(lang, "All businesses")
	case acct == 0:
		return tr(lang, "Whole service")
	}
	return tr(lang, "Account %d", acct)
}

func purposeName(lang, p string) string {
	names := map[string]string{
		spend.PurposeReply: "Replies to customers", spend.PurposeRewrite: "Search question rewrite",
		spend.PurposeNote: "Notes for staff", spend.PurposeSticker: "Sticker replies",
		spend.PurposeComment: "Comment reply drafts", spend.PurposeCommentReview: "Comment draft review",
		spend.PurposeEmbedding: "Knowledge search (embeddings)", spend.PurposeTranscription: "Voice notes",
		spend.PurposeTest: "Your tests (Test bot, AI tools)",
	}
	if n, ok := names[p]; ok {
		return tr(lang, n)
	}
	return p
}

func periodName(lang, p string) string {
	if p == spend.PeriodMonth {
		return tr(lang, "this month")
	}
	return tr(lang, "today")
}

// grouped writes n with thousands separators.
func grouped(n int64) string {
	s := strconv.FormatInt(n, 10)
	if n < 0 {
		return s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// dollars shows a cap for the form ("" = none).
func dollars(m spend.Micros) string {
	if m <= 0 {
		return ""
	}
	return strconv.FormatFloat(m.USD(), 'f', 2, 64)
}

func shortDate(lang string, d time.Time) string {
	if lang == langTH {
		return fmt.Sprintf("%d %s", d.Day(), thaiMonths[d.Month()-1])
	}
	return d.Format("2 Jan")
}

func monthName(lang string, d time.Time) string {
	if lang == langTH {
		return fmt.Sprintf("%s %d", thaiMonthsLong[d.Month()-1], d.Year()+543)
	}
	return d.Format("January 2006")
}

var thaiMonthsLong = [...]string{"มกราคม", "กุมภาพันธ์", "มีนาคม", "เมษายน", "พฤษภาคม", "มิถุนายน",
	"กรกฎาคม", "สิงหาคม", "กันยายน", "ตุลาคม", "พฤศจิกายน", "ธันวาคม"}

// parseCapUSD reads a dollar amount: empty is no cap; at most 2 decimals.
func parseCapUSD(raw string) (spend.Micros, bool) {
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "$"))
	if raw == "" {
		return 0, true
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v > maxCapUSD {
		return 0, false
	}
	if math.Abs(v*100-math.Round(v*100)) > 1e-6 {
		return 0, false
	}
	return spend.Micros(math.Round(v * 100 * 10_000)), true
}

func (h *Handler) submitSpend(w http.ResponseWriter, r *http.Request) {
	if h.Spend == nil {
		http.Error(w, "ai spend off: it needs DATABASE_URL", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSpendForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "form too large or invalid", http.StatusBadRequest)
		return
	}
	f := r.PostForm
	if f.Get("action") != "save_cap" {
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	lang := langOf(r)
	scope := spendScope(f.Get("scope"))
	data := spendData{Enabled: true, Scope: scope, FormAccount: f.Get("account"), FormDaily: f.Get("daily"), FormMonthly: f.Get("monthly")}
	acct, err := strconv.ParseInt(strings.TrimSpace(f.Get("account")), 10, 64)
	daily, okD := parseCapUSD(f.Get("daily"))
	monthly, okM := parseCapUSD(f.Get("monthly"))
	switch {
	case err != nil || acct < 0:
		data.Error = tr(lang, "Choose the whole service (0) or a business account number.")
	case !okD || !okM:
		data.Error = tr(lang, "A cap is a dollar amount above 0 and up to %d, with at most 2 decimals. Leave it empty for no cap.", maxCapUSD)
	}
	if data.Error != "" {
		h.renderSpend(w, r, http.StatusBadRequest, data)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), spendTimeout)
	defer cancel()
	c := spend.Cap{AccountID: acct, Daily: daily, Monthly: monthly, UpdatedBy: actor(r)}
	if err := h.Spend.SaveCap(ctx, c); err != nil {
		h.Logger.Error("ai spend cap save failed", "account_id", acct, "error", err)
		data.Error = tr(lang, "The cap was not saved. Try again.")
		h.renderSpend(w, r, http.StatusInternalServerError, data)
		return
	}
	summary := fmt.Sprintf("%s: daily %s, monthly %s", h.scopeName("en", acct), capText(daily), capText(monthly))
	h.record(r, acct, "ai spend cap set", summary, nil)
	h.Logger.Info("ai spend cap set", "account_id", acct)
	back := "/admin/spend?done=saved"
	if scope > 0 {
		back += "&account=" + strconv.FormatInt(scope, 10)
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func capText(m spend.Micros) string {
	if m <= 0 {
		return "none"
	}
	return spend.Money(m)
}
