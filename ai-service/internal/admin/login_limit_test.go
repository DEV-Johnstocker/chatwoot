package admin

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
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

// Security review of F-A-02: concurrent guesses must not pass the limit.
func TestLoginLimit_ConcurrentGuessesStayWithinTheLimit(t *testing.T) {
	// Arrange
	e := setup(t, nil)
	const n = 60
	codes := make(chan int, n)
	var wg sync.WaitGroup

	// Act
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- e.do(wrongPassword("203.0.113.9:1000")).Code
		}()
	}
	wg.Wait()
	close(codes)

	// Assert
	checked := 0
	for c := range codes {
		if c == http.StatusUnauthorized {
			checked++
		}
	}
	if checked != loginMaxFails {
		t.Errorf("%d guesses reached the password check, want exactly %d", checked, loginMaxFails)
	}
}

func TestLoginLimit_IPv6CountsPerSlash64(t *testing.T) {
	// Arrange
	e := setup(t, nil)
	for i := 0; i < loginMaxFails; i++ {
		e.do(wrongPassword("[2001:db8:1:2::" + strconv.Itoa(i+1) + "]:1000"))
	}

	// Act
	sameNet := e.do(wrongPassword("[2001:db8:1:2::ffff]:1000"))
	otherNet := e.do(from(get(true), "[2001:db8:1:3::1]:1000"))

	// Assert
	if sameNet.Code != http.StatusTooManyRequests || otherNet.Code != http.StatusOK {
		t.Errorf("same /64 = %d (want 429), other /64 = %d (want 200)", sameNet.Code, otherNet.Code)
	}
}

func TestLoginLimit_FullTableStillAdmitsTheOwner(t *testing.T) {
	// Arrange: an attacker filled the table with other addresses.
	e := setup(t, nil)
	e.h.loginFails.Max = 2
	e.do(wrongPassword("203.0.113.1:1000"))
	e.do(wrongPassword("203.0.113.2:1000"))

	// Act
	owner := e.do(from(get(true), "198.51.100.30:1000"))
	guess := e.do(wrongPassword("203.0.113.3:1000"))

	// Assert
	if owner.Code != http.StatusOK || guess.Code != http.StatusUnauthorized {
		t.Errorf("owner = %d (want 200), untracked guess = %d (want 401)", owner.Code, guess.Code)
	}
}

func TestLoginLimit_RightPasswordsDoNotUseTheAllowance(t *testing.T) {
	// Arrange
	e := setup(t, nil)
	for i := 0; i < 3*loginMaxFails; i++ {
		e.do(from(get(true), "203.0.113.20:1000"))
	}

	// Act
	ok := e.do(from(get(true), "203.0.113.20:1000"))

	// Assert
	if ok.Code != http.StatusOK {
		t.Errorf("owner after many page loads = %d, want 200", ok.Code)
	}
}

func TestLoginLimit_GuessesFromManyAddressesAreSlowedForEveryone(t *testing.T) {
	// Arrange: wrong passwords spread over many addresses, one each.
	e := setup(t, nil)
	compared := 0
	for i := 0; i < 3*loginMaxFailsAll; i++ {
		if e.do(wrongPassword("198.18."+strconv.Itoa(i/250)+"."+strconv.Itoa(i%250+1)+":1000")).Code == http.StatusUnauthorized {
			compared++
		}
	}

	// Act
	newcomer := e.do(wrongPassword("192.0.2.200:1000"))

	// Assert
	if compared != loginMaxFailsAll {
		t.Fatalf("%d compared, want %d per window", compared, loginMaxFailsAll)
	}
	if newcomer.Code != http.StatusTooManyRequests || newcomer.Header().Get("Retry-After") != "60" {
		t.Fatalf("newcomer code=%d retry=%q", newcomer.Code, newcomer.Header().Get("Retry-After"))
	}
}

func TestLoginLimit_OwnerAddressGetsInDuringASpreadAttack(t *testing.T) {
	// Arrange: the owner signed in from this address before the attack.
	e := setup(t, nil)
	if rec := e.do(from(get(true), "203.0.113.9:1000")); rec.Code != http.StatusOK {
		t.Fatalf("first sign-in: %d", rec.Code)
	}
	for i := 0; i < 2*loginMaxFailsAll; i++ {
		e.do(wrongPassword("198.18.0." + strconv.Itoa(i%250+1) + ":1000"))
	}

	// Act
	owner := e.do(from(get(true), "203.0.113.9:1000"))

	// Assert
	if owner.Code != http.StatusOK {
		t.Fatalf("owner code %d, want 200", owner.Code)
	}
}

func TestLoginLimit_FullTableGuessesStillHitTheAllAddressCap(t *testing.T) {
	// Arrange: the per-address table is full, so new addresses are untracked.
	e := setup(t, nil)
	e.h.loginFails.Max = 1
	e.do(wrongPassword("203.0.113.1:1000"))
	compared := 0

	// Act
	for i := 0; i < 3*loginMaxFailsAll; i++ {
		if e.do(wrongPassword("198.18.1."+strconv.Itoa(i%250+1)+":1000")).Code == http.StatusUnauthorized {
			compared++
		}
	}

	// Assert: one slot went to the first address; the rest share the cap.
	if compared != loginMaxFailsAll-1 {
		t.Fatalf("%d untracked guesses compared, want %d", compared, loginMaxFailsAll-1)
	}
}

func TestLoginLimit_KnownGoodUntrackedAddressIsStillCapped(t *testing.T) {
	// Arrange: the owner's address (shared by an attacker behind the same
	// NAT) signed in before, and the per-address table is full.
	e := setup(t, nil)
	e.h.rememberGood("203.0.113.50")
	e.h.loginFails.Max = 1
	e.do(wrongPassword("203.0.113.1:1000"))
	compared := 0

	// Act
	for i := 0; i < 3*loginMaxFailsAll; i++ {
		if e.do(wrongPassword("203.0.113.50:1000")).Code == http.StatusUnauthorized {
			compared++
		}
	}
	owner := e.do(from(get(true), "203.0.113.50:1000"))

	// Assert
	if compared != loginMaxFailsAll-1 {
		t.Fatalf("%d guesses compared from an untracked known address, want %d", compared, loginMaxFailsAll-1)
	}
	if owner.Code != http.StatusTooManyRequests {
		t.Fatalf("owner code %d: the cap is shared while the table is full, want 429", owner.Code)
	}
}
