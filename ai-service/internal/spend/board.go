package spend

import (
	"context"
	"time"
)

// Board is what /admin/spend reads and changes.
type Board struct {
	Store   Store
	Monitor *Monitor
	Prices  []Price
	Loc     *time.Location
	Now     func() time.Time
}

// Report builds the dashboard for one business, or all when accountID < 0.
func (b *Board) Report(ctx context.Context, accountID int64) (Report, error) {
	today := LocalDay(now(b.Now), b.Loc)
	rows, err := b.Store.Rows(ctx, Instant(Since(today), b.Loc), Instant(today.AddDate(0, 0, 1), b.Loc), TZ(b.Loc), accountID)
	if err != nil {
		return Report{}, err
	}
	return Build(rows, b.Prices, today), nil
}

// Caps lists the saved caps.
func (b *Board) Caps(ctx context.Context) ([]Cap, error) { return b.Store.Caps(ctx) }

// SaveCap saves a cap and checks the caps again, so the banner and any
// alert follow at once.
func (b *Board) SaveCap(ctx context.Context, c Cap) error {
	if err := b.Store.SaveCap(ctx, c); err != nil {
		return err
	}
	if b.Monitor != nil {
		if err := b.Monitor.Check(ctx); err != nil {
			b.Monitor.Logger.Warn("ai spend check after cap change failed", "account_id", c.AccountID, "error", err)
		}
	}
	return nil
}

// Warnings are the caps at 80% or more, from the last check.
func (b *Board) Warnings() []Status {
	if b.Monitor == nil {
		return nil
	}
	return b.Monitor.Warnings()
}

// Statuses are every cap's use, from the last check.
func (b *Board) Statuses() []Status {
	if b.Monitor == nil {
		return nil
	}
	s, _ := b.Monitor.Statuses()
	return s
}

// Alerting reports whether alerts reach Slack.
func (b *Board) Alerting() bool { return b.Monitor != nil && b.Monitor.Notify != nil }
