package spend

import (
	"testing"
	"time"
)

var testPrices = []Price{
	{Model: "chat", From: day("2026-01-01"), Input: 1, Output: 2}, // $1 / $2 per 1M
	{Model: "emb", From: day("2026-01-01"), Input: 0.5},
}

func TestBuild_TodayMonthBreakdownAndTrend(t *testing.T) {
	// Arrange: today is 16 Nov; one row last month, rows this month and today
	today := day("2026-11-16")
	rows := []Row{
		{Day: day("2026-10-31"), AccountID: 1, Purpose: PurposeReply, Model: "chat", Calls: 1, Usage: Usage{InputTokens: 1_000_000}},
		{Day: day("2026-11-02"), AccountID: 1, Purpose: PurposeReply, Model: "openai/chat", Calls: 2, Usage: Usage{InputTokens: 2_000_000}},
		{Day: today, AccountID: 2, Purpose: PurposeReply, Model: "chat", Calls: 3, Usage: Usage{OutputTokens: 1_000_000}},
		{Day: today, AccountID: 2, Purpose: PurposeEmbedding, Model: "emb", Calls: 4, Usage: Usage{InputTokens: 2_000_000}},
		{Day: today, AccountID: 2, Purpose: PurposeNote, Model: "mystery", Calls: 1, Usage: Usage{InputTokens: 5}},
	}

	// Act
	rep := Build(rows, testPrices, today)

	// Assert
	if rep.Today != 3_000_000 || rep.Month != 5_000_000 || rep.TodayCalls != 8 || rep.MonthCalls != 10 {
		t.Fatalf("today=%d month=%d calls=%d/%d", rep.Today, rep.Month, rep.TodayCalls, rep.MonthCalls)
	}
	if len(rep.Days) != TrendDays || !rep.Days[0].Day.Equal(day("2026-10-18")) || rep.Days[13].Cost != 1_000_000 || rep.Days[29].Cost != 3_000_000 {
		t.Fatalf("days=%d first=%v oct31=%d today=%d", len(rep.Days), rep.Days[0].Day, rep.Days[13].Cost, rep.Days[29].Cost)
	}
	if rep.ByPurpose[0].Name != PurposeReply || rep.ByPurpose[0].Cost != 4_000_000 || rep.ByPurpose[0].Calls != 5 {
		t.Fatalf("by purpose = %+v", rep.ByPurpose)
	}
	if len(rep.Unpriced) != 1 || rep.Unpriced[0] != "mystery" {
		t.Fatalf("unpriced = %v", rep.Unpriced)
	}
	for _, l := range rep.ByModel {
		if l.Name == "mystery" && l.Priced {
			t.Fatal("unknown model marked priced")
		}
	}
}

func TestStatuses_ScopeAndLevels(t *testing.T) {
	// Arrange: account 2 spent $3 today ($1 earlier this month); account 1 $0.50
	today := day("2026-11-16")
	rows := []Row{
		{Day: today, AccountID: 2, Model: "chat", Usage: Usage{InputTokens: 3_000_000}},
		{Day: day("2026-11-01"), AccountID: 2, Model: "chat", Usage: Usage{InputTokens: 1_000_000}},
		{Day: today, AccountID: 1, Model: "chat", Usage: Usage{InputTokens: 500_000}},
	}
	caps := []Cap{
		{AccountID: 0, Daily: 4_000_000},                      // service: $3.50 of $4 = 87%
		{AccountID: 2, Daily: 3_000_000, Monthly: 10_000_000}, // 100% day, 40% month
		{AccountID: 1, Monthly: 1_000_000},                    // 50%
	}

	// Act
	got := Statuses(caps, rows, testPrices, today)

	// Assert
	want := []struct {
		acct           int64
		period         string
		percent, level int
	}{{0, PeriodDay, 87, 80}, {2, PeriodDay, 100, 100}, {2, PeriodMonth, 40, 0}, {1, PeriodMonth, 50, 0}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, w := range want {
		g := got[i]
		if g.AccountID != w.acct || g.Period != w.period || g.Percent != w.percent || g.Level != w.level {
			t.Errorf("status %d = %+v, want %+v", i, g, w)
		}
	}
	if !got[2].PeriodStart.Equal(day("2026-11-01")) || !got[1].PeriodStart.Equal(today) {
		t.Errorf("period starts %v %v", got[1].PeriodStart, got[2].PeriodStart)
	}
}

func TestLocalDay_UsesBusinessTimeZone(t *testing.T) {
	// Arrange: 20:00 UTC on 15 Nov is 03:00 on 16 Nov in Bangkok
	bkk, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		t.Skip("no tzdata")
	}
	at := time.Date(2026, 11, 15, 20, 0, 0, 0, time.UTC)

	// Act
	d := LocalDay(at, bkk)
	start := Instant(d, bkk)

	// Assert
	if !d.Equal(day("2026-11-16")) || !start.Equal(time.Date(2026, 11, 15, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("day=%v start=%v", d, start.UTC())
	}
}

func TestSince_CoversMonthAndTrend(t *testing.T) {
	// Arrange / Act / Assert
	if got := Since(day("2026-11-16")); !got.Equal(day("2026-10-18")) {
		t.Errorf("mid-month since = %v", got)
	}
	if got := Since(day("2026-12-31")); !got.Equal(day("2026-12-01")) {
		t.Errorf("month-end since = %v", got)
	}
}

func TestMoney(t *testing.T) {
	cases := map[Micros]string{0: "$0.00", 1_234_567: "$1.23", 1_500: "$0.0015", 10_000: "$0.01"}
	for in, want := range cases {
		if got := Money(in); got != want {
			t.Errorf("Money(%d) = %q, want %q", in, got, want)
		}
	}
}
