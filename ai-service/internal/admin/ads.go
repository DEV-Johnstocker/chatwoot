package admin

import (
	"context"
	"strings"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/leads"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/referral"
)

// Ads on the Leads and Sales pages: which Click-to-Messenger ad or m.me link
// a customer came from (#155 saves it, #156 shows it). Issues:
// DEV-Johnstocker/chatwoot#1, #156.

// AdSources reads referrals for the account's own chats (referral.Postgres).
type AdSources interface {
	ForChats(ctx context.Context, accountID int64, conversationIDs []int64, perChat int) (map[int64][]referral.Referral, error)
	Report(ctx context.Context, accountID int64, from, to time.Time, credit referral.Credit) ([]referral.Row, error)
}

// adsPerLead is how many ads a lead's cell names: the latest and two earlier.
// More taps are read, since repeat taps of one ad are named once.
const (
	adsPerLead = 3
	adsRead    = 20
)

// addLeadAds fills the Ad column for chat leads. A failure is logged and the
// page shows the leads without it.
func (h *Handler) addLeadAds(ctx context.Context, lang string, acct int64, list []leads.Lead, views []leadView) {
	if h.Ads == nil {
		return
	}
	var convs []int64
	for _, l := range list {
		if l.Source == leads.SourceChat && l.ConversationID > 0 {
			convs = append(convs, l.ConversationID)
		}
	}
	if len(convs) == 0 {
		return
	}
	byChat, err := h.Ads.ForChats(ctx, acct, convs, adsRead)
	if err != nil {
		h.Logger.Warn("lead ads not read", "account_id", acct, "error", err)
		return
	}
	for i, l := range list {
		refs := distinctAds(byChat[l.ConversationID], adsPerLead)
		if l.Source != leads.SourceChat || len(refs) == 0 {
			continue
		}
		views[i].Ad = adName(lang, refs[0])
		var earlier []string
		for _, r := range refs[1:] {
			earlier = append(earlier, adName(lang, r)+" ("+h.dayIn(lang, r.ReferredAt)+")")
		}
		views[i].AdEarlier = strings.Join(earlier, ", ")
	}
}

// distinctAds keeps each ad or link once (its newest tap), at most n.
func distinctAds(refs []referral.Referral, n int) []referral.Referral {
	seen := map[string]bool{}
	var out []referral.Referral
	for _, r := range refs {
		if seen[r.Key()] || len(out) == n {
			continue
		}
		seen[r.Key()] = true
		out = append(out, r)
	}
	return out
}

// adName is an ad's headline, "Ad <id>" without one, or "Link: <ref>".
func adName(lang string, r referral.Referral) string {
	switch {
	case r.IsAd() && r.AdTitle != "":
		return r.AdTitle
	case r.IsAd():
		return tr(lang, "Ad %s", r.AdID)
	case r.Ref != "":
		return tr(lang, "Link: %s", r.Ref)
	}
	return tr(lang, "Link")
}

// adRowView is one line of the Sales page's Ads table.
type adRowView struct {
	Name    string
	Kind    string // "ad", "link" or "none"
	Chats   int
	Leads   int
	Paid    int
	Revenue string
}

// creditParam reads ?credit=; anything but "first" means the last ad.
func creditParam(v string) referral.Credit {
	if v == string(referral.CreditFirst) {
		return referral.CreditFirst
	}
	return referral.CreditLast
}

// loadAds fills the Sales page's Ads table. A failure is logged and the
// section says so; the rest of the page still shows.
func (h *Handler) loadAds(ctx context.Context, lang string, data *salesData, from, to time.Time) {
	if h.Ads == nil {
		return
	}
	data.AdsOn = true
	rows, err := h.Ads.Report(ctx, data.AccountID, from, to, data.AdCredit)
	if err != nil {
		h.Logger.Warn("ad report not read", "account_id", data.AccountID, "error", err)
		data.AdsError = tr(lang, "Could not read the ads. Try again in a moment.")
		return
	}
	for _, r := range rows {
		v := adRowView{Chats: r.Chats, Leads: r.Leads, Paid: r.Paid, Revenue: baht(r.Revenue)}
		switch {
		case r.Key == referral.NoSource:
			v.Name, v.Kind = tr(lang, "No ad"), "none"
		case r.IsAd:
			v.Name, v.Kind = adName(lang, referral.Referral{Source: "ADS", AdID: strings.TrimPrefix(r.Key, "ad:"), AdTitle: r.Label}), "ad"
		default:
			v.Name, v.Kind = adName(lang, referral.Referral{Ref: r.Label}), "link"
		}
		data.AdRows = append(data.AdRows, v)
	}
}
