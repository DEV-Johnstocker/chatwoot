package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/mcp"
)

func TestConnections_PageInThai(t *testing.T) {
	// Arrange
	e := setup(t, nil)
	e.h.Tokens, e.h.MCPURL = &memTokens{tokens: []mcp.Token{{ID: 1, AccountID: 1, Name: "Claude", CanWrite: true, CreatedAt: time.Now()}}}, "https://ai-admin.example/mcp"
	r := httptest.NewRequest(http.MethodGet, "/admin/connections?account=1", nil)
	r.SetBasicAuth(Username, pw)
	r.Header.Set("Accept-Language", "th-TH,th;q=0.9")

	// Act
	body := e.do(r).Body.String()

	// Assert
	for _, want := range []string{`<html lang="th"`, "เชื่อมต่อเครื่องมือ AI", "โทเคนเข้าถึงของบัญชี 1", "อ่านและแก้ไข", "ไม่เคย", "เพิกถอน", "สร้างโทเคน", "Claude"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	for _, english := range []string{"Access tokens for account", "Read and write", "Create token", ">Revoke<", ">Never<"} {
		if strings.Contains(body, english) {
			t.Errorf("page still says %q", english)
		}
	}
}

func TestConnections_PageInEnglishUnchanged(t *testing.T) {
	// Arrange
	e := setup(t, nil)
	e.h.Tokens = &memTokens{tokens: []mcp.Token{{ID: 1, AccountID: 1, Name: "Claude", CreatedAt: time.Now()}}}
	r := httptest.NewRequest(http.MethodGet, "/admin/connections?account=1", nil)
	r.SetBasicAuth(Username, pw)

	// Act
	body := e.do(r).Body.String()

	// Assert
	for _, want := range []string{`<html lang="en"`, "Access tokens for account 1", "Read only", "Never", "Revoke the token “Claude”? Tools using it stop working at once.", "<option value=\"90\" selected>90 days</option>"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}
