package comments

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newWebhook(store Store) (*http.ServeMux, *Webhook) {
	h := &Webhook{AppSecret: testSecret, VerifyToken: "verify-me", PageID: testPage, AccountID: 3,
		Store: store, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	mux := http.NewServeMux()
	h.Routes(mux)
	return mux, h
}

func post(mux *http.ServeMux, body []byte, sig string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, WebhookPath, bytes.NewReader(body))
	if sig != "" {
		r.Header.Set("X-Hub-Signature-256", sig)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestWebhook_StoresASignedComment(t *testing.T) {
	// Arrange
	store := &Memory{}
	mux, _ := newWebhook(store)
	body := feedBody("add", "9999")

	// Act
	res := post(mux, body, Sign(testSecret, body))

	// Assert
	got, _ := store.Unscored(context.Background(), 10)
	if res.Code != http.StatusOK || len(got) != 1 || got[0].AccountID != 3 || got[0].CommentID != "2222_3333" {
		t.Fatalf("status %d, stored %+v", res.Code, got)
	}
}

func TestWebhook_RejectedRequestsChangeNothing(t *testing.T) {
	body := feedBody("add", "9999")
	cases := map[string]string{
		"no signature":    "",
		"wrong secret":    Sign("not-the-secret", body),
		"malformed":       "sha256=nothex",
		"other body sign": Sign(testSecret, feedBody("add", "8888")),
	}
	for name, sig := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			store := &Memory{}
			mux, _ := newWebhook(store)

			// Act
			res := post(mux, body, sig)

			// Assert
			got, _ := store.Unscored(context.Background(), 10)
			if res.Code != http.StatusUnauthorized || len(got) != 0 {
				t.Fatalf("status %d, stored %d", res.Code, len(got))
			}
		})
	}
}

func TestWebhook_ReplayStoresOnce(t *testing.T) {
	// Arrange
	store := &Memory{}
	mux, _ := newWebhook(store)
	body := feedBody("add", "9999")
	sig := Sign(testSecret, body)
	post(mux, body, sig)
	got, _ := store.Unscored(context.Background(), 10)
	_ = store.Score(context.Background(), got[0].ID, IntentLead, "asks the price", "Hi!")

	// Act
	res := post(mux, body, sig)

	// Assert: still one row, and the scored row was not reset
	row, _ := store.Get(context.Background(), 3, got[0].ID)
	unscored, _ := store.Unscored(context.Background(), 10)
	if res.Code != http.StatusOK || len(unscored) != 0 || row.Status != StatusWaiting || row.Draft != "Hi!" {
		t.Fatalf("status %d, row %+v, unscored %d", res.Code, row, len(unscored))
	}
}

func TestWebhook_DeletedCommentIsForgotten(t *testing.T) {
	// Arrange
	store := &Memory{}
	mux, _ := newWebhook(store)
	add := feedBody("add", "9999")
	post(mux, add, Sign(testSecret, add))
	remove := feedBody("remove", "9999")

	// Act
	res := post(mux, remove, Sign(testSecret, remove))

	// Assert
	got, _ := store.Unscored(context.Background(), 10)
	if res.Code != http.StatusOK || len(got) != 0 {
		t.Fatalf("status %d, stored %d", res.Code, len(got))
	}
}

func TestWebhook_InvalidJSONIsABadRequest(t *testing.T) {
	// Arrange
	mux, _ := newWebhook(&Memory{})
	body := []byte(`{"object":`)

	// Act
	res := post(mux, body, Sign(testSecret, body))

	// Assert
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status %d", res.Code)
	}
}

type failingStore struct{ Memory }

func (*failingStore) Add(context.Context, Comment) (bool, error) { return false, errors.New("db down") }

func TestWebhook_StorageFailureAsksMetaToRetry(t *testing.T) {
	// Arrange
	mux, _ := newWebhook(&failingStore{})
	body := feedBody("add", "9999")

	// Act
	res := post(mux, body, Sign(testSecret, body))

	// Assert
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", res.Code)
	}
}

func TestWebhook_Handshake(t *testing.T) {
	// Arrange
	mux, _ := newWebhook(&Memory{})
	ok := httptest.NewRequest(http.MethodGet, WebhookPath+"?hub.mode=subscribe&hub.verify_token=verify-me&hub.challenge=4242", nil)
	bad := httptest.NewRequest(http.MethodGet, WebhookPath+"?hub.mode=subscribe&hub.verify_token=guess&hub.challenge=4242", nil)
	okRes, badRes := httptest.NewRecorder(), httptest.NewRecorder()

	// Act
	mux.ServeHTTP(okRes, ok)
	mux.ServeHTTP(badRes, bad)

	// Assert
	if okRes.Code != http.StatusOK || okRes.Body.String() != "4242" || okRes.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("ok: %d %q", okRes.Code, okRes.Body.String())
	}
	if badRes.Code != http.StatusForbidden || bytes.Contains(badRes.Body.Bytes(), []byte("4242")) {
		t.Fatalf("bad: %d %q", badRes.Code, badRes.Body.String())
	}
}

func TestComment_Expired(t *testing.T) {
	// Arrange
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	fresh := Comment{CreatedAt: now.Add(-6 * 24 * time.Hour)}
	old := Comment{CreatedAt: now.Add(-7*24*time.Hour - time.Minute)}

	// Act / Assert
	if fresh.Expired(now) || !old.Expired(now) {
		t.Fatal("window wrong")
	}
}
