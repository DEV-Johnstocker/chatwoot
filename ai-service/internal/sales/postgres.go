package sales

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres stores sales in the sales table, which the knowledge base
// migration creates (schema v20).
type Postgres struct {
	Pool *pgxpool.Pool
}

// RecordSlip implements Store. A paid row keeps its status; a cancelled one
// becomes a slip again (a new payment). The first slip time is kept.
func (p *Postgres) RecordSlip(ctx context.Context, s Sale) error {
	at := s.SlipAt
	if at.IsZero() {
		at = time.Now()
	}
	_, err := p.Pool.Exec(ctx, `INSERT INTO sales (account_id, conversation_id, inbox_id, status, slip_amount_satang, slip_at, updated_at)
		VALUES ($1, $2, $3, 'slip', $4, $5, now())
		ON CONFLICT (account_id, conversation_id) DO UPDATE SET
			status = CASE WHEN sales.status = 'paid' THEN 'paid' ELSE 'slip' END,
			slip_amount_satang = COALESCE(EXCLUDED.slip_amount_satang, sales.slip_amount_satang),
			slip_at = COALESCE(sales.slip_at, EXCLUDED.slip_at),
			inbox_id = CASE WHEN sales.inbox_id = 0 THEN EXCLUDED.inbox_id ELSE sales.inbox_id END,
			updated_at = now()`,
		s.AccountID, s.ConversationID, s.InboxID, s.SlipAmount, at)
	return err
}

// MarkPaid implements Store. A label change sets label_paid, a #paid note
// note_paid; the row is paid while either is set. paid_at is the first time
// the chat was seen paid; re-marking after a cancel starts a new paid_at. A
// label change older than the stored label_at updates nothing.
func (p *Postgres) MarkPaid(ctx context.Context, c Paid) (bool, bool, error) {
	at := c.At
	if at.IsZero() {
		at = time.Now()
	}
	var labelAt *time.Time
	if !c.ByStaff {
		labelAt = nullTime(c.Source)
	}
	var credit Credit
	err := p.Pool.QueryRow(ctx, `INSERT INTO sales (account_id, conversation_id, inbox_id, status, amount_satang, paid_at,
			label_paid, note_paid, label_at, updated_at)
		VALUES ($1, $2, $3, 'paid', $4, $5, NOT $6, $6, $7, now())
		ON CONFLICT (account_id, conversation_id) DO UPDATE SET
			paid_at = CASE WHEN sales.status = 'paid' AND sales.paid_at IS NOT NULL THEN sales.paid_at ELSE EXCLUDED.paid_at END,
			status = 'paid',
			amount_satang = CASE WHEN $6 AND EXCLUDED.amount_satang IS NOT NULL THEN EXCLUDED.amount_satang
				ELSE COALESCE(sales.amount_satang, EXCLUDED.amount_satang) END,
			label_paid = sales.label_paid OR EXCLUDED.label_paid,
			note_paid = sales.note_paid OR EXCLUDED.note_paid,
			label_at = GREATEST(sales.label_at, EXCLUDED.label_at),
			inbox_id = CASE WHEN sales.inbox_id = 0 THEN EXCLUDED.inbox_id ELSE sales.inbox_id END,
			updated_at = now()
		WHERE $6 OR EXCLUDED.label_at IS NULL OR sales.label_at IS NULL OR EXCLUDED.label_at >= sales.label_at
		RETURNING credit`, c.AccountID, c.ConversationID, max(c.InboxID, 0), c.Amount, at, c.ByStaff, labelAt).Scan(&credit)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil // an older label change than the one stored
	}
	return err == nil, credit == "", err
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// SetCredit implements Store.
func (p *Postgres) SetCredit(ctx context.Context, accountID, conversationID int64, c Credit) error {
	_, err := p.Pool.Exec(ctx, `UPDATE sales SET credit = $3, updated_at = now() WHERE account_id = $1 AND conversation_id = $2`,
		accountID, conversationID, string(c))
	return err
}

