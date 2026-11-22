package referral

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Keep is how long a referral is kept. psid identifies a person, so rows go
// after the same 90 days as the earlier-chat note (owner, 2026-11-21).
const Keep = 90 * 24 * time.Hour

// Postgres stores referrals in chat_referrals (schema v26).
type Postgres struct {
	Pool *pgxpool.Pool
}

// Add saves referrals, skipping any already saved (a retried delivery), and
// returns how many were new.
func (p *Postgres) Add(ctx context.Context, rs []Referral) (int, error) {
	batch := &pgx.Batch{}
	for _, r := range rs {
		batch.Queue(`INSERT INTO chat_referrals (page_id, psid, source, ref, ad_id, ad_title, referred_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT ON CONSTRAINT chat_referrals_once DO NOTHING`,
			r.PageID, r.PSID, r.Source, r.Ref, r.AdID, r.AdTitle, r.ReferredAt)
	}
	res := p.Pool.SendBatch(ctx, batch)
	defer res.Close()
	added := 0
	for range rs {
		tag, err := res.Exec()
		if err != nil {
			return added, err
		}
		added += int(tag.RowsAffected())
	}
	return added, nil
}

// Prune deletes referrals older than Keep and returns how many went.
func (p *Postgres) Prune(ctx context.Context, now time.Time) (int64, error) {
	tag, err := p.Pool.Exec(ctx, `DELETE FROM chat_referrals WHERE referred_at < $1`, now.Add(-Keep))
	return tag.RowsAffected(), err
}

// Latest returns a customer's newest referral on a Page, or false.
func (p *Postgres) Latest(ctx context.Context, pageID, psid string) (Referral, bool, error) {
	var r Referral
	err := p.Pool.QueryRow(ctx, `SELECT page_id, psid, source, ref, ad_id, ad_title, referred_at
		FROM chat_referrals WHERE page_id = $1 AND psid = $2 ORDER BY referred_at DESC, id DESC LIMIT 1`, pageID, psid).
		Scan(&r.PageID, &r.PSID, &r.Source, &r.Ref, &r.AdID, &r.AdTitle, &r.ReferredAt)
	if err == pgx.ErrNoRows {
		return Referral{}, false, nil
	}
	return r, err == nil, err
}
