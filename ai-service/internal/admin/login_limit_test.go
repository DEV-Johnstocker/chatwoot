package admin

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// QA F-A-02: wrong /admin passwords are slowed down per client IP.

func from(r *http.Request, remote string) *http.Request {
	r.RemoteAddr = remote
	return r
}

func wrongPassword(remote string) *http.Request {
	r := get(false)
	r.SetBasicAuth(Username, "nope")
	return from(r, remote)
}

func TestLoginLimit_LocksOutAnIPAfterTooManyWrongPasswords(t *testing.T) {
	// Arrange
	e := setup(t, nil)
	for i := 0; i < loginMaxFails; i++ {
		if rec := e.do(wrongPassword("203.0.113.5:1000")); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: code %d, want 401", i+1, rec.Code)
		}
	}

	// Act
	locked := e.do(wrongPassword("203.0.113.5:1000"))
	rightButLocked := e.do(from(get(true), "203.0.113.5:2000"))
	otherIP := e.do(from(get(true), "198.51.100.7:1000"))

	// Assert
	if locked.Code != http.StatusTooManyRequests || rightButLocked.Code != http.StatusTooManyRequests {
		t.Fatalf("codes locked=%d rightButLocked=%d, want 429", locked.Code, rightButLocked.Code)
	}
	if ra, err := strconv.Atoi(locked.Header().Get("Retry-After")); err != nil || ra <= 0 {
		t.Fatalf("Retry-After = %q", locked.Header().Get("Retry-After"))
	}
	if otherIP.Code != http.StatusOK {
		t.Fatalf("another IP with the right password: code %d, want 200", otherIP.Code)
	}
}

func TestLoginLimit_LockedRequestChangesNothing(t *testing.T) {
	// Arrange: lock the IP, then send a valid save from it.
	e := setup(t, nil)
	for i := 0; i <= loginMaxFails; i++ {
		e.do(wrongPassword("203.0.113.5:1000"))
	}
	before := e.store.Get()
	form := baseForm()
	form.Set("primary_model", "changed/model")

	// Act
	rec := e.do(from(post(form), "203.0.113.5:1000"))

	// Assert
	if rec.Code != http.StatusTooManyRequests || e.store.Get().Primary.Model != before.Primary.Model || len(e.probed) != 0 {
		t.Fatalf("code=%d model=%q probed=%d", rec.Code, e.store.Get().Primary.Model, len(e.probed))
	}
}

func TestLoginLimit_SpoofedForwardHeaderDoesNotEscape(t *testing.T) {
	// Arrange: a direct (public) caller cannot pick its key with CF-Connecting-IP.
	e := setup(t, nil)
	for i := 0; i <= loginMaxFails; i++ {
		r := wrongPassword("203.0.113.5:1000")
		r.Header.Set("CF-Connecting-IP", "10.0.0."+strconv.Itoa(i+1))
		e.do(r)
	}
	r := from(get(true), "203.0.113.5:1000")
	r.Header.Set("CF-Connecting-IP", "192.0.2.99")

	// Act
	rec := e.do(r)

	// Assert
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("code %d, want 429", rec.Code)
	}
}

func TestLoginLimit_VisitorsBehindTheTunnelAreCountedApart(t *testing.T) {
	// Arrange: behind the Cloudflare tunnel every request comes from a private
	// address; the visitor is CF-Connecting-IP.
	e := setup(t, nil)
	for i := 0; i <= loginMaxFails; i++ {
		r := wrongPassword("172.18.0.3:1000")
		r.Header.Set("CF-Connecting-IP", "203.0.113.5")
		e.do(r)
	}
	owner := from(get(true), "172.18.0.3:1000")
	owner.Header.Set("CF-Connecting-IP", "198.51.100.7")

	// Act
	rec := e.do(owner)

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("owner behind the tunnel: code %d, want 200", rec.Code)
	}
}

func TestLoginLimit_PromptsWithoutPasswordDoNotCount(t *testing.T) {
	// Arrange: a browser first asks without credentials, many times.
	e := setup(t, nil)
	for i := 0; i < 3*loginMaxFails; i++ {
		e.do(from(httptest.NewRequest(http.MethodGet, "/admin/providers", nil), "203.0.113.5:1000"))
	}

	// Act
	rec := e.do(from(get(true), "203.0.113.5:1000"))

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d, want 200", rec.Code)
	}
}
