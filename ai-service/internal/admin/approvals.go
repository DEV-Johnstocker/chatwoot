package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/pending"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
)

// Approver applies or drops an AI-app edit waiting for approval (#60). The
// MCP server implements it with the same checks and history as its tools.
type Approver interface {
	Approve(ctx context.Context, accountID, id int64, version int, approver string) error
	Reject(ctx context.Context, accountID, id int64, version int, approver string) error
}

type approvalsData struct {
	Enabled bool
	Account int64
	Items   []approvalItem
	Done    string // "approved" or "rejected" after a decision
	Error   string
}

type approvalItem struct {
	ID      int64
	Version int
	Heading string
	From    string
	When    string
	New     bool   // nothing exists yet: show only the proposed values
	Delete  bool   // a delete: show only the current values
	Badge   string // "New document", "Delete document" or "New source"
	Fields  []fieldDiff
}

type fieldDiff struct {
	Label, Current, Proposed string
	Same                     bool // the change leaves this field as it is
}

var profileLabels = []struct{ key, label string }{
	{"business_name", "Business name"}, {"about", "What the business does"}, {"tone", "Tone and style"}, {"rules", "Rules"},
}

func (h *Handler) approvalsOn() bool { return h.Pending != nil && h.Approver != nil }

func (h *Handler) showApprovals(w http.ResponseWriter, r *http.Request) {
	acct := accountParam(r.URL.Query().Get("account"))
	data := approvalsData{Account: acct}
	if d := r.URL.Query().Get("done"); d == "approved" || d == "rejected" {
		data.Done = d
	}
	h.renderApprovals(w, r, http.StatusOK, data)
}

