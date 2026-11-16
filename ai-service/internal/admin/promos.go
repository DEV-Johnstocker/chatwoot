package admin

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/promos"
)

// The Promotions page: knowledge documents with dates (or an end written in
// the text) on a timeline, so the owner sees what the bot quotes today,
// what ends soon and what is dead. Read only; every row links to its edit
// form on the Knowledge page. Issue: DEV-Johnstocker/chatwoot#1

type promoView struct {
	ID           int64
	Title        string
	Status       promos.Status
	StatusLabel  string
	Window       string // "1 Nov 2026 – 30 Nov 2026", "Until 30 Nov 2026"
	Note         string // "Ends in 3 days", "Expired 5 days ago", ...
	DatedInText  bool
	NoAfterPrice bool
	Expired      bool
	EditURL      string
}

type promoGroup struct {
	Status promos.Status
	Title  string
	Hint   string
	Items  []promoView
}

type promosData struct {
	Enabled   bool
	AccountID int64
	Today     string
	Groups    []promoGroup
	Total     int
	Problems  int // rows with a flag
	Error     string
}

func (h *Handler) showPromos(w http.ResponseWriter, r *http.Request) {
	lang := langOf(r)
	data := promosData{Enabled: h.Knowledge != nil, AccountID: accountParam(r.URL.Query().Get("account"))}
	now := time.Now()
	data.Today = h.dayIn(lang, now)
	if h.Knowledge == nil {
		h.renderPage(w, r, http.StatusOK, "promos.html", data)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	docs, err := h.Knowledge.List(ctx, data.AccountID)
	if err != nil {
		h.Logger.Error("list promotions", "account_id", data.AccountID, "error", err)
		data.Error = tr(lang, "Could not read the knowledge base. Try again in a moment.")
	}
	items := promos.Timeline(docs, now)
	data.Total = len(items)
	for _, s := range promos.Statuses {
		g := promoGroup{Status: s, Title: tr(lang, promoGroupTitle(s)), Hint: tr(lang, promoGroupHint(s))}
		for _, it := range items {
			if it.Status != s {
				continue
			}
			v := h.toPromoView(lang, data.AccountID, it)
			if v.NoAfterPrice || v.Expired {
				data.Problems++
			}
			g.Items = append(g.Items, v)
		}
		data.Groups = append(data.Groups, g)
	}
	h.renderPage(w, r, http.StatusOK, "promos.html", data)
}

func (h *Handler) toPromoView(lang string, acct int64, it promos.Item) promoView {
	d := it.Doc
	v := promoView{ID: d.ID, Title: d.Title, Status: it.Status, StatusLabel: tr(lang, promoGroupTitle(it.Status)),
		DatedInText: it.DatedInText, NoAfterPrice: it.NoAfterPrice, Expired: it.Status == promos.StatusExpired,
		EditURL: "/admin/knowledge?account=" + strconv.FormatInt(acct, 10) + "&edit=" + strconv.FormatInt(d.ID, 10)}
	last := func() string { return h.dayIn(lang, d.ValidUntil.Add(-time.Nanosecond)) }
	switch {
	case !d.ValidFrom.IsZero() && !d.ValidUntil.IsZero():
		v.Window = h.dayIn(lang, d.ValidFrom) + " – " + last()
	case !d.ValidUntil.IsZero():
		v.Window = tr(lang, "Until %s", last())
	case !d.ValidFrom.IsZero():
		v.Window = tr(lang, "From %s", h.dayIn(lang, d.ValidFrom))
	default:
		v.Window = tr(lang, "End date written in the text only")
	}
	switch it.Status {
	case promos.StatusEnding:
		if it.Days <= 1 {
			v.Note = tr(lang, "Last day today")
		} else {
			v.Note = tr(lang, "Ends in %d days", it.Days)
		}
	case promos.StatusLive:
		if it.Days > 0 {
			v.Note = tr(lang, "Ends in %d days", it.Days)
		}
	case promos.StatusUpcoming:
		v.Note = tr(lang, "Starts in %d days", max(it.Days, 1))
	case promos.StatusExpired:
		v.Note = tr(lang, "Ended %d days ago", max(it.Days, 1))
	}
	return v
}

func promoGroupTitle(s promos.Status) string {
	switch s {
	case promos.StatusEnding:
		return "Ending within 7 days"
	case promos.StatusLive:
		return "Live now"
	case promos.StatusUpcoming:
		return "Starting later"
	case promos.StatusExpired:
		return "Expired, still stored"
	}
	return string(s)
}

func promoGroupHint(s promos.Status) string {
	switch s {
	case promos.StatusEnding:
		return "The bot quotes these today. Check the price for after they end is ready."
	case promos.StatusLive:
		return "The bot quotes these today."
	case promos.StatusUpcoming:
		return "The bot does not see these until their first day."
	case promos.StatusExpired:
		return "The bot no longer sees these. Delete them, or give them new dates, so the list stays clean."
	}
	return ""
}
