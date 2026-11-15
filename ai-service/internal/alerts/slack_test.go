package alerts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateSlackURL(t *testing.T) {
	cases := []struct {
		raw string
		ok  bool
	}{
		{"https://hooks.slack.com/services/T0/B0/xyz", true},
		{"http://hooks.slack.com/services/T0/B0/xyz", false},          // not https
		{"https://hooks.slack.com.evil.example/services/T0", false},   // look-alike host
		{"https://evil.example/services/T0/B0/xyz", false},            // other host
		{"https://user:pw@hooks.slack.com/services/T0/B0/xyz", false}, // credentials
		{"https://hooks.slack.com/", false},                           // no webhook path
		{"::not a url", false},
	}
	for _, tc := range cases {
		// Act
		err := ValidateSlackURL(tc.raw)

		// Assert
		if (err == nil) != tc.ok {
			t.Errorf("ValidateSlackURL(%q) = %v, want ok=%v", tc.raw, err, tc.ok)
		}
		if err != nil && strings.Contains(err.Error(), "xyz") {
			t.Errorf("error echoes the secret URL: %v", err)
		}
	}
}

func TestSlack_PostsTextAsJSON(t *testing.T) {
	// Arrange
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("method %s, content type %q", r.Method, r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	// Act
	err := Slack{URL: srv.URL + "/services/secret", Client: srv.Client()}.Notify(context.Background(), `a "quoted" line`)

	// Assert
	if err != nil || got["text"] != `a "quoted" line` {
		t.Errorf("err %v, posted %v", err, got)
	}
}

func TestSlack_ErrorsNeverHoldTheURL(t *testing.T) {
	// Arrange: a refused post and a redirect elsewhere
	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer refused.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://elsewhere.example/", http.StatusFound)
	}))
	defer redirect.Close()
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()

	for _, base := range []string{refused.URL, redirect.URL, closedURL} {
		// Act
		err := Slack{URL: base + "/services/TOPSECRET"}.Notify(context.Background(), "x")

		// Assert
		if err == nil {
			t.Errorf("%s: want an error", base)
			continue
		}
		if strings.Contains(err.Error(), "TOPSECRET") {
			t.Errorf("error leaks the webhook URL: %v", err)
		}
	}
}
