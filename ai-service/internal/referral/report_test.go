package referral

import (
	"context"
	"testing"
	"time"
)

// seedSale inserts a paid chat (sales table, schema v20) for the report.
func seedSale(t *testing.T, db *Postgres, acct, conv int64, satang int64, paidAt time.Time) {
	t.Helper()
	_, err := db.Pool.Exec(context.Background(), `INSERT INTO sales (account_id, conversation_id, status, amount_satang, paid_at)
		VALUES ($1, $2, 'paid', $3, $4)`, acct, conv, satang, paidAt)
	if err != nil {
		t.Fatal(err)
	}
}

func seedLead(t *testing.T, db *Postgres, acct, conv int64) {
	t.Helper()
	_, err := db.Pool.Exec(context.Background(), `INSERT INTO leads (account_id, conversation_id, stage) VALUES ($1, $2, 'asked_price')`, acct, conv)
	if err != nil {
		t.Fatal(err)
	}
}

func openReportDB(t *testing.T) *Postgres {
	t.Helper()
	db := openDB(t)
	if _, err := db.Pool.Exec(context.Background(), "TRUNCATE chat_customers, sales, leads"); err != nil {
		t.Fatal(err)
	}
	return db
}

func rowByKey(rows []Row, key string) (Row, bool) {
	for _, r := range rows {
		if r.Key == key {
			return r, true
		}
	}
	return Row{}, false
}

func TestReport_CreditsFirstOrLastTapBeforePayment(t *testing.T) {
	// Arrange: Ann tapped the John Auto Cut ad, then the Workshop ad, paid,
	// then tapped a third ad after paying (gets no credit).
	db := openReportDB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 10, 3, 0, 0, 0, time.UTC)
	must(t, db.LinkChat(ctx, 1, 10, 2, "ANN"))
	_, err := db.Add(ctx, []Referral{
		{PageID: "111", PSID: "ANN", Source: "ADS", AdID: "jac", AdTitle: "John Auto Cut", ReferredAt: t0},
		{PageID: "111", PSID: "ANN", Source: "ADS", AdID: "ws", AdTitle: "Workshop", ReferredAt: t0.Add(24 * time.Hour)},
		{PageID: "111", PSID: "ANN", Source: "ADS", AdID: "late", AdTitle: "Later ad", ReferredAt: t0.Add(72 * time.Hour)},
	})
	must(t, err)
	seedLead(t, db, 1, 10)
	seedSale(t, db, 1, 10, 890000, t0.Add(48*time.Hour))
	from, to := t0.Add(-time.Hour), t0.Add(30*24*time.Hour)

	// Act
	last, errLast := db.Report(ctx, 1, from, to, CreditLast)
	first, errFirst := db.Report(ctx, 1, from, to, CreditFirst)

	// Assert
	must(t, errLast)
	must(t, errFirst)
	if r, _ := rowByKey(last, "ad:ws"); r.Paid != 1 || r.Revenue != 890000 || r.Label != "Workshop" {
		t.Errorf("last-touch Workshop row = %+v", r)
	}
	if r, _ := rowByKey(first, "ad:jac"); r.Paid != 1 || r.Revenue != 890000 {
		t.Errorf("first-touch John Auto Cut row = %+v", r)
	}
	if r, _ := rowByKey(last, "ad:late"); r.Paid != 0 || r.Chats != 1 || r.Leads != 1 {
		t.Errorf("a tap after payment must get no credit but still count as a chat: %+v", r)
	}
	if len(last) == 0 || last[0].Key != "ad:ws" {
		t.Errorf("rows not sorted by revenue: %+v", last)
	}
}

