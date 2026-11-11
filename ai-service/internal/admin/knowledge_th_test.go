package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/knowledge"
)

func TestKnowledge_PageInThai(t *testing.T) {
	// Arrange
	mux, ms := knowledgeEnv(t, true)
	ms.docs = []knowledge.Document{{ID: 1, AccountID: 2, Kind: knowledge.KindText, Title: "นโยบายคืนเงิน", Content: "ไม่คืนเงิน",
		UpdatedAt: time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)}}
	r := httptest.NewRequest(http.MethodGet, "/admin/knowledge?account=2&done=deleted", nil)
	r.SetBasicAuth(Username, pw)
	r.Header.Set("Accept-Language", "th-TH,th;q=0.9")

	// Act
	body := serve(mux, r).Body.String()

	// Assert
	for _, want := range []string{`<html lang="th"`, "ฐานความรู้ · Pronto", "เพิ่ม FAQ", "คลังเอกสาร", "ทุกกล่องข้อความ",
		"ยังไม่ถูกใช้", "ลบแล้ว บอตไม่ใช้อีกต่อไป", "12 พ.ย. 2569", "นโยบายคืนเงิน"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	for _, english := range []string{"Add an FAQ", "Document library", "All inboxes", "Not used yet", "Deleted. The bot no longer uses it.", "12 Nov 2026"} {
		if strings.Contains(body, english) {
			t.Errorf("page still says %q", english)
		}
	}
}

func TestKnowledge_ErrorInThaiAndEnglish(t *testing.T) {
	// Arrange
	mux, ms := knowledgeEnv(t, true)
	form := url.Values{"action": {"add_faq"}, "account": {"2"}, "question": {"Q"}, "answer": {"A"}, "valid_from": {"31/12/2026"}}
	th := kform(form)
	th.Header.Set("Accept-Language", "th")

	// Act
	thai := serve(mux, th).Body.String()
	english := serve(mux, kform(form)).Body.String()

	// Assert
	if !strings.Contains(thai, "วันเริ่มต้องเป็นวันที่ เช่น 2026-12-31") || strings.Contains(thai, "Start date must be") {
		t.Error("Thai page does not show the date error in Thai")
	}
	if !strings.Contains(english, "Start date must be a date like 2026-12-31") {
		t.Error("English page lost the English date error")
	}
	if len(ms.docs) != 0 {
		t.Errorf("rejected form stored %d documents", len(ms.docs))
	}
}
