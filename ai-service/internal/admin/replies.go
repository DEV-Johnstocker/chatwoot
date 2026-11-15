package admin

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/ratings"
)

// The "Rate replies" page: recent bot replies from Chatwoot, each with a
// thumbs up or down (and a reason for a thumbs down), plus the list of rated
// replies. Ratings are kept so they can later become test cases.
// Issue: DEV-Johnstocker/chatwoot#1

// ReplyFeed lists recent bot replies (ratings.Feed over chatwoot.Reader).
type ReplyFeed interface {
	Recent(ctx context.Context, accountID int64, page int) ([]ratings.Reply, error)
}

const (
	// maxRateForm fits the copied texts at their caps (7,000 characters) when
	// every character is Thai: 3 bytes in UTF-8, 9 once URL-encoded.
	maxRateForm    = (ratings.MaxCustomer+ratings.MaxBot+ratings.MaxNote)*9 + 4<<10
	ratedPageLimit = 50
	maxReplyPage   = 20
	replyTimeout   = 20 * time.Second
)

// reasonLabels name the reasons for a thumbs down, in ratings.Reasons order.
var reasonLabels = map[string]string{
	"wrong_fact":  "Wrong or made-up fact",
	"missed_sale": "Missed the sale",
	"handoff":     "Handed off when it should not",
	"tone":        "Tone or wording",
	"too_long":    "Too long",
	"other":       "Other (say what in the note)",
}

type replyView struct {
	ConversationID int64
	MessageID      int64
	InboxID        string
	Customer       string
	Bot            string
	When           string
	RepliedUnix    int64
	ChatURL        string
	Rated          bool
	Good           bool
	Reason         string // the key
	ReasonLabel    string
	Note           string
	RatedWhen      string
}

type repliesData struct {
	Enabled   bool // a database is configured
	FeedOn    bool // the reader token is set
	AccountID int64
	View      string // "torate" or "rated"
	Filter    string // rated view: "", "good" or "bad"
	Page      int
	Replies   []replyView
	Hidden    int // replies on this page already rated
	Good, Bad int
	Total     int // Good + Bad
	Prev      int // newer page of chats; 0 for none
	Next      int // older page of chats; 0 for none
	Reasons   []option
	Notice    string
	Error     string
}

func (h *Handler) showReplies(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	data := repliesData{AccountID: accountParam(q.Get("account")), View: q.Get("view"), Filter: q.Get("filter")}
	data.Page, _ = strconv.Atoi(q.Get("page"))
	switch q.Get("done") {
	case "good":
		data.Notice = tr(langOf(r), "Saved: good reply.")
	case "bad":
		data.Notice = tr(langOf(r), "Saved: needs work. It is listed under Rated.")
	}
	h.renderReplies(w, r, http.StatusOK, data)
}

func (h *Handler) renderReplies(w http.ResponseWriter, r *http.Request, status int, data repliesData) {
	lang := langOf(r)
	data.Enabled, data.FeedOn = h.Ratings != nil, h.Replies != nil
	if data.View != "rated" {
		data.View = "torate"
	}
	if data.Filter != "good" && data.Filter != "bad" {
		data.Filter = ""
	}
	data.Page = min(max(data.Page, 1), maxReplyPage)
	if data.Page > 1 {
		data.Prev = data.Page - 1
	}
	if data.Page < maxReplyPage {
		data.Next = data.Page + 1
	}
	for _, k := range ratings.Reasons {
		data.Reasons = append(data.Reasons, option{Value: k, Label: tr(lang, reasonLabels[k])})
	}
	if !data.Enabled {
		h.renderPage(w, r, status, "replies.html", data)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), replyTimeout)
	defer cancel()
	var err error
	if data.Good, data.Bad, err = h.Ratings.Counts(ctx, data.AccountID); err != nil {
		h.Logger.Error("count reply ratings", "account_id", data.AccountID, "error", err)
	}
	data.Total = data.Good + data.Bad
	if data.View == "rated" {
		h.loadRated(ctx, lang, &data)
	} else if data.FeedOn {
		h.loadToRate(ctx, lang, &data)
	}
	h.renderPage(w, r, status, "replies.html", data)
}

// loadToRate fills the page with recent bot replies not rated yet.
func (h *Handler) loadToRate(ctx context.Context, lang string, data *repliesData) {
	list, err := h.Replies.Recent(ctx, data.AccountID, data.Page)
	if err != nil {
		h.Logger.Error("list recent replies", "account_id", data.AccountID, "error", err)
		if data.Error == "" {
			data.Error = tr(lang, "Could not read the chats from Chatwoot. Check that the AI Reader user still has access.")
		}
		return
	}
	ids := make([]int64, 0, len(list))
	for _, rp := range list {
		ids = append(ids, rp.MessageID)
	}
	rated, err := h.Ratings.Rated(ctx, data.AccountID, ids)
	if err != nil {
		h.Logger.Error("load reply ratings", "account_id", data.AccountID, "error", err)
	}
	for _, rp := range list {
		if _, done := rated[rp.MessageID]; done {
			data.Hidden++
			continue
		}
		v := replyView{ConversationID: rp.ConversationID, MessageID: rp.MessageID, Customer: rp.Customer, Bot: rp.Bot,
			RepliedUnix: rp.At.Unix(), ChatURL: h.chatURL(data.AccountID, rp.ConversationID)}
		if rp.InboxID > 0 {
			v.InboxID = strconv.FormatInt(rp.InboxID, 10)
		}
		if !rp.At.IsZero() {
			v.When = h.whenIn(lang, rp.At)
		}
		data.Replies = append(data.Replies, v)
	}
}

