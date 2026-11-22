package referral

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/comments"
)

const secret = "test-messenger-secret"

const adBody = `{"object":"page","entry":[{"id":"111","messaging":[{"sender":{"id":"PSID-SECRET-9"},"recipient":{"id":"111"},"timestamp":1795400000123,"message":{"mid":"m1","text":"ข้อความลูกค้า"},"referral":{"ad_id":"6001","source":"ADS","ads_context_data":{"ad_title":"Headline-Private"}}}]}]}`

// fakeStore records saved referrals; err makes Add fail.
type fakeStore struct {
	mu    sync.Mutex
	saved []Referral
	err   error
}

func (s *fakeStore) Add(_ context.Context, rs []Referral) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return 0, s.err
	}
	s.saved = append(s.saved, rs...)
	return len(rs), nil
}

func (s *fakeStore) count() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.saved) }

// upstream is a stand-in for Chatwoot's /bot: it re-checks the signature on
// what it receives, like Chatwoot does, and records the bodies.
type upstream struct {
	mu     sync.Mutex
	bodies [][]byte
	sigOK  []bool
	proto  []string
	delay  time.Duration
	status int
}

func (u *upstream) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u.delay > 0 {
			time.Sleep(u.delay)
		}
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, r.URL.Query().Get("hub.challenge"))
			return
		}
		b, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.bodies = append(u.bodies, b)
		u.sigOK = append(u.sigOK, comments.VerifySignature(secret, r.Header.Get("X-Hub-Signature-256"), b) == nil &&
			r.Header.Get("X-Hub-Signature") == "sha1=legacy")
		u.proto = append(u.proto, r.Header.Get("X-Forwarded-Proto"))
		u.mu.Unlock()
		if u.status != 0 {
			w.WriteHeader(u.status)
			return
		}
		_, _ = io.WriteString(w, "EVENT_RECEIVED")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (u *upstream) received() int { u.mu.Lock(); defer u.mu.Unlock(); return len(u.bodies) }

