package leads

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres stores chat leads in the leads table (schema v23; the knowledge
// base migration creates it) and reads comment leads from fb_comments. The
// sales table's slip and paid rows (schema v20) set the stage of the
// matching chat.
type Postgres struct {
	Pool *pgxpool.Pool
}

// Customer implements Store.
func (p *Postgres) Customer(ctx context.Context, c Contact, asked bool) error {
	name := Clip(c.Name, MaxName)
	if asked {
		_, err := p.Pool.Exec(ctx, `INSERT INTO leads
			(account_id, conversation_id, inbox_id, contact_id, contact_name, stage)
			VALUES ($1, $2, $3, $4, $5, 'asked_price')
			ON CONFLICT (account_id, conversation_id) DO UPDATE SET last_contact_at = now(),
			  inbox_id = CASE WHEN EXCLUDED.inbox_id > 0 THEN EXCLUDED.inbox_id ELSE leads.inbox_id END,
			  contact_id = CASE WHEN EXCLUDED.contact_id > 0 THEN EXCLUDED.contact_id ELSE leads.contact_id END,
			  contact_name = CASE WHEN EXCLUDED.contact_name <> '' THEN EXCLUDED.contact_name ELSE leads.contact_name END`,
			c.AccountID, c.ConversationID, max(c.InboxID, 0), max(c.ContactID, 0), name)
		return err
	}
	_, err := p.Pool.Exec(ctx, `UPDATE leads SET last_contact_at = now(),
		  contact_name = CASE WHEN $3 <> '' THEN $3 ELSE contact_name END
		WHERE account_id = $1 AND conversation_id = $2`, c.AccountID, c.ConversationID, name)
	return err
}

// Replied implements Store. The stage only moves forward.
func (p *Postgres) Replied(ctx context.Context, accountID, conversationID int64, interest string, quoted bool) error {
	_, err := p.Pool.Exec(ctx, `UPDATE leads SET
		  stage = CASE WHEN $4 AND stage = 'asked_price' THEN 'quoted' ELSE stage END,
		  interest = CASE WHEN $3 <> '' THEN $3 ELSE interest END
		WHERE account_id = $1 AND conversation_id = $2`,
		accountID, conversationID, Clip(interest, MaxInterest), quoted)
	return err
}

// leadRows is every lead of account $1 with its current stage: chat leads
// (moved on to slip or paid by the sales table, schema v20) and comment leads.
const leadRows = `SELECT 'chat' AS source, l.conversation_id, 0::bigint AS comment_id, l.inbox_id, l.contact_id,
		       l.contact_name AS name, l.interest,
		       CASE s.status WHEN 'paid' THEN 'paid' WHEN 'slip' THEN
		           CASE WHEN l.stage = 'paid' THEN 'paid' ELSE 'slip' END ELSE l.stage END AS stage,
		       l.first_at, l.last_contact_at AS last_at
		FROM leads l
		LEFT JOIN sales s ON s.account_id = l.account_id AND s.conversation_id = l.conversation_id
		WHERE l.account_id = $1
		UNION ALL
		SELECT 'comment', 0, c.id, 0, 0, c.from_name, c.reason, 'commented', c.created_at, c.created_at
		FROM fb_comments c
		WHERE c.account_id = $1 AND c.intent = 'lead' AND c.status <> 'removed'`

// List implements Store.
func (p *Postgres) List(ctx context.Context, accountID int64, f Filter) ([]Lead, error) {
	if f.Stage != "" && !f.Stage.Valid() {
		return nil, fmt.Errorf("unknown stage %q", f.Stage)
	}
	limit := f.Limit
	if limit <= 0 || limit > MaxList {
		limit = MaxList
	}
	rows, err := p.Pool.Query(ctx, `SELECT source, conversation_id, comment_id, inbox_id, contact_id,
		       name, interest, stage, first_at, last_at
		FROM (`+leadRows+`) x
		WHERE ($2 = '' OR stage = $2)
		ORDER BY last_at DESC, conversation_id DESC, comment_id DESC
		LIMIT $3`, accountID, string(f.Stage), limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Lead, error) {
		l := Lead{AccountID: accountID}
		var source, stage string
		var first, last time.Time
		err := r.Scan(&source, &l.ConversationID, &l.CommentID, &l.InboxID, &l.ContactID,
			&l.Name, &l.Interest, &stage, &first, &last)
		l.Source, l.Stage, l.FirstAt, l.LastAt = Source(source), Stage(stage), first, last
		return l, err
	})
}

// Counts implements Store.
func (p *Postgres) Counts(ctx context.Context, accountID int64) (map[Stage]int, error) {
	rows, err := p.Pool.Query(ctx, `SELECT stage, count(*) FROM (`+leadRows+`) x GROUP BY stage`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[Stage]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		out[Stage(s)] = n
	}
	return out, rows.Err()
}
