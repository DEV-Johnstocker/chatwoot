package admin

import (
	"context"
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/leads"
)

// The Leads page: customers who showed buying interest in a bot chat (asked
// about price, buying or a promotion) or in a Facebook comment, with how far
// they got. Names are personal data: shown here and in the CSV download
// only, never logged. Issue: DEV-Johnstocker/chatwoot#1

// LeadList reads the lead list (leads.Postgres).
type LeadList interface {
	List(ctx context.Context, accountID int64, f leads.Filter) ([]leads.Lead, error)
	Counts(ctx context.Context, accountID int64) (map[leads.Stage]int, error)
}

const (
	leadPageLimit = 200 // rows on the page; the CSV has up to leads.MaxList
	leadTimeout   = 10 * time.Second
)

type leadView struct {
	Name       string
	Channel    string
	Interest   string
	Stage      leads.Stage
	StageLabel string
	When       string
	Link       string // the chat in Chatwoot, or the comment queue
	LinkText   string
	External   bool   // Link opens Chatwoot in a new tab
	Ad         string // the ad or link the customer came from last; "" none
	AdEarlier  string // earlier ones, "Name (12 Nov), …"
}

type stageCount struct {
	Stage   leads.Stage
	Label   string
	Count   int
	Current bool
}

type leadsData struct {
	Enabled   bool
	AccountID int64
	Stage     leads.Stage // the filter; "" = all
	Stages    []stageCount
	Total     int
	Leads     []leadView
	Full      bool
	Error     string
	AdsOn     bool // show the Ad column
}

// stageLabel is the English name of a stage; the page translates it.
func stageLabel(s leads.Stage) string {
	switch s {
	case leads.StageCommented:
		return "Commented"
	case leads.StageAsked:
		return "Asked price"
	case leads.StageQuoted:
		return "Got price or link"
	case leads.StageSlip:
		return "Sent slip"
	case leads.StagePaid:
		return "Paid"
	}
	return string(s)
}

// stageParam reads ?stage=; an unknown value lists every stage.
func stageParam(raw string) leads.Stage {
	if s := leads.Stage(strings.TrimSpace(raw)); s.Valid() {
		return s
	}
	return ""
}

func (h *Handler) showLeads(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lang := langOf(r)
	data := leadsData{Enabled: h.Leads != nil, AccountID: accountParam(q.Get("account")), Stage: stageParam(q.Get("stage"))}
	if h.Leads == nil {
		h.renderPage(w, r, http.StatusOK, "leads.html", data)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), leadTimeout)
	defer cancel()
	counts, err := h.Leads.Counts(ctx, data.AccountID)
	var list []leads.Lead
	if err == nil {
		list, err = h.Leads.List(ctx, data.AccountID, leads.Filter{Stage: data.Stage, Limit: leadPageLimit + 1})
	}
	if err != nil {
		h.Logger.Error("list leads", "account_id", data.AccountID, "error", err)
		data.Error = tr(lang, "Could not read the lead list. Try again in a moment.")
	}
	for _, s := range leads.Stages {
		data.Stages = append(data.Stages, stageCount{Stage: s, Label: tr(lang, stageLabel(s)), Count: counts[s], Current: s == data.Stage})
		data.Total += counts[s]
	}
	if len(list) > leadPageLimit {
		list, data.Full = list[:leadPageLimit], true
	}
	pk := h.newPickers(r)
	for _, l := range list {
		data.Leads = append(data.Leads, h.toLeadView(lang, pk, l))
	}
	data.AdsOn = h.Ads != nil
	h.addLeadAds(ctx, lang, data.AccountID, list, data.Leads)
	h.renderPage(w, r, http.StatusOK, "leads.html", data)
}

func (h *Handler) toLeadView(lang string, pk *pickers, l leads.Lead) leadView {
	v := leadView{Name: l.Name, Interest: l.Interest, Stage: l.Stage, Channel: h.leadChannel(lang, pk, l)}
	if v.Name == "" {
		v.Name = tr(lang, "(no name)")
	}
	if !l.LastAt.IsZero() {
		v.When = h.whenIn(lang, l.LastAt)
	}
	v.StageLabel = tr(lang, stageLabel(l.Stage))
	v.Link, v.LinkText = h.leadLink(lang, l)
	v.External = v.Link != "" && !strings.HasPrefix(v.Link, "/")
	return v
}