func newProxy(t *testing.T, up *httptest.Server, st Store, logs *bytes.Buffer) *http.ServeMux {
	t.Helper()
	target, err := url.Parse(up.URL + "/bot")
	if err != nil {
		t.Fatal(err)
	}
	if logs == nil {
		logs = &bytes.Buffer{}
	}
	p := &Proxy{AppSecret: secret, Upstream: target, Store: st, Timeout: 300 * time.Millisecond,
		Logger: slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	mux := http.NewServeMux()
	p.Routes(mux)
	return mux
}

func signedPost(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, WebhookPath, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Hub-Signature-256", comments.Sign(secret, []byte(body)))
	r.Header.Set("X-Hub-Signature", "sha1=legacy")
	return r
}

func TestProxy_ForwardsExactBytesAndSavesReferral(t *testing.T) {
	// Arrange
	up := &upstream{}
	st := &fakeStore{}
	mux := newProxy(t, up.server(t), st, nil)
	rec := httptest.NewRecorder()

	// Act
	mux.ServeHTTP(rec, signedPost(adBody))

	// Assert
	if rec.Code != http.StatusOK || rec.Body.String() != "EVENT_RECEIVED" {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
	if up.received() != 1 || string(up.bodies[0]) != adBody || !up.sigOK[0] {
		t.Fatalf("upstream got %d bodies, identical=%v, signature ok=%v", up.received(), up.received() == 1 && string(up.bodies[0]) == adBody, up.sigOK)
	}
	if up.proto[0] != "https" {
		t.Errorf("X-Forwarded-Proto = %q, want https (Chatwoot forces SSL)", up.proto[0])
	}
	if st.count() != 1 || st.saved[0].AdID != "6001" {
		t.Errorf("saved %+v, want the ad referral", st.saved)
	}
}

func TestProxy_RejectsBadOrMissingSignatureWithoutForwarding(t *testing.T) {
	for name, sig := range map[string]string{"missing": "", "wrong": comments.Sign("other-secret", []byte(adBody)), "malformed": "sha256=zz"} {
		t.Run(name, func(t *testing.T) {
			// Arrange
			up := &upstream{}
			st := &fakeStore{}
			mux := newProxy(t, up.server(t), st, nil)
			r := signedPost(adBody)
			r.Header.Set("X-Hub-Signature-256", sig)
			rec := httptest.NewRecorder()

			// Act
			mux.ServeHTTP(rec, r)

			// Assert
			if rec.Code != http.StatusUnauthorized || up.received() != 0 || st.count() != 0 {
				t.Errorf("status %d, forwarded %d, saved %d; want 401, 0, 0", rec.Code, up.received(), st.count())
			}
		})
	}
}

func TestProxy_RejectsOversizedBody(t *testing.T) {
	// Arrange
	up := &upstream{}
	st := &fakeStore{}
	mux := newProxy(t, up.server(t), st, nil)
	big := `{"object":"page","x":"` + strings.Repeat("a", MaxBodyBytes) + `"}`
	rec := httptest.NewRecorder()

	// Act
	mux.ServeHTTP(rec, signedPost(big))

	// Assert
	if rec.Code != http.StatusRequestEntityTooLarge || up.received() != 0 || st.count() != 0 {
		t.Errorf("status %d, forwarded %d, saved %d; want 413, 0, 0", rec.Code, up.received(), st.count())
	}
}

func TestProxy_HandshakePassesThrough(t *testing.T) {
	// Arrange
	up := &upstream{}
	mux := newProxy(t, up.server(t), &fakeStore{}, nil)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, WebhookPath+"?hub.mode=subscribe&hub.verify_token=t&hub.challenge=12345", nil)

	// Act
	mux.ServeHTTP(rec, r)

	// Assert
	if rec.Code != http.StatusOK || rec.Body.String() != "12345" {
		t.Errorf("status %d body %q; want Chatwoot's challenge", rec.Code, rec.Body.String())
	}
}

func TestProxy_ChatwootTimeoutReturns502AndSavesNothing(t *testing.T) {
	// Arrange
	up := &upstream{delay: time.Second}
	st := &fakeStore{}
	mux := newProxy(t, up.server(t), st, nil)
	rec := httptest.NewRecorder()

	// Act
	mux.ServeHTTP(rec, signedPost(adBody))

	// Assert
	if rec.Code != http.StatusBadGateway || st.count() != 0 {
		t.Errorf("status %d saved %d; want 502 and nothing saved (Facebook retries)", rec.Code, st.count())
	}
}

func TestProxy_ChatwootErrorIsPassedOnAndNothingSaved(t *testing.T) {
	// Arrange
	up := &upstream{status: http.StatusInternalServerError}
	st := &fakeStore{}
	mux := newProxy(t, up.server(t), st, nil)
	rec := httptest.NewRecorder()

	// Act
	mux.ServeHTTP(rec, signedPost(adBody))

	// Assert
	if rec.Code != http.StatusInternalServerError || st.count() != 0 {
		t.Errorf("status %d saved %d; want Chatwoot's 500 and nothing saved", rec.Code, st.count())
	}
}

func TestProxy_SaveFailureStillDelivers(t *testing.T) {
	// Arrange
	up := &upstream{}
	st := &fakeStore{err: errors.New("db down")}
	mux := newProxy(t, up.server(t), st, nil)
	rec := httptest.NewRecorder()

	// Act
	mux.ServeHTTP(rec, signedPost(adBody))

	// Assert
	if rec.Code != http.StatusOK || up.received() != 1 {
		t.Errorf("status %d forwarded %d; a failed save must not block delivery", rec.Code, up.received())
	}
}

func TestProxy_LogsNoPersonalData(t *testing.T) {
	// Arrange
	up := &upstream{}
	var logs bytes.Buffer
	mux := newProxy(t, up.server(t), &fakeStore{err: errors.New("db down")}, &logs)

	// Act
	mux.ServeHTTP(httptest.NewRecorder(), signedPost(adBody))
	bad := signedPost(adBody)
	bad.Header.Set("X-Hub-Signature-256", "sha256=00")
	mux.ServeHTTP(httptest.NewRecorder(), bad)

	// Assert
	for _, private := range []string{"PSID-SECRET-9", "Headline-Private", "ข้อความลูกค้า", secret} {
		if strings.Contains(logs.String(), private) {
			t.Errorf("logs contain %q:\n%s", private, logs.String())
		}
	}
}

func TestProxy_DoesNotFollowRedirects(t *testing.T) {
	// Arrange: Chatwoot answering with a redirect (e.g. a FORCE_SSL mistake).
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Redirect(w, r, "/elsewhere", http.StatusMovedPermanently)
	}))
	t.Cleanup(srv.Close)
	st := &fakeStore{}
	mux := newProxy(t, srv, st, nil)
	rec := httptest.NewRecorder()

	// Act
	mux.ServeHTTP(rec, signedPost(adBody))

	// Assert
	if rec.Code != http.StatusMovedPermanently || hits != 1 || st.count() != 0 {
		t.Errorf("status %d, upstream hits %d, saved %d; want 301 passed back, 1 hit, nothing saved", rec.Code, hits, st.count())
	}
}
