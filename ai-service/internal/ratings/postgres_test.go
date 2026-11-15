package ratings

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
	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS kb_chunks, kb_documents, business_profiles, kb_audit, mcp_tokens, oauth_codes, oauth_grants, oauth_clients, team_members, admin_events, web_sources, chatwoot_inboxes, ai_pending_changes, fb_comments, bot_schedules, bot_images, reply_ratings, sales, kb_schema_version"); err != nil {
		t.Fatal(err)
	}
	if err := (&knowledge.Postgres{Pool: pool}).Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return &Postgres{Pool: pool}
}

func TestPostgres_SaveRateAgainListAndCount(t *testing.T) {
	// Arrange
	db := openDB(t)
	ctx := context.Background()
	replied := time.Date(2026, 11, 15, 2, 0, 0, 0, time.UTC)
	bad := Rating{AccountID: 1, ConversationID: 5, MessageID: 50, Reason: "missed_sale", Note: "ควรเสนอคูปอง",
		Customer: "แพงไป", Bot: "ราคา 3,900 บาทค่ะ", RatedBy: "admin password", RepliedAt: replied}
	good := Rating{AccountID: 1, ConversationID: 6, MessageID: 60, Good: true, Customer: "q", Bot: "a"}
	other := Rating{AccountID: 2, ConversationID: 6, MessageID: 60, Good: true}

	// Act
	for _, r := range []Rating{bad, good, other} {
		if err := db.Save(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	again := good
	again.Good, again.Reason, again.Bot = false, "tone", "changed text is ignored"
	if err := db.Save(ctx, again); err != nil {
		t.Fatal(err)
	}
	rated, errR := db.Rated(ctx, 1, []int64{50, 60, 70})
	isBad := false
	badOnes, errL := db.List(ctx, 1, &isBad, 10)
	g, b, errC := db.Counts(ctx, 1)

	// Assert
	if errR != nil || errL != nil || errC != nil {
		t.Fatal(errR, errL, errC)
	}
	if len(rated) != 2 || rated[50].Note != "ควรเสนอคูปอง" || !rated[50].RepliedAt.Equal(replied) {
		t.Errorf("rated = %+v", rated)
	}
	if r := rated[60]; r.Good || r.Reason != "tone" || r.Bot != "a" {
		t.Errorf("re-rating should change the verdict but keep the copied text: %+v", r)
	}
	if len(badOnes) != 2 || badOnes[0].MessageID != 60 {
		t.Errorf("bad list (newest first) = %+v", badOnes)
	}
	if g != 0 || b != 2 {
		t.Errorf("counts good=%d bad=%d", g, b)
	}
}

func TestPostgres_SaveRefusesAnInvalidRating(t *testing.T) {
	// Arrange
	db := openDB(t)

	// Act
	err := db.Save(context.Background(), Rating{AccountID: 1, ConversationID: 2, MessageID: 3})

	// Assert
	if err != ErrNoReason {
		t.Fatalf("err = %v", err)
	}
}
