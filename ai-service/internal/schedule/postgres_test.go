package schedule

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
	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS kb_chunks, kb_documents, business_profiles, kb_audit, mcp_tokens, oauth_codes, oauth_grants, oauth_clients, team_members, admin_events, web_sources, chatwoot_inboxes, ai_pending_changes, fb_comments, bot_schedules, kb_schema_version"); err != nil {
		t.Fatal(err)
	}
	if err := (&knowledge.Postgres{Pool: pool}).Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return &Postgres{Pool: pool}
}

func TestPostgres_GetWithoutRowIsDefault(t *testing.T) {
	// Arrange
	db := openDB(t)

	// Act
	got, err := db.Get(context.Background(), 7)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != ModeOn || got.AccountID != 7 || !got.UpdatedAt.IsZero() {
		t.Errorf("got %+v, want the default (bot on)", got)
	}
}

func TestPostgres_SaveAndGetRoundTrip(t *testing.T) {
	// Arrange
	db := openDB(t)
	ctx := context.Background()
	s := Default(1)
	s.Mode = ModeScheduled
	s.Days[time.Friday] = Day{On: true, From: 18 * 60, To: 9 * 60}
	s.Days[time.Sunday] = Day{On: false, From: 0, To: 0}

	// Act
	saveErr := db.Save(ctx, s)
	got, getErr := db.Get(ctx, 1)
	other, _ := db.Get(ctx, 2)

	// Assert
	if saveErr != nil || getErr != nil {
		t.Fatal(saveErr, getErr)
	}
	if got.Mode != ModeScheduled || got.Days != s.Days || got.TimeZone != DefaultTimeZone || got.UpdatedAt.IsZero() {
		t.Errorf("got %+v, want %+v", got, s)
	}
	if other.Mode != ModeOn {
		t.Errorf("account 2 changed: %+v", other)
	}
}

func TestPostgres_SaveRejectsInvalidAndKeepsTheOldRow(t *testing.T) {
	// Arrange
	db := openDB(t)
	ctx := context.Background()
	off := Default(1)
	off.Mode = ModeOff
	if err := db.Save(ctx, off); err != nil {
		t.Fatal(err)
	}
	bad := Default(1)
	bad.Mode = "sometimes"

	// Act
	err := db.Save(ctx, bad)
	got, _ := db.Get(ctx, 1)

	// Assert
	if err == nil {
		t.Fatal("want a validation error")
	}
	if got.Mode != ModeOff {
		t.Errorf("mode = %q, want the saved off", got.Mode)
	}
}

func TestPostgres_TableRejectsUnknownMode(t *testing.T) {
	// Arrange
	db := openDB(t)

	// Act: bypass Save to test the table's own check.
	_, err := db.Pool.Exec(context.Background(),
		"INSERT INTO bot_schedules (account_id, mode) VALUES (1, 'sometimes')")

	// Assert
	if err == nil {
		t.Error("the table accepted an unknown mode")
	}
}
