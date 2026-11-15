package admin

import "github.com/DEV-Johnstocker/chatwoot/ai-service/internal/alerts"

// AlertBoard lists the bot's own problems going on now (alerts.Monitor).
type AlertBoard interface {
	Active() []alerts.Status
}

// alertView is one problem line on Home.
type alertView struct {
	Title, Since string
}

// activeAlerts lists the problems going on now, in the page's language.
func (h *Handler) activeAlerts(lang string) []alertView {
	if h.Alerts == nil {
		return nil
	}
	var out []alertView
	for _, a := range h.Alerts.Active() {
		out = append(out, alertView{Title: tr(lang, a.Title), Since: h.whenIn(lang, a.Since)})
	}
	return out
}
