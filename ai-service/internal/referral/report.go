package referral

import (
	"context"
	"sort"
	"strings"
	"time"
)

// A chat is matched to its referrals through chat_customers (schema v27):
// the customer's Page-scoped id that Chatwoot's webhook carries for each
// Messenger chat. Reads always start from the account's own chats, so an
// account only ever sees referrals of customers who chatted in its inboxes;
// a Page-scoped id belongs to one Page, so referrals never mix accounts.
// Issues: DEV-Johnstocker/chatwoot#1, #156.

// LinkChat records which Messenger customer a chat is with. It writes only
// when the customer changed or the row is a day old, so a busy chat does not
// write on every message.
func (p *Postgres) LinkChat(ctx context.Context, accountID, conversationID, inboxID int64, psid string) error {
	psid = clean(psid, maxIDLen)
	if accountID <= 0 || conversationID <= 0 || psid == "" {
		return nil
	}
	_, err := p.Pool.Exec(ctx, `INSERT INTO chat_customers (account_id, conversation_id, inbox_id, psid)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (account_id, conversation_id) DO UPDATE
		SET psid = EXCLUDED.psid, inbox_id = EXCLUDED.inbox_id, updated_at = now()
		WHERE chat_customers.psid <> EXCLUDED.psid OR chat_customers.updated_at < now() - interval '1 day'`,
		accountID, conversationID, max(inboxID, 0), psid)
	return err
}

// PruneChats deletes chat links not seen for Keep.
func (p *Postgres) PruneChats(ctx context.Context, now time.Time) (int64, error) {
	tag, err := p.Pool.Exec(ctx, `DELETE FROM chat_customers WHERE updated_at < $1`, now.Add(-Keep))
	return tag.RowsAffected(), err
}

// ForChats returns, for each of the account's chats, its customer's
// referrals newest first (at most perChat each).
func (p *Postgres) ForChats(ctx context.Context, accountID int64, conversationIDs []int64, perChat int) (map[int64][]Referral, error) {
	out := map[int64][]Referral{}
	if len(conversationIDs) == 0 {
		return out, nil
	}
	rows, err := p.Pool.Query(ctx, `SELECT cc.conversation_id, r.page_id, r.source, r.ref, r.ad_id, r.ad_title, r.referred_at
		FROM chat_customers cc JOIN chat_referrals r ON r.psid = cc.psid
		WHERE cc.account_id = $1 AND cc.conversation_id = ANY($2)
		ORDER BY cc.conversation_id, r.referred_at DESC, r.id DESC`, accountID, conversationIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var conv int64
		var r Referral
		if err := rows.Scan(&conv, &r.PageID, &r.Source, &r.Ref, &r.AdID, &r.AdTitle, &r.ReferredAt); err != nil {
			return nil, err
		}
		if len(out[conv]) < perChat {
			out[conv] = append(out[conv], r)
		}
	}
	return out, rows.Err()
}

// Credit says which tap gets a sale: the first or the last before payment.
type Credit string

// Credit rules.
const (
	CreditLast  Credit = "last"
	CreditFirst Credit = "first"
)

// NoSource is the report row key for paid chats with no referral.
const NoSource = "none"

// Row is one line of the per-ad report.
type Row struct {
	Key     string // "ad:<id>", "ref:<source>:<ref>" or NoSource
	Label   string // the ad's latest headline, the link's ref, or ""
	IsAd    bool
	Chats   int   // customers who tapped it in the period
	Leads   int   // of those, customers whose chat became a lead
	Paid    int   // paid chats credited to it in the period
	Revenue int64 // satang, of those paid chats
}

// Key groups referrals: one row per ad, or per link source and ref.
func (r Referral) Key() string {
	if r.IsAd() && r.AdID != "" {
		return "ad:" + r.AdID
	}
	return "ref:" + r.Source + ":" + r.Ref
}

