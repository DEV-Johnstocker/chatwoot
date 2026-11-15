package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
)

// The gap report: questions the bot handed to staff only because it had no
// answer (Chatwoot label kb-gap), each with "Turn into an answer". The list
// is read live from Chatwoot; nothing is stored here.
// Issue: DEV-Johnstocker/chatwoot#1

// GapFinder reads and closes gap chats (gaps.Finder over chatwoot.Reader).
type GapFinder interface {
	List(ctx context.Context, accountID int64, limit int) ([]gaps.Gap, error)
	Done(ctx context.Context, accountID, conversationID int64) error
}

const (
	gapPageLimit = 25               // gaps shown at once, newest first
	gapTimeout   = 20 * time.Second // the Chatwoot reads behind one page
	maxGapForm   = 4 << 10
)

type gapView struct {
	ID         int64
	InboxID    string
	When       string
	Wanted     string
	Asked      string
	Question   string // pre-filled FAQ question: the customer's words, else the summary
	StaffReply string
	NoteFound  bool
	ChatURL    string
}

type gapsData struct {
	Enabled   bool // the reader token is set
	AccountID int64
	Gaps      []gapView
	Full      bool // the list may have more than it shows
	Notice    string
	Error     string
}

func (h *Handler) showGaps(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lang := langOf(r)
	data := gapsData{Enabled: h.Gaps != nil, AccountID: accountParam(q.Get("account"))}
	switch q.Get("done") {
	case "answered":
		data.Notice = tr(lang, "Answer added to the knowledge base, and the chat is off the list.")
	case "answered-kept":
		data.Notice = tr(lang, "Answer added to the knowledge base. The chat could not be taken off the list; mark it done below.")
	case "closed":
		data.Notice = tr(lang, "Marked done. The chat now has the label kb-gap-done in Chatwoot.")
	}
	if h.Gaps != nil {
		ctx, cancel := context.WithTimeout(r.Context(), gapTimeout)
		defer cancel()
		list, err := h.Gaps.List(ctx, data.AccountID, gapPageLimit)
		if err != nil {
			h.Logger.Error("list gap chats", "account_id", data.AccountID, "error", err)
			data.Error = tr(lang, "Could not read the chats from Chatwoot. Check that the AI Reader user still has access.")
		}
		for _, g := range list {
			data.Gaps = append(data.Gaps, h.toGapView(lang, data.AccountID, g))
		}
		data.Full = len(list) >= gapPageLimit
	}
	h.renderPage(w, r, http.StatusOK, "gaps.html", data)
}

func (h *Handler) toGapView(lang string, acct int64, g gaps.Gap) gapView {
	v := gapView{ID: g.ConversationID, Wanted: g.Wanted, Asked: g.Asked, Question: g.Question(), StaffReply: g.StaffReply, NoteFound: g.NoteFound}
	if g.InboxID > 0 {
		v.InboxID = strconv.FormatInt(g.InboxID, 10)
	}
	if !g.At.IsZero() {
		v.When = h.whenIn(lang, g.At)
	}
	if h.ChatwootURL != "" {
		v.ChatURL = h.ChatwootURL + "/app/accounts/" + strconv.FormatInt(acct, 10) + "/conversations/" + strconv.FormatInt(g.ConversationID, 10)
	}
	return v
}

// submitGaps handles "Done" (takes the chat off the list). The answer form
// on each card posts to /admin/knowledge (add_faq with the gap field), so
// duplicate checks and errors work as on the Knowledge page.
func (h *Handler) submitGaps(w http.ResponseWriter, r *http.Request) {
	if h.Gaps == nil {
		http.Error(w, "gap report off: set CHATWOOT_READER_TOKEN", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxGapForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "form too large or invalid", http.StatusBadRequest)
		return
	}
	f := r.PostForm
	acct, ok := formAccount(f.Get("account"))
	conv, err := strconv.ParseInt(strings.TrimSpace(f.Get("conversation")), 10, 64)
	if !ok || err != nil || conv < 1 {
		http.Error(w, "account and conversation must be positive numbers", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), gapTimeout)
	defer cancel()
	switch f.Get("action") {
	case "done":
		if err := h.Gaps.Done(ctx, acct, conv); err != nil {
			h.gapFailed(w, r, acct, conv, err)
			return
		}
		h.record(r, acct, "gap marked done", "chat #"+strconv.FormatInt(conv, 10), nil)
		h.Logger.Info("gap marked done", "account_id", acct, "conversation_id", conv)
		http.Redirect(w, r, "/admin/gaps?account="+strconv.FormatInt(acct, 10)+"&done=closed", http.StatusSeeOther)
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
	}
}

// gapFailed shows the list again with why the action did not happen.
func (h *Handler) gapFailed(w http.ResponseWriter, r *http.Request, acct, conv int64, err error) {
	lang := langOf(r)
	msg := tr(lang, "Could not reach Chatwoot. Nothing was changed; try again.")
	status := http.StatusBadGateway
	if errors.Is(err, gaps.ErrNotAGap) {
		msg, status = tr(lang, "Chat #%d is no longer on the gap list. Someone may have handled it already.", conv), http.StatusConflict
	} else {
		h.Logger.Warn("gap action failed", "account_id", acct, "conversation_id", conv, "error", err)
	}
	data := gapsData{Enabled: true, AccountID: acct, Error: msg}
	ctx, cancel := context.WithTimeout(r.Context(), gapTimeout)
	defer cancel()
	if list, lerr := h.Gaps.List(ctx, acct, gapPageLimit); lerr == nil {
		for _, g := range list {
			data.Gaps = append(data.Gaps, h.toGapView(lang, acct, g))
		}
		data.Full = len(list) >= gapPageLimit
	}
	h.renderPage(w, r, status, "gaps.html", data)
}

// closeGap takes the chat an answer came from off the gap list; it reports
// whether that worked. The answer is saved either way.
func (h *Handler) closeGap(r *http.Request, acct, conv int64) bool {
	if h.Gaps == nil || conv < 1 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), gapTimeout)
	defer cancel()
	if err := h.Gaps.Done(ctx, acct, conv); err != nil && !errors.Is(err, gaps.ErrNotAGap) {
		h.Logger.Warn("gap not closed after its answer was added", "account_id", acct, "conversation_id", conv, "error", err)
		return false
	}
	h.record(r, acct, "gap answered", "chat #"+strconv.FormatInt(conv, 10), nil)
	return true
}
