package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres stores schedules in the bot_schedules table, which the knowledge
// base migration creates (schema v17).
type Postgres struct {
	Pool *pgxpool.Pool
}

// Get returns the account's schedule, or Default when none is saved.
func (s *Postgres) Get(ctx context.Context, accountID int64) (Schedule, error) {
	out := Default(accountID)
	var mode string
	var days []byte
	err := s.Pool.QueryRow(ctx, `SELECT mode, time_zone, days, updated_at
		FROM bot_schedules WHERE account_id = $1`, accountID).
		Scan(&mode, &out.TimeZone, &days, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Default(accountID), nil
	}
	if err != nil {
		return Default(accountID), err
	}
	out.Mode = Mode(mode)
	if err := decodeDays(days, &out.Days); err != nil {
		return out, fmt.Errorf("bot schedule days: %w", err)
	}
	return out, nil
}

// Save inserts or replaces the account's schedule.
func (s *Postgres) Save(ctx context.Context, sc Schedule) error {
	if err := sc.Validate(); err != nil {
		return err
	}
	days, err := json.Marshal(sc.Days[:])
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO bot_schedules (account_id, mode, time_zone, days, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (account_id) DO UPDATE SET mode = EXCLUDED.mode,
		time_zone = EXCLUDED.time_zone, days = EXCLUDED.days, updated_at = now()`,
		sc.AccountID, string(sc.Mode), sc.TimeZone, days)
	return err
}

// decodeDays fills d from a JSON array of seven days, Sunday first.
func decodeDays(raw []byte, d *[7]Day) error {
	var list []Day
	if err := json.Unmarshal(raw, &list); err != nil {
		return err
	}
	if len(list) != len(d) {
		return fmt.Errorf("want %d days, got %d", len(d), len(list))
	}
	copy(d[:], list)
	return nil
}
