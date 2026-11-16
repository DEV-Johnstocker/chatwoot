package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Moving between /admin pages keeps the account the owner is working on.
// Issue: DEV-Johnstocker/chatwoot#1

func accountCookieOf(res *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range res.Result().Cookies() {
		if c.Name == accountCookie {
			return c
		}
	}
	return nil
}

func TestAccount_OpeningAPageRemembersItsAccount(t *testing.T) {
	// Arrange
	e := nudgeEnv(t, true)

	// Act
	res := serve(e.mux, hget("/admin/nudge?account=3"))

	// Assert
	c := accountCookieOf(res)
	if c == nil || c.Value != "3" || c.Path != "/admin" || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.MaxAge <= 0 {
		t.Fatalf("cookie = %+v, want admin_account=3, /admin, HttpOnly, SameSite=Strict, long-lived", c)
	}
}

func TestAccount_PageWithoutAccountShowsTheRememberedOne(t *testing.T) {
	// Arrange
	e := nudgeEnv(t, true)
	r := hget("/admin/hours")
	r.AddCookie(&http.Cookie{Name: accountCookie, Value: "3"})

	// Act
	res := serve(e.mux, r)
	body := res.Body.String()

	// Assert
	if res.Code != http.StatusOK {
		t.Fatalf("status %d", res.Code)
	}
	for _, want := range []string{`href="/admin/hours?account=3" aria-current="page"`, `href="/admin/nudge?account=3"`, `name="account" value="3"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if accountCookieOf(res) != nil {
		t.Error("remembered account was set again without a change")
	}
}

func TestAccount_BadCookieFallsBackToAccountOne(t *testing.T) {
	for _, bad := range []string{"abc", "0", "-2", "3;x", `3"><script>`} {
		// Arrange
		e := nudgeEnv(t, true)
		r := hget("/admin/hours")
		r.AddCookie(&http.Cookie{Name: accountCookie, Value: bad})

		// Act
		body := serve(e.mux, r).Body.String()

		// Assert
		if !strings.Contains(body, `href="/admin/hours?account=1" aria-current="page"`) || strings.Contains(body, "<script>") {
			t.Errorf("cookie %q: not account 1, or cookie text reached the page", bad)
		}
	}
}

func TestAccount_InvalidAccountParamIsNotRemembered(t *testing.T) {
	// Arrange
	e := nudgeEnv(t, true)
	r := hget("/admin/hours?account=abc")
	r.AddCookie(&http.Cookie{Name: accountCookie, Value: "3"})

	// Act
	res := serve(e.mux, r)

	// Assert
	if accountCookieOf(res) != nil {
		t.Error("an invalid ?account= replaced the remembered account")
	}
}

func TestAccount_SpendAndProvidersStayAccountFree(t *testing.T) {
	// Arrange
	e := nudgeEnv(t, true)
	r := hget("/admin/hours?account=3")

	// Act
	body := serve(e.mux, r).Body.String()

	// Assert
	for _, want := range []string{`<a href="/admin/spend">`, `<a href="/admin/providers">`} {
		if !strings.Contains(body, want) {
			t.Errorf("menu lacks %q: spend without an account must keep showing every business", want)
		}
	}
}

func TestAccount_SwitchFromASharedPageGoesHome(t *testing.T) {
	// Arrange / Act / Assert
	if got := navSwitchHref("spend", "4"); got != "/admin?account=4" {
		t.Errorf("switch from spend = %q", got)
	}
	if got := navSwitchHref("leads", "4"); got != "/admin/leads?account=4" {
		t.Errorf("switch from leads = %q", got)
	}
	if got := navSwitchAction("providers"); got != "/admin" {
		t.Errorf("switch form from providers = %q", got)
	}
}

func TestAccount_WrongPasswordRemembersNothing(t *testing.T) {
	// Arrange
	e := nudgeEnv(t, true)
	r := httptest.NewRequest(http.MethodGet, "/admin/hours?account=5", nil)
	r.SetBasicAuth(Username, "wrong-password")

	// Act
	res := serve(e.mux, r)

	// Assert
	if res.Code != http.StatusUnauthorized || accountCookieOf(res) != nil {
		t.Errorf("status %d, cookie %v: want 401 and no cookie", res.Code, accountCookieOf(res))
	}
}