func (h *Handler) submitApprovals(w http.ResponseWriter, r *http.Request) {
	if !h.approvalsOn() {
		http.Error(w, "approvals are off: they need DATABASE_URL and MCP_ENABLED", http.StatusServiceUnavailable)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	f := r.PostForm
	acct, err1 := strconv.ParseInt(strings.TrimSpace(f.Get("account")), 10, 64)
	id, err2 := strconv.ParseInt(f.Get("id"), 10, 64)
	version, err3 := strconv.Atoi(f.Get("version"))
	decision := f.Get("decision")
	if err1 != nil || err2 != nil || err3 != nil || acct < 1 || id < 1 || version < 1 || (decision != "approve" && decision != "reject") {
		http.Error(w, "account, id, version and decision (approve or reject) are required", http.StatusBadRequest)
		return
	}
	decide := h.Approver.Approve
	if decision == "reject" {
		decide = h.Approver.Reject
	}
	err := decide(context.WithoutCancel(r.Context()), acct, id, version, actor(r))
	switch {
	case err == nil:
		h.Logger.Info("ai change "+decision+"d", "account_id", acct, "change_id", id)
		done := map[string]string{"approve": "approved", "reject": "rejected"}[decision]
		http.Redirect(w, r, "/admin/approvals?account="+strconv.FormatInt(acct, 10)+"&done="+done, http.StatusSeeOther)
	case errors.Is(err, pending.ErrChanged):
		h.renderApprovals(w, r, http.StatusConflict, approvalsData{Account: acct,
			Error: "The app changed this after you opened it. Nothing was applied; check the new version below."})
	case errors.Is(err, pending.ErrTargetChanged):
		h.renderApprovals(w, r, http.StatusConflict, approvalsData{Account: acct,
			Error: "Not applied: the document changed since the app proposed this, and approving would overwrite the newer version. Reject it, or ask the app to propose the change again."})
	case errors.Is(err, pending.ErrNotFound):
		h.renderApprovals(w, r, http.StatusNotFound, approvalsData{Account: acct,
			Error: "That change is no longer waiting. It may have been approved or rejected already."})
	default:
		h.Logger.Error("ai change "+decision+" failed", "account_id", acct, "change_id", id, "error", err)
		h.renderApprovals(w, r, http.StatusUnprocessableEntity, approvalsData{Account: acct,
			Error: "Not applied. The change still waits; try again, and check the service logs if it keeps failing."})
	}
}

func (h *Handler) renderApprovals(w http.ResponseWriter, r *http.Request, status int, data approvalsData) {
	lang := langOf(r)
	// Errors are set in English; show them in the page language.
	data.Error = tr(lang, data.Error)
	data.Enabled = h.approvalsOn()
	if data.Enabled {
		items, err := h.approvalItems(r.Context(), lang, data.Account)
		if err != nil {
			h.Logger.Error("load ai changes", "account_id", data.Account, "error", err)
			data.Error = tr(lang, "Could not load the waiting changes. Check the database connection.")
		}
		data.Items = items
	}
	h.renderPage(w, r, status, "approvals.html", data)
}

// approvalItems shows each waiting change next to what customers see now.
func (h *Handler) approvalItems(ctx context.Context, lang string, acct int64) ([]approvalItem, error) {
	changes, err := h.Pending.List(ctx, acct)
	if err != nil {
		return nil, err
	}
	items := make([]approvalItem, 0, len(changes))
	for _, c := range changes {
		it := approvalItem{ID: c.ID, Version: c.Version, From: c.Actor, When: h.whenIn(lang, c.At)}
		switch c.Kind {
		case pending.KindProfile:
			it.Heading = tr(lang, "Business profile")
			it.Fields, err = h.profileDiff(ctx, lang, acct, c.Proposed)
		case pending.KindDocument:
			if it.Delete = isDelete(c.Proposed); it.Delete {
				it.Heading, it.Fields, err = h.deleteDiff(ctx, lang, acct, c.Target, c.Proposed)
				it.Badge = tr(lang, "Delete document")
				break
			}
			it.Heading, it.New, it.Fields, err = h.documentDiff(ctx, lang, acct, c.Target, c.Proposed)
			if it.New {
				it.Badge = tr(lang, "New document")
			}
		case pending.KindSource:
			it.Heading, it.Fields, err = sourceDiff(lang, c.Proposed)
			it.New, it.Badge = true, tr(lang, "New source")
		}
		if err != nil {
			return nil, err
		}
		for i := range it.Fields {
			it.Fields[i].Same = !it.New && !it.Delete && it.Fields[i].Current == it.Fields[i].Proposed
		}
		items = append(items, it)
	}
	return items, nil
}

// sourceDiff shows a proposed web page or data file: the full address (the
// owner checks it is the right tab), what it is, and what approving does.
func sourceDiff(lang string, raw []byte) (string, []fieldDiff, error) {
	var s struct {
		URL   string   `json:"url"`
		Kind  string   `json:"kind"`
		Rows  int      `json:"rows"`
		Title string   `json:"title"`
		Notes []string `json:"notes"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", nil, err
	}
	where := s.URL
	if u, err := url.Parse(s.URL); err == nil {
		where = u.Host + u.Path
	}
	kind := tr(lang, "Web page")
	if s.Kind == "feed" {
		kind = tr(lang, "Data file (CSV or JSON Lines)")
	}
	fields := []fieldDiff{{Label: tr(lang, "Address"), Proposed: s.URL}, {Label: tr(lang, "Kind"), Proposed: kind}}
	if s.Title != "" {
		fields = append(fields, fieldDiff{Label: tr(lang, "Title"), Proposed: s.Title})
	}
	if s.Rows > 0 {
		fields = append(fields, fieldDiff{Label: tr(lang, "Rows"), Proposed: tr(lang, "%d rows", s.Rows)})
	}
	if len(s.Notes) > 0 {
		fields = append(fields, fieldDiff{Label: tr(lang, "Checks"), Proposed: strings.Join(s.Notes, "\n")})
	}
	fields = append(fields, fieldDiff{Label: tr(lang, "After approval"),
		Proposed: tr(lang, "It is loaded at once and the bot answers from it. It is checked for changes on a schedule, and you can remove it on the Knowledge base page.")})
	return tr(lang, "New source: %s", where), fields, nil
}

// isDelete reports whether a document change asks to delete the document.
func isDelete(raw []byte) bool {
	var d struct {
		Delete bool `json:"delete"`
	}
	return json.Unmarshal(raw, &d) == nil && d.Delete
}

// deleteDiff shows the document a delete removes, as customers see it now.
func (h *Handler) deleteDiff(ctx context.Context, lang string, acct int64, target string, raw []byte) (string, []fieldDiff, error) {
	var d struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return "", nil, err
	}
	cur := knowledge.Document{Title: d.Title}
	if h.Knowledge != nil {
		got, err := h.Knowledge.Get(ctx, acct, d.ID)
		switch {
		case err == nil:
			cur = got
		case !errors.Is(err, knowledge.ErrNotFound):
			return "", nil, err
		}
	}
	fields := []fieldDiff{{Label: tr(lang, "Title"), Current: cur.Title}, {Label: tr(lang, "Text"), Current: cur.Content},
		{Label: tr(lang, "After approval"), Current: tr(lang, "The document is deleted and the bot stops using it. A copy stays in the change history.")}}
	return tr(lang, "Knowledge: %s", target), fields, nil
}

func (h *Handler) profileDiff(ctx context.Context, lang string, acct int64, raw []byte) ([]fieldDiff, error) {
	var proposed map[string]string
	if err := json.Unmarshal(raw, &proposed); err != nil {
		return nil, err
	}
	cur := profile.Profile{}
	if h.Profiles != nil {
		var err error
		if cur, err = h.Profiles.Get(ctx, acct); err != nil {
			return nil, err
		}
	}
	now := map[string]string{"business_name": cur.BusinessName, "about": cur.About, "tone": cur.Tone, "rules": cur.Rules}
	var out []fieldDiff
	for _, f := range profileLabels {
		if v, ok := proposed[f.key]; ok {
			out = append(out, fieldDiff{Label: tr(lang, f.label), Current: now[f.key], Proposed: v})
		}
	}
	return out, nil
}

func (h *Handler) documentDiff(ctx context.Context, lang string, acct int64, key string, raw []byte) (string, bool, []fieldDiff, error) {
	var d struct {
		Kind      string `json:"kind"`
		InboxID   int64  `json:"inbox_id"`
		Title     string `json:"title"`
		Content   string `json:"content"`
		StartDate string `json:"start_date"`
		LastDay   string `json:"last_day"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return "", false, nil, err
	}
	var cur knowledge.Document
	isNew := false
	if h.Knowledge != nil {
		var err error
		cur, err = h.Knowledge.GetByKey(ctx, acct, key)
		switch {
		case errors.Is(err, knowledge.ErrNotFound):
			isNew = true
		case err != nil:
			return "", false, nil, err
		}
	}
	fields := []fieldDiff{{Label: tr(lang, "Title"), Current: cur.Title, Proposed: d.Title}, {Label: tr(lang, "Text"), Current: cur.Content, Proposed: d.Content}}
	// Everything the approval applies is shown: where it is used, and the
	// kind of a new document (an existing one keeps its kind).
	usedIn := fieldDiff{Label: tr(lang, "Used in"), Proposed: inboxLabel(lang, d.InboxID)}
	if !isNew && h.Knowledge != nil {
		usedIn.Current = inboxLabel(lang, cur.InboxID)
	}
	fields = append(fields, usedIn)
	if isNew {
		kind := tr(lang, "Text")
		if d.Kind == "faq" {
			kind = tr(lang, "FAQ (question and answer)")
		}
		fields = append(fields, fieldDiff{Label: tr(lang, "Kind"), Proposed: kind})
	}
	if d.StartDate != "" || d.LastDay != "" {
		fields = append(fields, fieldDiff{Label: tr(lang, "Dates"), Proposed: tr(lang, "from %s to %s", orNow(lang, d.StartDate), orNoEnd(lang, d.LastDay))})
	}
	return tr(lang, "Knowledge: %s", key), isNew, fields, nil
}

func orNow(lang, s string) string {
	if s == "" {
		return tr(lang, "now")
	}
	return s
}

func orNoEnd(lang, s string) string {
	if s == "" {
		return tr(lang, "no end")
	}
	return s
}

func inboxLabel(lang string, id int64) string {
	if id > 0 {
		return tr(lang, "Only inbox %d", id)
	}
	return tr(lang, "All inboxes")
}
