package spend_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/spend"
	"github.com/jackc/pgx/v5/pgxpool"
)

func openDB(t *testing.T) *spend.Postgres {
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
	if _, err := pool.Exec(ctx, "TRUNCATE ai_usage, ai_spend_caps, ai_spend_alerts"); err != nil {
		t.Fatal(err)
	}
	return &spend.Postgres{Pool: pool}
}

func TestPostgres_InsertAndRowsByLocalDay(t *testing.T) {
	// Arrange: 23:30 UTC on 15 Nov is 16 Nov in Bangkok
	db := openDB(t)
	ctx := context.Background()
	late := time.Date(2026, 11, 15, 23, 30, 0, 0, time.UTC)
	events := []spend.Event{
		{AccountID: 1, Purpose: spend.PurposeReply, Model: "gpt-6-luna", InputTokens: 100, CachedTokens: 40, OutputTokens: 10, At: late},
		{AccountID: 1, Purpose: spend.PurposeReply, Model: "gpt-6-luna", InputTokens: 50, OutputTokens: 5, At: late.Add(time.Minute)},
		{AccountID: 2, Purpose: spend.PurposeTranscription, Model: "whisper-1", AudioSeconds: 12.5, At: late},
		{AccountID: -5, Purpose: "", Model: "m", InputTokens: -3, At: late}, // cleaned, not refused
	}

	// Act
	err := db.Insert(ctx, events)
	all, allErr := db.Rows(ctx, late.Add(-time.Hour), late.Add(time.Hour), "Asia/Bangkok", -1)
	one, oneErr := db.Rows(ctx, late.Add(-time.Hour), late.Add(time.Hour), "Asia/Bangkok", 1)
	none, noneErr := db.Rows(ctx, late.Add(time.Hour), late.Add(2*time.Hour), "Asia/Bangkok", -1)

	// Assert
	if err != nil || allErr != nil || oneErr != nil || noneErr != nil {
		t.Fatal(err, allErr, oneErr, noneErr)
	}
	if len(all) != 3 || len(one) != 1 || len(none) != 0 {
		t.Fatalf("all=%+v one=%+v none=%+v", all, one, none)
	}
	r := one[0]
	if !r.Day.Equal(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)) || r.Calls != 2 || r.Usage.InputTokens != 150 ||
		r.Usage.CachedTokens != 40 || r.Usage.OutputTokens != 15 {
		t.Fatalf("row = %+v", r)
	}
	for _, x := range all {
		if x.Model == "m" && (x.AccountID != 0 || x.Purpose != "other" || x.Usage.InputTokens != 0) {
			t.Fatalf("bad event not cleaned: %+v", x)
		}
		if x.Model == "whisper-1" && x.Usage.AudioSeconds != 12.5 {
			t.Fatalf("audio = %v", x.Usage.AudioSeconds)
		}
	}
}

func TestPostgres_CapsSaveReplaceAndRemove(t *testing.T) {
	// Arrange
	db := openDB(t)
	ctx := context.Background()

	// Act
	e1 := db.SaveCap(ctx, spend.Cap{AccountID: 0, Daily: 5_000_000, UpdatedBy: "owner@example.com"})
	e2 := db.SaveCap(ctx, spend.Cap{AccountID: 2, Monthly: 50_000_000})
	e3 := db.SaveCap(ctx, spend.Cap{AccountID: 0, Monthly: 90_000_000}) // replaces: daily removed
	e4 := db.SaveCap(ctx, spend.Cap{AccountID: 2})                      // both zero: removed
	bad := db.SaveCap(ctx, spend.Cap{AccountID: -1, Daily: 1})
	neg := db.SaveCap(ctx, spend.Cap{AccountID: 3, Daily: -1})
	caps, err := db.Caps(ctx)

	// Assert
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || err != nil || bad == nil || neg == nil {
		t.Fatal(e1, e2, e3, e4, err, bad, neg)
	}
	if len(caps) != 1 || caps[0].AccountID != 0 || caps[0].Daily != 0 || caps[0].Monthly != 90_000_000 || caps[0].UpdatedBy != "" {
		t.Fatalf("caps = %+v", caps)
	}
}

func TestPostgres_AlertMarkedOnceAndUnmark(t *testing.T) {
	// Arrange
	db := openDB(t)
	ctx := context.Background()
	a := spend.Alert{AccountID: 0, Period: spend.PeriodDay, PeriodStart: time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC), Level: 80, Cap: 1_000_000, Spent: 850_000}
	raised := a
	raised.Cap = 2_000_000 // the owner raised the cap: a new alert at the new cap

	// Act
	first, e1 := db.MarkAlert(ctx, a)
	again, e2 := db.MarkAlert(ctx, a)
	other, e3 := db.MarkAlert(ctx, raised)
	e4 := db.UnmarkAlert(ctx, a)
	retry, e5 := db.MarkAlert(ctx, a)

	// Assert
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil {
		t.Fatal(e1, e2, e3, e4, e5)
	}
	if !first || again || !other || !retry {
		t.Fatalf("first=%v again=%v other=%v retry=%v", first, again, other, retry)
	}
}

func TestPostgres_PruneKeepsRecent(t *testing.T) {
	// Arrange
	db := openDB(t)
	ctx := context.Background()
	now := time.Now()
	if err := db.Insert(ctx, []spend.Event{
		{Purpose: "reply", Model: "m", At: now.Add(-500 * 24 * time.Hour)},
		{Purpose: "reply", Model: "m", At: now},
	}); err != nil {
		t.Fatal(err)
	}

	// Act
	n, err := db.Prune(ctx, now.Add(-spend.Retention))
	left, _ := db.Rows(ctx, now.Add(-600*24*time.Hour), now.Add(time.Hour), "UTC", -1)

	// Assert
	if err != nil || n != 1 || len(left) != 1 {
		t.Fatalf("n=%d err=%v left=%+v", n, err, left)
	}
}

func TestWriter_FlushesOnCloseAndDropsAfter(t *testing.T) {
	// Arrange
	db := openDB(t)
	w := spend.NewWriter(db, slog.New(slog.NewTextHandler(io.Discard, nil)), 10)
	w.FlushEvery = time.Hour // only Close flushes
	w.Start()

	// Act
	for range 3 {
		w.Record(spend.Event{AccountID: 4, Purpose: "reply", Model: "m", At: time.Now()})
	}
	w.Close()
	w.Record(spend.Event{AccountID: 4, Purpose: "reply", Model: "m"}) // after Close: ignored, no panic
	w.Close()                                                         // twice is fine
	rows, err := db.Rows(context.Background(), time.Now().Add(-time.Hour), time.Now().Add(time.Hour), "UTC", 4)

	// Assert
	if err != nil || len(rows) != 1 || rows[0].Calls != 3 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}
