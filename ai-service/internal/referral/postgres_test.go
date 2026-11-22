package referral

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
	"github.com/jackc/pgx/v5/pgxpool"
)

func openDB(t *testing.T) *Postgres {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := (&knowledge.Postgres{Pool: pool}).Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE chat_referrals"); err != nil {
		t.Fatal(err)
	}
	return &Postgres{Pool: pool}
}

func TestPostgres_AddIsIdempotentAndLatestWins(t *testing.T) {
	// Arrange
	db := openDB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
	old := Referral{PageID: "111", PSID: "P", Source: "ADS", AdID: "1", AdTitle: "Old ad", ReferredAt: t0}
	newer := Referral{PageID: "111", PSID: "P", Source: "ADS", AdID: "2", AdTitle: "Workshop ad", ReferredAt: t0.Add(time.Hour)}

	// Act
	first, err1 := db.Add(ctx, []Referral{old, newer})
	again, err2 := db.Add(ctx, []Referral{old, newer}) // Facebook retried the delivery
	latest, ok, err3 := db.Latest(ctx, "111", "P")
	_, none, err4 := db.Latest(ctx, "111", "someone-else")

	// Assert
	for _, err := range []error{err1, err2, err3, err4} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if first != 2 || again != 0 {
		t.Errorf("added %d then %d; want 2 then 0", first, again)
	}
	if !ok || latest.AdID != "2" || latest.AdTitle != "Workshop ad" || !latest.ReferredAt.Equal(newer.ReferredAt) {
		t.Errorf("latest = %+v, %v; want the Workshop ad", latest, ok)
	}
	if none {
		t.Error("another customer must have no referral")
	}
}

func TestPostgres_PruneDropsOnlyOldRows(t *testing.T) {
	// Arrange
	db := openDB(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	_, err := db.Add(ctx, []Referral{
		{PageID: "111", PSID: "Old", Source: "ADS", AdID: "1", ReferredAt: now.Add(-Keep - time.Hour)},
		{PageID: "111", PSID: "New", Source: "ADS", AdID: "1", ReferredAt: now.Add(-Keep + time.Hour)},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Act
	gone, err := db.Prune(ctx, now)

	// Assert
	if err != nil || gone != 1 {
		t.Fatalf("pruned %d, %v; want 1", gone, err)
	}
	if _, ok, _ := db.Latest(ctx, "111", "New"); !ok {
		t.Error("the recent referral was deleted")
	}
}
