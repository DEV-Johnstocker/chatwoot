package admin

import (
	"strings"
	"testing"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/pending"
)

func TestApprovals_PageInThai(t *testing.T) {
	// Arrange
	e := newApprovalsEnv(t, true)
	e.propose(t, 2, pending.KindProfile, pending.ProfileTarget, map[string]string{"tone": "Warm and short."})
	e.propose(t, 2, pending.KindDocument, "hours", map[string]any{"key": "hours", "kind": "faq", "inbox_id": 7, "title": "Opening hours", "content": "9 to 5"})
	r := approvalsGet("account=2")
	r.Header.Set("Accept-Language", "th-TH,th;q=0.9")

	// Act
	body := serve(e.mux, r).Body.String()

	// Assert
	for _, want := range []string{`<html lang="th"`, "อนุมัติการแก้ไขจาก AI", "น้ำเสียงและสไตล์", "ความรู้: hours", "เอกสารใหม่", "เฉพาะกล่องข้อความ 7", "FAQ (คำถามและคำตอบ)", "ปฏิเสธ", "Opening hours", "Warm and short."} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	for _, english := range []string{"Tone and style", "Knowledge: hours", "New document", "Only inbox 7", ">Approve<", ">Reject<"} {
		if strings.Contains(body, english) {
			t.Errorf("page still says %q", english)
		}
	}
}

func TestApprovals_EmptyStateInThai(t *testing.T) {
	// Arrange
	e := newApprovalsEnv(t, true)
	r := approvalsGet("account=2")
	r.Header.Set("Accept-Language", "th")

	// Act
	body := serve(e.mux, r).Body.String()

	// Assert
	if !strings.Contains(body, "ไม่มีรายการรออนุมัติ") || strings.Contains(body, "Nothing is waiting") {
		t.Fatal("empty state not in Thai")
	}
}
