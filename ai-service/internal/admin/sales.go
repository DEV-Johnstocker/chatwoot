package admin

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/sales"
)

// The sales dashboard: paid chats in a period, how much they brought in, and
// how many the bot closed on its own, plus the payment slips staff still
// have to check. Rows come from the sales table (internal/sales).
// Issue: DEV-Johnstocker/chatwoot#1

const (
	salesListLimit = 100
	slipListLimit  = 50
	salesTimeout   = 10 * time.Second
)

type saleView struct {
	ConversationID int64
	InboxID        string
	When           string
	Amount         string // "" when staff did not set paid_amount
	SlipAmount     string // what the bot read off the slip; "" when none
	Credit         string // "bot", "staff" or ""
	ChatURL        string
}

type salesData struct {
	Enabled    bool
	AccountID  int64
	Period     string // "month", "last-month" or "30d"
	PeriodName string
	Totals     sales.Totals
	Revenue    string
	BotRevenue string
	StaffRev   string
	BotShare   int // percent of paid chats the bot closed (of those with a known credit)
	Sales      []saleView
	Slips      []saleView
	Full       bool // more paid chats than the list shows
	Error      string
}

func (h *Handler) showSales(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lang := langOf(r)
	data := salesData{Enabled: h.Sales != nil, AccountID: accountParam(q.Get("account")), Period: q.Get("period")}
	from, to := h.salesPeriod(&data, lang, time.Now())
	if h.Sales != nil {
		h.loadSales(r.Context(), lang, &data, from, to)
	}
	h.renderPage(w, r, http.StatusOK, "sales.html", data)
}

// salesPeriod picks [from, to) in the business time zone; unknown periods
// mean this month.
func (h *Handler) salesPeriod(data *salesData, lang string, now time.Time) (time.Time, time.Time) {
	loc := h.Location
	if loc == nil {
		loc = time.UTC
	}
	now = now.In(loc)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	switch data.Period {
	case "last-month":
		data.PeriodName = tr(lang, "Last month")
		return month.AddDate(0, -1, 0), month
	case "30d":
		data.PeriodName = tr(lang, "Last 30 days")
		return now.AddDate(0, 0, -30), now.Add(time.Minute)
	default:
		data.Period, data.PeriodName = "month", tr(lang, "This month")
		return month, month.AddDate(0, 1, 0)
	}
}

func (h *Handler) loadSales(parent context.Context, lang string, data *salesData, from, to time.Time) {
	ctx, cancel := context.WithTimeout(parent, salesTimeout)
	defer cancel()
	t, err := h.Sales.Totals(ctx, data.AccountID, from, to)
	if err == nil {
		var list, slips []sales.Sale
		if list, err = h.Sales.Paid(ctx, data.AccountID, from, to, salesListLimit); err == nil {
			slips, err = h.Sales.Slips(ctx, data.AccountID, slipListLimit)
		}
		for _, s := range list {
			data.Sales = append(data.Sales, h.toSaleView(lang, s, s.PaidAt))
		}
		for _, s := range slips {
			data.Slips = append(data.Slips, h.toSaleView(lang, s, s.SlipAt))
		}
		data.Full = len(list) >= salesListLimit
	}
	if err != nil {
		h.Logger.Error("load sales", "account_id", data.AccountID, "error", err)
		data.Error = tr(lang, "Could not load the sales. Check the database connection.")
		return
	}
	data.Totals = t
	data.Revenue, data.BotRevenue, data.StaffRev = baht(t.Revenue), baht(t.BotRevenue), baht(t.StaffRevenue)
	if known := t.Bot + t.Staff; known > 0 {
		data.BotShare = (t.Bot*100 + known/2) / known
	}
}

func (h *Handler) toSaleView(lang string, s sales.Sale, at time.Time) saleView {
	v := saleView{ConversationID: s.ConversationID, Credit: string(s.Credit), ChatURL: h.chatURL(s.AccountID, s.ConversationID)}
	if s.InboxID > 0 {
		v.InboxID = strconv.FormatInt(s.InboxID, 10)
	}
	if !at.IsZero() {
		v.When = h.whenIn(lang, at)
	}
	if s.Amount != nil {
		v.Amount = baht(*s.Amount)
	}
	if s.SlipAmount != nil {
		v.SlipAmount = baht(*s.SlipAmount)
	}
	return v
}

// baht formats satang as "฿3,900" or "฿3,900.50".
func baht(satang int64) string {
	neg := satang < 0
	if neg {
		satang = -satang
	}
	whole, frac := satang/100, satang%100
	s := strconv.FormatInt(whole, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := "฿" + b.String()
	if frac != 0 {
		out += "." + strconv.FormatInt(frac/10, 10) + strconv.FormatInt(frac%10, 10)
	}
	if neg {
		out = "-" + out
	}
	return out
}
