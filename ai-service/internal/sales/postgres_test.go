package sales

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

func TestPostgres_SlipThenPaidThenCancel(t *testing.T) {
	// Arrange
	db := openDB(t)
	ctx := context.Background()
	slipAt := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	paidAt := time.Date(2026, 11, 10, 11, 0, 0, 0, time.UTC)

	// Act
	slipErr := db.RecordSlip(ctx, Sale{AccountID: 1, ConversationID: 52, InboxID: 2, SlipAmount: ptr(390000), SlipAt: slipAt})
	slips, _ := db.Slips(ctx, 1, 10)
	_, need1, paidErr := db.MarkPaid(ctx, Paid{AccountID: 1, ConversationID: 52, Amount: ptr(390000), At: paidAt, Source: paidAt})
	creditErr := db.SetCredit(ctx, 1, 52, CreditBot)
	_, need2, _ := db.MarkPaid(ctx, Paid{AccountID: 1, ConversationID: 52, At: paidAt.Add(time.Hour), Source: paidAt.Add(time.Hour)}) // a later label edit
	slipAgain := db.RecordSlip(ctx, Sale{AccountID: 1, ConversationID: 52, SlipAt: paidAt.Add(2 * time.Hour)})
	paid, _ := db.Paid(ctx, 1, slipAt, paidAt.Add(24*time.Hour), 10)
	slipsAfter, _ := db.Slips(ctx, 1, 10)
	cancelErr := db.Cancel(ctx, 1, 52, false, paidAt.Add(3*time.Hour))
	paidAfterCancel, _ := db.Paid(ctx, 1, slipAt, paidAt.Add(24*time.Hour), 10)

	// Assert
	if slipErr != nil || paidErr != nil || creditErr != nil || slipAgain != nil || cancelErr != nil {
		t.Fatalf("errors: %v %v %v %v %v", slipErr, paidErr, creditErr, slipAgain, cancelErr)
	}
	if len(slips) != 1 || *slips[0].SlipAmount != 390000 || slips[0].InboxID != 2 || !slips[0].SlipAt.Equal(slipAt) {
		t.Fatalf("slips = %+v", slips)
	}
	if !need1 || need2 {
		t.Fatalf("needCredit first=%v second=%v", need1, need2)
	}
	if len(paid) != 1 || *paid[0].Amount != 390000 || paid[0].Credit != CreditBot || !paid[0].PaidAt.Equal(paidAt) || paid[0].InboxID != 2 {
		t.Fatalf("paid = %+v", paid)
	}
	if len(slipsAfter) != 0 {
		t.Fatalf("a paid chat went back to slip: %+v", slipsAfter)
	}
	if len(paidAfterCancel) != 0 {
		t.Fatalf("cancelled sale still listed: %+v", paidAfterCancel)
	}
}

func TestPostgres_TotalsByCreditAndPeriodAndAccount(t *testing.T) {
	// Arrange
	db := openDB(t)
	ctx := context.Background()
	nov := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	add := func(acct, conv int64, amount *int64, at time.Time, c Credit) {
		if _, _, err := db.MarkPaid(ctx, Paid{AccountID: acct, ConversationID: conv, InboxID: 1, Amount: amount, At: at}); err != nil {
			t.Fatal(err)
		}
		if c != "" {
			if err := db.SetCredit(ctx, acct, conv, c); err != nil {
				t.Fatal(err)
			}
		}
	}
	add(1, 1, ptr(390000), nov, CreditBot)
	add(1, 2, ptr(990000), nov, CreditStaff)
	add(1, 3, nil, nov, "")                                                       // no amount, unknown credit
	add(1, 4, ptr(100), time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), CreditBot) // last month
	add(2, 1, ptr(500000), nov, CreditBot)                                        // other account

	// Act
	tot, err := db.Totals(ctx, 1, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))

	// Assert
	want := Totals{Paid: 3, Bot: 1, Staff: 1, Unknown: 1, Revenue: 1380000, BotRevenue: 390000, StaffRevenue: 990000, NoAmount: 1}
	if err != nil || tot != want {
		t.Fatalf("totals = %+v err %v, want %+v", tot, err, want)
	}
}

func TestPostgres_TableRefusesBadRows(t *testing.T) {
	// Arrange
	db := openDB(t)
	ctx := context.Background()
	insert := func(status string, amount int64) error {
		_, err := db.Pool.Exec(ctx, `INSERT INTO sales (account_id, conversation_id, status, amount_satang) VALUES (1, $1, $2, $3)`,
			time.Now().UnixNano()%1_000_000+1, status, amount)
		return err
	}

	// Act
	badStatus, negative, huge := insert("refunded", 1), insert("paid", -1), insert("paid", MaxSatang)

	// Assert
	if badStatus == nil || negative == nil || huge == nil {
		t.Fatalf("status=%v negative=%v huge=%v", badStatus, negative, huge)
	}
}

func TestPostgres_AmountRulesAndOrdering(t *testing.T) {
	// Arrange
	db := openDB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	mark := func(amount *int64, byStaff bool, source time.Time) bool {
		applied, _, err := db.MarkPaid(ctx, Paid{AccountID: 1, ConversationID: 7, Amount: amount, ByStaff: byStaff, At: source, Source: source})
		if err != nil {
			t.Fatal(err)
		}
		return applied
	}
	amount := func() int64 {
		var n int64
		if err := db.Pool.QueryRow(ctx, `SELECT amount_satang FROM sales WHERE account_id = 1 AND conversation_id = 7`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Act
	first := mark(ptr(390000), false, t0)
	mark(ptr(99999900), false, t0.Add(time.Minute)) // attribute changed later: ignored
	afterAttr := amount()
	mark(ptr(420000), true, t0.Add(2*time.Minute)) // staff note: replaces
	afterNote := amount()
	stale := mark(nil, false, t0.Add(-time.Hour)) // an old label event arriving late
	afterStale := amount()
	if err := db.Cancel(ctx, 1, 7, false, t0.Add(-time.Hour)); err != nil { // an old label removal
		t.Fatal(err)
	}
	var status string
	_ = db.Pool.QueryRow(ctx, `SELECT status FROM sales WHERE account_id = 1 AND conversation_id = 7`).Scan(&status)

	// Assert
	if !first || afterAttr != 390000 || afterNote != 420000 || stale || afterStale != 420000 || status != "paid" {
		t.Fatalf("first=%v attr=%d note=%d stale=%v afterStale=%d status=%s", first, afterAttr, afterNote, stale, afterStale, status)
	}
}

func TestPostgres_NoteAndLabelAreSeparate(t *testing.T) {
	// Arrange
	db := openDB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	status := func() string {
		var st string
		if err := db.Pool.QueryRow(ctx, `SELECT status FROM sales WHERE account_id = 1 AND conversation_id = 8`).Scan(&st); err != nil {
			t.Fatal(err)
		}
		return st
	}

	// Act
	if _, _, err := db.MarkPaid(ctx, Paid{AccountID: 1, ConversationID: 8, Amount: ptr(390000), ByStaff: true, At: t0}); err != nil {
		t.Fatal(err)
	}
	if err := db.Cancel(ctx, 1, 8, false, t0.Add(time.Hour)); err != nil { // a later label change without "paid"
		t.Fatal(err)
	}
	afterLabel := status()
	if err := db.Cancel(ctx, 1, 8, true, time.Time{}); err != nil { // #unpaid
		t.Fatal(err)
	}
	afterUnpaid := status()

	// Assert
	if afterLabel != "paid" || afterUnpaid != "cancelled" {
		t.Fatalf("after label=%s after #unpaid=%s", afterLabel, afterUnpaid)
	}
}