func TestReport_RepeatTapsCountOnceAndLinksAreNotAds(t *testing.T) {
	// Arrange
	db := openReportDB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 10, 3, 0, 0, 0, time.UTC)
	must(t, db.LinkChat(ctx, 1, 20, 2, "BO"))
	_, err := db.Add(ctx, []Referral{
		{PageID: "111", PSID: "BO", Source: "ADS", AdID: "ws", AdTitle: "Workshop", ReferredAt: t0},
		{PageID: "111", PSID: "BO", Source: "ADS", AdID: "ws", AdTitle: "Workshop", ReferredAt: t0.Add(time.Minute)},
		{PageID: "111", PSID: "BO", Source: "ADS", AdID: "ws", AdTitle: "Workshop", ReferredAt: t0.Add(2 * time.Minute)},
		{PageID: "111", PSID: "BO", Source: "SHORTLINK", Ref: "adtest1", ReferredAt: t0.Add(time.Hour)},
	})
	must(t, err)

	// Act
	rows, err := db.Report(ctx, 1, t0.Add(-time.Hour), t0.Add(24*time.Hour), CreditLast)

	// Assert
	must(t, err)
	if r, _ := rowByKey(rows, "ad:ws"); r.Chats != 1 || !r.IsAd {
		t.Errorf("three taps by one customer = one chat: %+v", r)
	}
	if r, ok := rowByKey(rows, "ref:SHORTLINK:adtest1"); !ok || r.IsAd || r.Label != "adtest1" {
		t.Errorf("link row = %+v (found %v)", r, ok)
	}
}

func TestReport_PaidWithoutTapAndOtherAccountsStaySeparate(t *testing.T) {
	// Arrange: account 2's customer tapped an ad; account 1 has a paid chat
	// with no tap.
	db := openReportDB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 10, 3, 0, 0, 0, time.UTC)
	must(t, db.LinkChat(ctx, 2, 30, 5, "OTHER"))
	_, err := db.Add(ctx, []Referral{{PageID: "222", PSID: "OTHER", Source: "ADS", AdID: "x", AdTitle: "Other business", ReferredAt: t0}})
	must(t, err)
	must(t, db.LinkChat(ctx, 1, 40, 2, "CAT"))
	seedSale(t, db, 1, 40, 390000, t0)
	seedSale(t, db, 1, 41, 100000, t0) // a website-chat sale with no Messenger customer

	// Act
	rows, err := db.Report(ctx, 1, t0.Add(-time.Hour), t0.Add(time.Hour), CreditLast)
	chats, err2 := db.ForChats(ctx, 1, []int64{30, 40}, 5)

	// Assert
	must(t, err)
	must(t, err2)
	if _, ok := rowByKey(rows, "ad:x"); ok {
		t.Error("account 1 sees account 2's ad")
	}
	if r, _ := rowByKey(rows, NoSource); r.Paid != 2 || r.Revenue != 490000 {
		t.Errorf("no-ad row = %+v", r)
	}
	if rows[len(rows)-1].Key != NoSource {
		t.Errorf("no-ad row not last: %+v", rows)
	}
	if len(chats[30]) != 0 {
		t.Error("ForChats returned another account's chat")
	}
}

func TestForChats_NewestFirstCapped(t *testing.T) {
	// Arrange
	db := openReportDB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 10, 3, 0, 0, 0, time.UTC)
	must(t, db.LinkChat(ctx, 1, 50, 2, "DAN"))
	must(t, db.LinkChat(ctx, 1, 50, 2, "DAN")) // repeated message: no change
	var rs []Referral
	for i := 0; i < 4; i++ {
		rs = append(rs, Referral{PageID: "111", PSID: "DAN", Source: "ADS", AdID: string(rune('a' + i)), AdTitle: "Ad", ReferredAt: t0.Add(time.Duration(i) * time.Hour)})
	}
	_, err := db.Add(ctx, rs)
	must(t, err)

	// Act
	got, err := db.ForChats(ctx, 1, []int64{50}, 3)

	// Assert
	must(t, err)
	if len(got[50]) != 3 || got[50][0].AdID != "d" || got[50][2].AdID != "b" {
		t.Errorf("got %+v; want newest three, newest first", got[50])
	}
}

func TestPruneChats_DropsOnlyOldLinks(t *testing.T) {
	// Arrange
	db := openReportDB(t)
	ctx := context.Background()
	must(t, db.LinkChat(ctx, 1, 60, 2, "OLD"))
	must(t, db.LinkChat(ctx, 1, 61, 2, "NEW"))
	_, err := db.Pool.Exec(ctx, `UPDATE chat_customers SET updated_at = now() - interval '91 days' WHERE conversation_id = 60`)
	must(t, err)

	// Act
	gone, err := db.PruneChats(ctx, time.Now())

	// Assert
	must(t, err)
	if gone != 1 {
		t.Errorf("pruned %d, want 1", gone)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
