package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/mcp"
)

func guideGet(auth bool) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/admin/connections/guide", nil)
	if auth {
		r.SetBasicAuth(Username, pw)
	}
	return r
}

func TestGuide_ExplainsEveryTool(t *testing.T) {
	// Arrange
	e := setup(t, nil)
	e.h.Tokens, e.h.MCPURL = &memTokens{}, "https://ai-admin.example/mcp"

	// Act
	res := e.do(guideGet(true))

	// Assert
	body := res.Body.String()
	if res.Code != http.StatusOK {
		t.Fatalf("code = %d", res.Code)
	}
	for _, name := range mcp.ToolNames() {
		if !strings.Contains(body, "<code>"+name+"</code>") {
			t.Errorf("guide does not explain tool %s", name)
		}
	}
	for _, want := range []string{"https://ai-admin.example/mcp", `href="/admin/connections"`, `aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Errorf("guide is missing %q", want)
		}
	}
}

func TestGuide_SaysWhenMCPIsOff(t *testing.T) {
	// Arrange: no token store means MCP cannot run here.
	e := setup(t, nil)

	// Act
	body := e.do(guideGet(true)).Body.String()

	// Assert
	if !strings.Contains(body, "MCP_ENABLED") {
		t.Error("guide should say how MCP is turned on when it is off")
	}
}

func TestGuide_RequiresAuth(t *testing.T) {
	// Arrange
	e := setup(t, nil)
	e.h.Tokens = &memTokens{}

	// Act
	res := e.do(guideGet(false))

	// Assert
	if res.Code != http.StatusUnauthorized || strings.Contains(res.Body.String(), "upsert_document") {
		t.Fatalf("code = %d, want 401 without the guide", res.Code)
	}
}

func TestConnections_LinksToGuide(t *testing.T) {
	// Arrange
	e := setup(t, nil)
	e.h.Tokens = &memTokens{}
	r := httptest.NewRequest(http.MethodGet, "/admin/connections?account=1", nil)
	r.SetBasicAuth(Username, pw)

	// Act
	body := e.do(r).Body.String()

	// Assert
	if !strings.Contains(body, `href="/admin/connections/guide?account=1"`) {
		t.Error("connections page should link to the MCP guide")
	}
}

func TestConnections_WarnsWhenMCPIsOff(t *testing.T) {
	// Arrange
	e := setup(t, nil)
	e.h.Tokens, e.h.MCPOff = &memTokens{}, true
	r := httptest.NewRequest(http.MethodGet, "/admin/connections?account=1", nil)
	r.SetBasicAuth(Username, pw)

	// Act
	body := e.do(r).Body.String()

	// Assert
	if !strings.Contains(body, "AI tools can't connect yet") {
		t.Error("connections page should warn that tokens do not work while MCP is off")
	}
}
