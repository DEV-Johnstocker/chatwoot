package admin

import "net/http"

// guideData fills the MCP guide page.
type guideData struct {
	AccountID int64
	URL       string // the MCP address AI tools connect to
	Off       bool   // /mcp is not served by this ai-service
}

// mcpOff reports whether AI tools cannot reach /mcp here: it is switched off,
// or there is no database for its tokens.
func (h *Handler) mcpOff() bool { return h.MCPOff || h.Tokens == nil }

func (h *Handler) showGuide(w http.ResponseWriter, r *http.Request) {
	d := guideData{AccountID: accountParam(r.URL.Query().Get("account")), URL: h.mcpURL(r), Off: h.mcpOff()}
	h.renderPage(w, r, http.StatusOK, "guide.html", d)
}
