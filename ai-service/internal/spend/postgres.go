package spend

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres stores usage, caps and sent alerts in the tables the knowledge
// base migration creates (schema v20).
type Postgres struct {
	Pool *pgxpool.Pool
}

// Row is the usage of one model for one purpose, business and local day.
type Row struct {
	Day       time.Time // the local date, at midnight UTC
	AccountID int64
	Purpose   string
	Model     string
	Calls     int64
	Usage     Usage
}

// Cap is the owner's spend cap for one business, or the whole service
// (AccountID 0). Zero means no cap for that period.
type Cap struct {
	AccountID int64
	Daily     Micros
	Monthly   Micros
	UpdatedBy string
	UpdatedAt time.Time
}

// Alert is one sent 80% / 100% alert.
type Alert struct {
	AccountID   int64
	Period      string // "day" or "month"
	PeriodStart time.Time
	Level       int // 80 or 100
	Cap, Spent  Micros
}

const (
	maxModel   = 200
	maxPurpose = 40
)

func clean(e Event) Event {
	e.AccountID = max(e.AccountID, 0)
	e.InputTokens, e.CachedTokens, e.OutputTokens = max(e.InputTokens, 0), max(e.CachedTokens, 0), max(e.OutputTokens, 0)
	e.AudioSeconds = max(e.AudioSeconds, 0)
	if len(e.Model) > maxModel {
		e.Model = e.Model[:maxModel]
	}
	if e.Purpose == "" || len(e.Purpose) > maxPurpose {
		e.Purpose = "other"
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	return e
}

// Insert writes events in one COPY.
func (p *Postgres) Insert(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	rows := make([][]any, len(events))
	for i, e := range events {
		e = clean(e)
		rows[i] = []any{e.AccountID, e.Purpose, e.Model, e.InputTokens, e.CachedTokens, e.OutputTokens, e.AudioSeconds, e.At}
	}
	_, err := p.Pool.CopyFrom(ctx, pgx.Identifier{"ai_usage"},
		[]string{"account_id", "purpose", "model", "input_tokens", "cached_tokens", "output_tokens", "audio_seconds", "at"},
		pgx.CopyFromRows(rows))
	return err
}

// Rows sums usage from..to (to exclusive) by local day in tz, business,
// purpose and model. accountID < 0 means every business.
func (p *Postgres) Rows(ctx context.Context, from, to time.Time, tz string, accountID int64) ([]Row, error) {
	rows, err := p.Pool.Query(ctx, `SELECT (at AT TIME ZONE $3)::date, account_id, purpose, model, count(*),
		sum(input_tokens), sum(cached_tokens), sum(output_tokens), sum(audio_seconds)
		FROM ai_usage WHERE at >= $1 AND at < $2 AND ($4 < 0 OR account_id = $4)
		GROUP BY 1, 2, 3, 4`, from, to, tz, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		var audio float64
		if err := rows.Scan(&r.Day, &r.AccountID, &r.Purpose, &r.Model, &r.Calls,
			&r.Usage.InputTokens, &r.Usage.CachedTokens, &r.Usage.OutputTokens, &audio); err != nil {
			return nil, err
		}
		r.Usage.AudioSeconds = audio
		out = append(out, r)
	}
	return out, rows.Err()
}

// Caps returns every cap, the service-wide one first.
func (p *Postgres) Caps(ctx context.Context) ([]Cap, error) {
	rows, err := p.Pool.Query(ctx, `SELECT account_id, coalesce(daily_micros, 0), coalesce(monthly_micros, 0), updated_by, updated_at
		FROM ai_spend_caps ORDER BY account_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Cap
	for rows.Next() {
		var c Cap
		if err := rows.Scan(&c.AccountID, &c.Daily, &c.Monthly, &c.UpdatedBy, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ErrBadCap is a cap the page should never send.
var ErrBadCap = errors.New("cap must be for account 0 or higher and not negative")

// SaveCap sets a cap; both periods zero removes it.
func (p *Postgres) SaveCap(ctx context.Context, c Cap) error {
	if c.AccountID < 0 || c.Daily < 0 || c.Monthly < 0 || len(c.UpdatedBy) > 200 {
		return ErrBadCap
	}
	if c.Daily == 0 && c.Monthly == 0 {
		_, err := p.Pool.Exec(ctx, `DELETE FROM ai_spend_caps WHERE account_id = $1`, c.AccountID)
		return err
	}
	_, err := p.Pool.Exec(ctx, `INSERT INTO ai_spend_caps (account_id, daily_micros, monthly_micros, updated_by, updated_at)
		VALUES ($1, nullif($2, 0), nullif($3, 0), $4, now())
		ON CONFLICT (account_id) DO UPDATE SET daily_micros = EXCLUDED.daily_micros,
		monthly_micros = EXCLUDED.monthly_micros, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		c.AccountID, int64(c.Daily), int64(c.Monthly), c.UpdatedBy)
	return err
}

// MarkAlert records a to-be-sent alert; false means it was already sent.
func (p *Postgres) MarkAlert(ctx context.Context, a Alert) (bool, error) {
	tag, err := p.Pool.Exec(ctx, `INSERT INTO ai_spend_alerts (account_id, period, period_start, level, cap_micros, spent_micros)
		VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING`,
		a.AccountID, a.Period, a.PeriodStart, a.Level, int64(a.Cap), int64(a.Spent))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// UnmarkAlert forgets an alert that could not be delivered, so the next
// check tries again.
func (p *Postgres) UnmarkAlert(ctx context.Context, a Alert) error {
	_, err := p.Pool.Exec(ctx, `DELETE FROM ai_spend_alerts
		WHERE account_id = $1 AND period = $2 AND period_start = $3 AND level = $4 AND cap_micros = $5`,
		a.AccountID, a.Period, a.PeriodStart, a.Level, int64(a.Cap))
	return err
}

// Prune deletes usage and sent alerts older than before.
func (p *Postgres) Prune(ctx context.Context, before time.Time) (int64, error) {
	tag, err := p.Pool.Exec(ctx, `DELETE FROM ai_usage WHERE at < $1`, before)
	if err != nil {
		return 0, err
	}
	if _, err := p.Pool.Exec(ctx, `DELETE FROM ai_spend_alerts WHERE sent_at < $1`, before); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
