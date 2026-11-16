package admin

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
)

func TestPromotions_TimelineFlagsAndEditLinks(t *testing.T) {
	// Arrange
	mux, ms := leadsEnv(t, nil, nil)
	now := time.Now()
	ms.docs = []knowledge.Document{
		{ID: 1, AccountID: 3, Title: "Opening hours", Content: "เปิด 9 โมง"},
		{ID: 2, AccountID: 3, Title: "Black Friday <i>", Content: "ราคา 2,900 บาท", ValidUntil: now.Add(3 * 24 * time.Hour)},
		{ID: 3, AccountID: 3, Title: "Old promo", Content: "ราคา 1,990 บาท", ValidUntil: now.Add(-5 * 24 * time.Hour)},
		{ID: 4, AccountID: 3, Title: "New year", Content: "ราคา 3,500 บาท", ValidFrom: now.Add(9 * 24 * time.Hour)},
		{ID: 5, AccountID: 4, Title: "Other account promo", Content: "ราคา 1 บาท", ValidUntil: now.Add(24 * time.Hour)},
	}

	// Act
	rec := serve(mux, gapGet("/admin/promotions?account=3"))

	// Assert
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, body)
	}
	for _, want := range []string{"Ending within 7 days", "Expired, still stored", "Starting later", "Black Friday &lt;i&gt;",
		"No row starts when this one ends", "Expired but still stored.", "2 promotions need a look",
		`href="/admin/knowledge?account=3&amp;edit=2"`, `href="/admin/knowledge?account=3&amp;edit=4"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "Opening hours") || strings.Contains(body, "Other account promo") {
		t.Error("a non-promotion or another account's row is listed")
	}
	if strings.Index(body, "Black Friday") > strings.Index(body, "New year") || strings.Index(body, "New year") > strings.Index(body, "Old promo") {
		t.Error("groups out of order: ending, live, upcoming, expired")
	}
}

func TestPromotions_EmptyAndOff(t *testing.T) {
	// Arrange
	mux, _ := leadsEnv(t, nil, nil)
	h := &Handler{Password: pw, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	offMux := http.NewServeMux()
	h.Routes(offMux)

	// Act
	empty := serve(mux, gapGet("/admin/promotions?account=3"))
	off := serve(offMux, gapGet("/admin/promotions"))

	// Assert
	if empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), "No promotions.") {
		t.Fatalf("empty: %d", empty.Code)
	}
	if off.Code != http.StatusOK || !strings.Contains(off.Body.String(), "The promotions calendar is off.") {
		t.Fatalf("off: %d %s", off.Code, off.Body.String())
	}
}