// Report returns the account's per-ad table for taps and paid chats in
// [from, to). A sale is credited to its customer's first or last tap made
// before it was paid; paid chats without one share the NoSource row.
func (p *Postgres) Report(ctx context.Context, accountID int64, from, to time.Time, credit Credit) ([]Row, error) {
	rows := map[string]*Row{}
	get := func(r Referral) *Row {
		k := r.Key()
		row := rows[k]
		if row == nil {
			row = &Row{Key: k, IsAd: r.IsAd()}
			if row.IsAd {
				row.Label = r.AdTitle
			} else {
				row.Label = r.Ref
			}
			rows[k] = row
		}
		return row
	}
	if err := p.countTaps(ctx, accountID, from, to, get); err != nil {
		return nil, err
	}
	if err := p.creditSales(ctx, accountID, from, to, credit, rows, get); err != nil {
		return nil, err
	}
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Key == NoSource) != (b.Key == NoSource) {
			return b.Key == NoSource
		}
		if a.Revenue != b.Revenue {
			return a.Revenue > b.Revenue
		}
		if a.Chats != b.Chats {
			return a.Chats > b.Chats
		}
		return strings.Compare(a.Key, b.Key) < 0
	})
	return out, nil
}

// countTaps fills Chats and Leads: distinct customers per row, so repeat taps
// of one ad count once.
func (p *Postgres) countTaps(ctx context.Context, accountID int64, from, to time.Time, get func(Referral) *Row) error {
	rs, err := p.Pool.Query(ctx, `SELECT r.psid, r.source, r.ref, r.ad_id, r.ad_title,
			EXISTS (SELECT 1 FROM chat_customers c2 JOIN leads l
				ON l.account_id = c2.account_id AND l.conversation_id = c2.conversation_id
				WHERE c2.account_id = $1 AND c2.psid = r.psid) AS lead
		FROM chat_referrals r
		WHERE r.psid IN (SELECT psid FROM chat_customers WHERE account_id = $1)
			AND r.referred_at >= $2 AND r.referred_at < $3
		ORDER BY r.referred_at, r.id`, accountID, from, to)
	if err != nil {
		return err
	}
	defer rs.Close()
	seen := map[string]bool{}
	for rs.Next() {
		var r Referral
		var lead bool
		if err := rs.Scan(&r.PSID, &r.Source, &r.Ref, &r.AdID, &r.AdTitle, &lead); err != nil {
			return err
		}
		row := get(r)
		if row.IsAd && r.AdTitle != "" {
			row.Label = r.AdTitle // the newest headline wins
		}
		if k := row.Key + "\x00" + r.PSID; !seen[k] {
			seen[k] = true
			row.Chats++
			if lead {
				row.Leads++
			}
		}
	}
	return rs.Err()
}

// creditSales fills Paid and Revenue from paid chats in the period.
func (p *Postgres) creditSales(ctx context.Context, accountID int64, from, to time.Time, credit Credit, rows map[string]*Row, get func(Referral) *Row) error {
	order := "DESC"
	if credit == CreditFirst {
		order = "ASC"
	}
	rs, err := p.Pool.Query(ctx, `SELECT COALESCE(s.amount_satang, 0), r.source, r.ref, r.ad_id, r.ad_title
		FROM sales s
		LEFT JOIN chat_customers cc ON cc.account_id = s.account_id AND cc.conversation_id = s.conversation_id
		LEFT JOIN LATERAL (
			SELECT x.source, x.ref, x.ad_id, x.ad_title FROM chat_referrals x
			WHERE x.psid = cc.psid AND x.referred_at <= s.paid_at
			ORDER BY x.referred_at `+order+`, x.id `+order+` LIMIT 1) r ON true
		WHERE s.account_id = $1 AND s.status = 'paid' AND s.paid_at >= $2 AND s.paid_at < $3`, accountID, from, to)
	if err != nil {
		return err
	}
	defer rs.Close()
	for rs.Next() {
		var amount int64
		var src, ref, adID, title *string
		if err := rs.Scan(&amount, &src, &ref, &adID, &title); err != nil {
			return err
		}
		var row *Row
		if src == nil {
			row = rows[NoSource]
			if row == nil {
				row = &Row{Key: NoSource}
				rows[NoSource] = row
			}
		} else {
			row = get(Referral{Source: *src, Ref: *ref, AdID: *adID, AdTitle: *title})
		}
		row.Paid++
		row.Revenue += amount
	}
	return rs.Err()
}