// loadRated fills the page with saved ratings, newest first.
func (h *Handler) loadRated(ctx context.Context, lang string, data *repliesData) {
	var good *bool
	if data.Filter != "" {
		g := data.Filter == "good"
		good = &g
	}
	list, err := h.Ratings.List(ctx, data.AccountID, good, ratedPageLimit)
	if err != nil {
		h.Logger.Error("list reply ratings", "account_id", data.AccountID, "error", err)
		data.Error = tr(lang, "Could not load the ratings. Check the database connection.")
		return
	}
	for _, rt := range list {
		v := replyView{ConversationID: rt.ConversationID, MessageID: rt.MessageID, Customer: rt.Customer, Bot: rt.Bot,
			RepliedUnix: rt.RepliedAt.Unix(), ChatURL: h.chatURL(data.AccountID, rt.ConversationID),
			Rated: true, Good: rt.Good, Reason: rt.Reason, Note: rt.Note, RatedWhen: h.whenIn(lang, rt.RatedAt)}
		if rt.Reason != "" {
			v.ReasonLabel = tr(lang, reasonLabels[rt.Reason])
		}
		if !rt.RepliedAt.IsZero() {
			v.When = h.whenIn(lang, rt.RepliedAt)
		} else {
			v.RepliedUnix = 0
		}
		data.Replies = append(data.Replies, v)
	}
}

func (h *Handler) chatURL(acct, conv int64) string {
	if h.ChatwootURL == "" {
		return ""
	}
	return h.ChatwootURL + "/app/accounts/" + strconv.FormatInt(acct, 10) + "/conversations/" + strconv.FormatInt(conv, 10)
}

// submitReplies saves one rating.
func (h *Handler) submitReplies(w http.ResponseWriter, r *http.Request) {
	if h.Ratings == nil {
		http.Error(w, "ratings disabled: set DATABASE_URL", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRateForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "form too large or invalid", http.StatusBadRequest)
		return
	}
	f := r.PostForm
	acct, ok := formAccount(f.Get("account"))
	conv, err1 := strconv.ParseInt(strings.TrimSpace(f.Get("conversation")), 10, 64)
	msg, err2 := strconv.ParseInt(strings.TrimSpace(f.Get("message")), 10, 64)
	verdict := f.Get("rating")
	if !ok || err1 != nil || err2 != nil || conv < 1 || msg < 1 || (verdict != "good" && verdict != "bad") {
		http.Error(w, "account, chat, message and rating (good or bad) are required", http.StatusBadRequest)
		return
	}
	// The copied texts come back from the page as hidden fields: the owner's
	// own copy of what was shown, not re-read from Chatwoot (admin-trusted).
	rt := ratings.Rating{AccountID: acct, ConversationID: conv, MessageID: msg, Good: verdict == "good",
		Reason: f.Get("reason"), Note: f.Get("note"), Customer: f.Get("customer"), Bot: f.Get("bot"), RatedBy: actor(r)}
	if sec, err := strconv.ParseInt(f.Get("replied_at"), 10, 64); err == nil && sec > 0 && sec <= time.Now().Add(24*time.Hour).Unix() {
		rt.RepliedAt = time.Unix(sec, 0)
	}
	back := repliesData{AccountID: acct, View: f.Get("view"), Filter: f.Get("filter")}
	back.Page, _ = strconv.Atoi(f.Get("page"))
	if err := rt.Clean(); err != nil {
		back.Error = tr(langOf(r), "Not saved: %s.", tr(langOf(r), err.Error()))
		h.renderReplies(w, r, http.StatusUnprocessableEntity, back)
		return
	}
	if err := h.Ratings.Save(r.Context(), rt); err != nil {
		h.Logger.Error("save reply rating", "account_id", acct, "conversation_id", conv, "error", err)
		back.Error = tr(langOf(r), "Could not save the rating. Check the database connection.")
		h.renderReplies(w, r, http.StatusInternalServerError, back)
		return
	}
	h.Logger.Info("reply rated", "account_id", acct, "conversation_id", conv, "good", rt.Good, "reason", rt.Reason)
	q := url.Values{"account": {strconv.FormatInt(acct, 10)}, "done": {verdict}}
	if back.View == "rated" {
		q.Set("view", "rated")
		if back.Filter == "good" || back.Filter == "bad" {
			q.Set("filter", back.Filter)
		}
	}
	if back.Page > 1 && back.Page <= maxReplyPage {
		q.Set("page", strconv.Itoa(back.Page))
	}
	http.Redirect(w, r, "/admin/replies?"+q.Encode(), http.StatusSeeOther)
}
