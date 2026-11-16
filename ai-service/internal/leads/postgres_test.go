package leads

import (
	"context"
	"os"
	"testing"

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
	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS kb_chunks, kb_documents, business_profiles, kb_audit, mcp_tokens, oauth_codes, oauth_grants, oauth_clients, team_members, admin_events, web_sources, chatwoot_inboxes, ai_pending_changes, fb_comments, bot_schedules, bot_images, reply_ratings, leads, sales, kb_schema_version"); err != nil {
		t.Fatal(err)
	}
	if err := (&knowledge.Postgres{Pool: pool}).Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return &Postgres{Pool: pool}
}

func TestPostgres_AskQuoteAndList(t *testing.T) {
	// Arrange
	db := openDB(t)
	ctx := context.Background()
	ann := Contact{AccountID: 1, ConversationID: 10, InboxID: 2, ContactID: 7, Name: "Ann"}
	quiet := Contact{AccountID: 1, ConversationID: 11, InboxID: 2, ContactID: 8, Name: "Bo"}
	other := Contact{AccountID: 2, ConversationID: 10, InboxID: 3, ContactID: 9, Name: "Other account"}

	// Act
	steps := []error{
		db.Customer(ctx, quiet, false), // not a lead: nothing stored
		db.Replied(ctx, 1, 11, "Course A", true),
		db.Customer(ctx, ann, true),
		db.Replied(ctx, 1, 10, "AutoCut course", true),
		db.Customer(ctx, Contact{AccountID: 1, ConversationID: 10}, true), // asks again: stage stays, name kept
		db.Replied(ctx, 1, 10, "", false),
		db.Customer(ctx, other, true),
	}
	all, errL := db.List(ctx, 1, Filter{})
	quoted, errQ := db.List(ctx, 1, Filter{Stage: StageQuoted})
	asked, errA := db.List(ctx, 1, Filter{Stage: StageAsked})
	counts, errC := db.Counts(ctx, 1)
	_, errBad := db.List(ctx, 1, Filter{Stage: "won"})

	// Assert
	for i, err := range append(steps, errL, errQ, errA, errC) {
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if len(all) != 1 {
		t.Fatalf("got %d leads, want 1 (only Ann asked): %+v", len(all), all)
	}
	got := all[0]
	if got.Source != SourceChat || got.ConversationID != 10 || got.InboxID != 2 || got.ContactID != 7 ||
		got.Name != "Ann" || got.Interest != "AutoCut course" || got.Stage != StageQuoted || got.LastAt.IsZero() {
		t.Fatalf("lead = %+v", got)
	}
	if len(quoted) != 1 || len(asked) != 0 || counts[StageQuoted] != 1 || counts[StageAsked] != 0 {
		t.Fatalf("quoted=%d asked=%d counts=%v", len(quoted), len(asked), counts)
	}
	if errBad == nil {
		t.Fatal("unknown stage filter accepted")
	}
}

func TestPostgres_CommentLeadsAndSalesStages(t *testing.T) {
	// Arrange: two chat leads, one comment lead, one non-lead comment, and
	// sales rows (schema v20).
	db := openDB(t)
	ctx := context.Background()
	for _, c := range []Contact{{AccountID: 1, ConversationID: 20, Name: "Slip"}, {AccountID: 1, ConversationID: 21, Name: "Paid"}, {AccountID: 1, ConversationID: 22, Name: "Open"}} {
		if err := db.Customer(ctx, c, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO fb_comments (account_id, page_id, comment_id, from_id, from_name, created_at, intent, reason, status) VALUES
		(1, 'p', 'c1', 'u1', 'Commenter', now(), 'lead', 'asks the price', 'waiting'),
		(1, 'p', 'c2', 'u2', 'Chatter', now(), 'other', '', 'ignored'),
		(1, 'p', 'c3', 'u3', '', now(), 'lead', '', 'removed');
		INSERT INTO sales (account_id, conversation_id, status) VALUES (1, 20, 'slip'), (1, 21, 'paid'), (1, 22, 'cancelled'), (2, 20, 'paid');`); err != nil {
		t.Fatal(err)
	}

	// Act
	all, err := db.List(ctx, 1, Filter{})
	paid, errP := db.List(ctx, 1, Filter{Stage: StagePaid})
	comments, errC := db.List(ctx, 1, Filter{Stage: StageCommented})

	// Assert
	if err != nil || errP != nil || errC != nil {
		t.Fatal(err, errP, errC)
	}
	stages := map[string]Stage{}
	for _, l := range all {
		stages[l.Name] = l.Stage
	}
	want := map[string]Stage{"Slip": StageSlip, "Paid": StagePaid, "Open": StageAsked, "Commenter": StageCommented}
	if len(stages) != len(want) {
		t.Fatalf("stages = %v, want %v", stages, want)
	}
	for n, s := range want {
		if stages[n] != s {
			t.Errorf("%s: stage %q, want %q", n, stages[n], s)
		}
	}
	if len(paid) != 1 || paid[0].Name != "Paid" {
		t.Errorf("paid = %+v", paid)
	}
	if len(comments) != 1 || comments[0].Source != SourceComment || comments[0].CommentID == 0 || comments[0].Interest != "asks the price" {
		t.Errorf("comments = %+v", comments)
	}
}
