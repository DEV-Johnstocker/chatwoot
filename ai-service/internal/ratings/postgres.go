package ratings

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres stores ratings in reply_ratings, which the knowledge base
// migration creates (schema v19).
type Postgres struct {
	Pool *pgxpool.Pool
}

// Save inserts a rating or replaces the earlier rating of the same reply.
func (p *Postgres) Save(ctx context.Context, r Rating) error {
	if err := r.Clean(); err != nil {
		return err
	}
	rating := -1
	if r.Good {
		rating = 1
	}
	var replied *time.Time
	if !r.RepliedAt.IsZero() {
		replied = &r.RepliedAt
	}
	_, err := p.Pool.Exec(ctx, `INSERT INTO reply_ratings
		(account_id, conversation_id, message_id, rating, reason, note, customer_text, bot_text, rated_by, replied_at, rated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
		ON CONFLICT (account_id, message_id) DO UPDATE SET rating = EXCLUDED.rating,
		reason = EXCLUDED.reason, note = EXCLUDED.note, rated_by = EXCLUDED.rated_by, rated_at = now()`,
		r.AccountID, r.ConversationID, r.MessageID, rating, r.Reason, r.Note, r.Customer, r.Bot, r.RatedBy, replied)
	return err
}

const columns = `account_id, conversation_id, message_id, rating, reason, note, customer_text, bot_text, rated_by, replied_at, rated_at`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (Rating, error) {
	var r Rating
	var rating int16
	var replied *time.Time
	err := row.Scan(&r.AccountID, &r.ConversationID, &r.MessageID, &rating, &r.Reason, &r.Note,
		&r.Customer, &r.Bot, &r.RatedBy, &replied, &r.RatedAt)
	r.Good = rating > 0
	if replied != nil {
		r.RepliedAt = *replied
	}
	return r, err
}

// Rated returns the ratings of the given messages, by message ID.
func (p *Postgres) Rated(ctx context.Context, accountID int64, messageIDs []int64) (map[int64]Rating, error) {
	out := map[int64]Rating{}
	if len(messageIDs) == 0 {
		return out, nil
	}
	rows, err := p.Pool.Query(ctx, `SELECT `+columns+` FROM reply_ratings
		WHERE account_id = $1 AND message_id = ANY($2)`, accountID, messageIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out[r.MessageID] = r
	}
	return out, rows.Err()
}

// List returns ratings newest first; good nil means both kinds.
func (p *Postgres) List(ctx context.Context, accountID int64, good *bool, limit int) ([]Rating, error) {
	q := `SELECT ` + columns + ` FROM reply_ratings WHERE account_id = $1`
	args := []any{accountID, limit}
	if good != nil {
		q += ` AND rating = $3`
		v := -1
		if *good {
			v = 1
		}
		args = append(args, v)
	}
	rows, err := p.Pool.Query(ctx, q+` ORDER BY rated_at DESC, id DESC LIMIT $2`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rating
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Counts returns how many replies the account rated good and bad.
func (p *Postgres) Counts(ctx context.Context, accountID int64) (good, bad int, err error) {
	err = p.Pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE rating = 1), count(*) FILTER (WHERE rating = -1)
		FROM reply_ratings WHERE account_id = $1`, accountID).Scan(&good, &bad)
	return good, bad, err
}
