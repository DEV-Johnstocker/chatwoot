package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/alerts"
)

type fakeBoard []alerts.Status

func (f fakeBoard) Active() []alerts.Status { return f }

func homeBody(t *testing.T, board AlertBoard) string {
	t.Helper()
	mux := newHandlerMux(t, &Handler{Alerts: board})
	r := httptest.NewRequest(http.MethodGet, "/admin", nil)
	r.SetBasicAuth(Username, pw)
	return serve(mux, r).Body.String()
}

func TestHome_ShowsActiveBotAlerts(t *testing.T) {
	// Arrange
	board := fakeBoard{{Kind: alerts.LLMError, Title: alerts.Titles[alerts.LLMError], Since: time.Now().Add(-10 * time.Minute)}}

	// Act
	body := homeBody(t, board)

	// Assert
	if !strings.Contains(body, `class="alert warn"`) || !strings.Contains(body, "Bot alert: The AI model is failing") {
		t.Fatalf("home lacks the alert banner")
	}
}

func TestHome_NoAlertsNoBanner(t *testing.T) {
	// Act
	quiet, none := homeBody(t, fakeBoard{}), homeBody(t, nil)

	// Assert
	if strings.Contains(quiet, "Bot alert") || strings.Contains(none, "Bot alert") {
		t.Fatal("banner shown with no active alert")
	}
}

func TestHome_AlertTitlesAreTranslated(t *testing.T) {
	for k, title := range alerts.Titles {
		// Act
		th := tr("th", title)

		// Assert
		if th == title {
			t.Errorf("%s: no Thai line for %q", k, title)
		}
	}
}