// leadChannel is the inbox's name for a chat lead, or "Facebook comment".
func (h *Handler) leadChannel(lang string, pk *pickers, l leads.Lead) string {
	if l.Source == leads.SourceComment {
		return tr(lang, "Facebook comment")
	}
	if l.InboxID < 1 {
		return tr(lang, "Chat")
	}
	for _, o := range pk.inboxOptions(l.AccountID, strconv.FormatInt(l.InboxID, 10)) {
		if o.Selected {
			return o.Label
		}
	}
	return tr(lang, "Chat")
}

// leadLink opens the chat in Chatwoot, or the comment queue for a comment.
func (h *Handler) leadLink(lang string, l leads.Lead) (url, text string) {
	acct := strconv.FormatInt(l.AccountID, 10)
	if l.Source == leads.SourceComment {
		return "/admin/comments?account=" + acct, tr(lang, "Comment leads")
	}
	if h.ChatwootURL == "" {
		return "", tr(lang, "Chat #%d", l.ConversationID)
	}
	return h.ChatwootURL + "/app/accounts/" + acct + "/conversations/" + strconv.FormatInt(l.ConversationID, 10),
		tr(lang, "Open chat #%d", l.ConversationID)
}

// exportLeads downloads the lead list (with the page's stage filter) as a
// CSV file the owner can open in Excel or import into Google Sheets.
func (h *Handler) exportLeads(w http.ResponseWriter, r *http.Request) {
	if h.Leads == nil {
		http.Error(w, "lead list off: it needs DATABASE_URL", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	acct, ok := formAccount(q.Get("account"))
	if !ok {
		http.Error(w, "account must be a positive number", http.StatusBadRequest)
		return
	}
	stage := stageParam(q.Get("stage"))
	ctx, cancel := context.WithTimeout(r.Context(), leadTimeout)
	defer cancel()
	list, err := h.Leads.List(ctx, acct, leads.Filter{Stage: stage, Limit: leads.MaxList})
	if err != nil {
		h.Logger.Error("export leads", "account_id", acct, "error", err)
		http.Error(w, "could not read the lead list; try again", http.StatusBadGateway)
		return
	}
	var buf strings.Builder
	buf.WriteString("\uFEFF") // a byte-order mark, so Excel reads Thai names as UTF-8
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{"name", "channel", "interest", "stage", "last_contact", "first_contact", "contact_id", "chat_link"})
	pk := h.newPickers(r)
	for _, l := range list {
		link, _ := h.leadLink(langEN, l)
		if l.Source == leads.SourceComment {
			link = ""
		}
		_ = cw.Write([]string{
			leadCell(l.Name), leadCell(h.leadChannel(langEN, pk, l)), leadCell(l.Interest), string(l.Stage),
			csvTime(h, l.LastAt), csvTime(h, l.FirstAt), contactID(l.ContactID), link,
		})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		h.Logger.Error("export leads", "account_id", acct, "error", err)
		http.Error(w, "could not write the file", http.StatusInternalServerError)
		return
	}
	h.record(r, acct, "leads exported", strconv.Itoa(len(list))+" leads, stage "+stageOrAll(stage), nil)
	h.Logger.Info("leads exported", "account_id", acct, "rows", len(list), "stage", string(stage))
	name := "leads-account-" + strconv.FormatInt(acct, 10) + "-" + time.Now().In(h.loc()).Format("2006-01-02") + ".csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	_, _ = w.Write([]byte(buf.String()))
}

func stageOrAll(s leads.Stage) string {
	if s == "" {
		return "all"
	}
	return string(s)
}

// leadCell stops a spreadsheet from running a cell as a formula (OWASP "CSV
// injection"): a value whose first non-space character is = + - @ (or the
// full-width ＝ ＋ － ＠), or that starts with a tab or carriage return, gets a
// leading apostrophe. Names and comment summaries come from customers.
func leadCell(s string) string {
	if s == "" {
		return s
	}
	if s[0] == '\t' || s[0] == '\r' {
		return "'" + s
	}
	t := strings.TrimLeftFunc(s, unicode.IsSpace)
	if t != "" && strings.ContainsRune("=+-@＝＋－＠", []rune(t)[0]) {
		return "'" + s
	}
	return s
}

func csvTime(h *Handler, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(h.loc()).Format("2006-01-02 15:04")
}

func contactID(id int64) string {
	if id < 1 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}
