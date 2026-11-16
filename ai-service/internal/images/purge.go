package images

// Expired removed pictures are erased on a timer for every account, so a
// picture removed by an owner is gone for good after RemovedKeepDays even if
// that account never removes another one (Delete only prunes its own
// account). Issue: DEV-Johnstocker/chatwoot#1

import (
	"context"
	"log/slog"
	"time"
)

// Purger erases removed copies older than RemovedKeepDays for all accounts.
type Purger interface {
	PurgeExpired(ctx context.Context) (int64, error)
}

// PurgeExpired implements Purger.
func (s *Postgres) PurgeExpired(ctx context.Context) (int64, error) {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM bot_images_removed WHERE removed_at < now() - make_interval(days => $1)`, RemovedKeepDays)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// PurgeExpired implements Purger.
func (m *Memory) PurgeExpired(_ context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff, kept, n := removedCutoff(), m.removed[:0], int64(0)
	for _, r := range m.removed {
		if r.RemovedAt.Before(cutoff) {
			n++
			continue
		}
		kept = append(kept, r)
	}
	m.removed = kept
	return n, nil
}

// RunPurge erases expired copies now and then every interval until ctx ends.
// Errors are logged and retried on the next tick.
func RunPurge(ctx context.Context, p Purger, every time.Duration, logger *slog.Logger) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if n, err := p.PurgeExpired(ctx); err != nil {
			logger.Error("removed pictures purge failed", "error", err)
		} else if n > 0 {
			logger.Info("removed pictures purged", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