// Cancel implements Store. A label removal clears label_paid and cancels
// the sale only when no #paid note keeps it paid; a #unpaid note clears both.
func (p *Postgres) Cancel(ctx context.Context, accountID, conversationID int64, byNote bool, source time.Time) error {
	var labelAt *time.Time
	if !byNote {
		labelAt = nullTime(source)
	}
	_, err := p.Pool.Exec(ctx, `UPDATE sales SET
			label_paid = false,
			note_paid = note_paid AND NOT $3,
			status = CASE WHEN note_paid AND NOT $3 THEN 'paid' ELSE 'cancelled' END,
			label_at = GREATEST(label_at, $4),
			updated_at = now()
		WHERE account_id = $1 AND conversation_id = $2 AND status = 'paid'
			AND ($3 OR $4::timestamptz IS NULL OR label_at IS NULL OR $4 >= label_at)`, accountID, conversationID, byNote, labelAt)
	return err
}

// Totals implements Store.
func (p *Postgres) Totals(ctx context.Context, accountID int64, from, to time.Time) (Totals, error) {
	var t Totals
	err := p.Pool.QueryRow(ctx, `SELECT
		count(*),
		count(*) FILTER (WHERE credit = 'bot'),
		count(*) FILTER (WHERE credit = 'staff'),
		count(*) FILTER (WHERE credit = ''),
		COALESCE(sum(amount_satang), 0),
		COALESCE(sum(amount_satang) FILTER (WHERE credit = 'bot'), 0),
		COALESCE(sum(amount_satang) FILTER (WHERE credit = 'staff'), 0),
		COALESCE(sum(amount_satang) FILTER (WHERE credit = ''), 0),
		count(*) FILTER (WHERE amount_satang IS NULL)
		FROM sales WHERE account_id = $1 AND status = 'paid' AND paid_at >= $2 AND paid_at < $3`,
		accountID, from, to).Scan(&t.Paid, &t.Bot, &t.Staff, &t.Unknown, &t.Revenue, &t.BotRevenue, &t.StaffRevenue, &t.UnknownRev, &t.NoAmount)
	return t, err
}

const columns = `account_id, conversation_id, inbox_id, status, slip_amount_satang, amount_satang, credit, slip_at, paid_at`

func scanSales(rows pgx.Rows) ([]Sale, error) {
	defer rows.Close()
	var out []Sale
	for rows.Next() {
		var s Sale
		var slipAt, paidAt *time.Time
		if err := rows.Scan(&s.AccountID, &s.ConversationID, &s.InboxID, &s.Status, &s.SlipAmount, &s.Amount, &s.Credit, &slipAt, &paidAt); err != nil {
			return nil, err
		}
		if slipAt != nil {
			s.SlipAt = *slipAt
		}
		if paidAt != nil {
			s.PaidAt = *paidAt
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Paid implements Store.
func (p *Postgres) Paid(ctx context.Context, accountID int64, from, to time.Time, limit int) ([]Sale, error) {
	rows, err := p.Pool.Query(ctx, `SELECT `+columns+` FROM sales
		WHERE account_id = $1 AND status = 'paid' AND paid_at >= $2 AND paid_at < $3
		ORDER BY paid_at DESC, id DESC LIMIT $4`, accountID, from, to, limit)
	if err != nil {
		return nil, err
	}
	return scanSales(rows)
}

// Slips implements Store.
func (p *Postgres) Slips(ctx context.Context, accountID int64, limit int) ([]Sale, error) {
	rows, err := p.Pool.Query(ctx, `SELECT `+columns+` FROM sales
		WHERE account_id = $1 AND status = 'slip' ORDER BY slip_at DESC, id DESC LIMIT $2`, accountID, limit)
	if err != nil {
		return nil, err
	}
	return scanSales(rows)
}
