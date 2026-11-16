package promos

import (
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
)

var now = time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)

func day(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC) }

func doc(id int64, title, content string, from, until time.Time) knowledge.Document {
	return knowledge.Document{ID: id, AccountID: 1, Title: title, Content: content, ValidFrom: from, ValidUntil: until}
}

func byID(items []Item) map[int64]Item {
	m := map[int64]Item{}
	for _, it := range items {
		m[it.Doc.ID] = it
	}
	return m
}

func TestTimeline_StatusesAndOrder(t *testing.T) {
	// Arrange
	docs := []knowledge.Document{
		doc(1, "Opening hours", "เปิด 9 โมง", time.Time{}, time.Time{}),                                  // not a promotion
		doc(2, "Black Friday", "ลดเหลือ 2,900 บาท", time.Time{}, day(20)),                                // ends in 4 days
		doc(3, "Year-end", "ลด 10% ราคา 3,510 บาท", day(1), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)), // live, ends later
		doc(4, "New year", "ราคา 3,500 บาท", day(25), time.Time{}),                                       // starts in 9 days
		doc(5, "Old promo", "ราคา 1,990 บาท", time.Time{}, day(10)),                                      // expired 6 days ago
		doc(6, "Older promo", "ราคา 1,490 บาท", time.Time{}, day(1)),                                     // expired 15 days ago
		doc(7, "Text promo", "ราคา 2,900 บาท ถึง 30 พ.ย. ตั้งแต่ 1 ธ.ค. ราคา 3,900 บาท", time.Time{}, time.Time{}),
		doc(8, "Ends tonight", "ราคา 990 บาท", time.Time{}, day(16).Add(20*time.Hour)), // 10 hours left
	}

	// Act
	items := Timeline(docs, now)

	// Assert
	var order []int64
	for _, it := range items {
		order = append(order, it.Doc.ID)
	}
	want := []int64{8, 2, 3, 7, 4, 5, 6}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
	m := byID(items)
	checks := []struct {
		id     int64
		status Status
		days   int
	}{{8, StatusEnding, 1}, {2, StatusEnding, 4}, {3, StatusLive, 46}, {7, StatusLive, -1}, {4, StatusUpcoming, 9}, {5, StatusExpired, 7}, {6, StatusExpired, 16}}
	for _, c := range checks {
		if got := m[c.id]; got.Status != c.status || got.Days != c.days {
			t.Errorf("doc %d: status %s days %d, want %s %d", c.id, got.Status, got.Days, c.status, c.days)
		}
	}
	if !m[7].DatedInText || m[2].DatedInText {
		t.Errorf("dated in text: 7=%v 2=%v", m[7].DatedInText, m[2].DatedInText)
	}
	if c := Counts(items); c[StatusEnding] != 2 || c[StatusLive] != 2 || c[StatusUpcoming] != 1 || c[StatusExpired] != 2 {
		t.Errorf("counts = %v", c)
	}
}

func TestTimeline_AfterPriceFlags(t *testing.T) {
	// Arrange
	docs := []knowledge.Document{
		doc(1, "Promo with follow-on", "ราคา 2,900 บาท", time.Time{}, day(30)),
		doc(2, "Regular price", "ราคา 3,900 บาท", day(30), time.Time{}), // starts as 1 ends: the after-price row
		doc(3, "Promo alone", "ราคา 2,900 บาท", time.Time{}, day(28)),
		doc(4, "Text, no after", "ราคา 2,900 บาท หมดเขต 30 พ.ย.", time.Time{}, time.Time{}),
		doc(5, "Text, with after", "ราคา 2,900 บาท ถึง 30 พ.ย. หลังจากนั้น 3,900 บาท", time.Time{}, time.Time{}),
		doc(6, "Coupon note", "ใช้โค้ด XMAS ได้", time.Time{}, day(30)), // no price: nothing to follow
		doc(7, "Far follow-on", "ราคา 3,900 บาท", day(30).Add(5*24*time.Hour), time.Time{}),
		doc(8, "Expired alone", "ราคา 1,990 บาท", time.Time{}, day(10)), // flagged as expired instead
	}

	// Act
	m := byID(Timeline(docs, now))

	// Assert
	want := map[int64]bool{1: false, 2: false, 3: true, 4: true, 5: false, 6: false, 7: false, 8: false}
	for id, flag := range want {
		if it, ok := m[id]; !ok || it.NoAfterPrice != flag {
			t.Errorf("doc %d: listed=%v no-after-price=%v, want %v", id, ok, it.NoAfterPrice, flag)
		}
	}
}

func TestTimeline_Empty(t *testing.T) {
	// Arrange, Act
	items := Timeline(nil, now)

	// Assert
	if len(items) != 0 {
		t.Fatalf("items = %v", items)
	}
}
