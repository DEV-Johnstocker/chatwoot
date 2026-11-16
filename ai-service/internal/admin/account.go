package admin

import (
	"net/http"
	"strconv"
)

// The admin keeps the account the owner is working on: opening a page with
// ?account=N remembers N in a cookie, and pages opened without ?account=
// (menu links, bookmarks, /admin) show that account instead of account 1.
// Issue: DEV-Johnstocker/chatwoot#1

const (
	accountCookie    = "admin_account"
	accountCookieAge = 365 * 24 * 60 * 60
)

// accountPagePath lists the pages that show one account, by menu key.
// AI spend and AI providers are left out on purpose: spend without an
// account shows every business, and providers are shared.
var accountPagePath = map[string]string{
	"home": "/admin", "hours": "/admin/hours", "test": "/admin/test", "nudge": "/admin/nudge",
	"business": "/admin/business", "knowledge": "/admin/knowledge", "promos": "/admin/promotions", "images": "/admin/images",
	"replies": "/admin/replies", "gaps": "/admin/gaps", "approvals": "/admin/approvals",
	"sales": "/admin/sales", "leads": "/admin/leads", "comments": "/admin/comments",
	"alerts": "/admin/alerts", "connections": "/admin/connections", "history": "/admin/history",
}

var sharedPagePath = map[string]string{"spend": "/admin/spend", "providers": "/admin/providers"}

// rememberedAccount is the account in the cookie, when it is a valid ID.
func rememberedAccount(r *http.Request) (int64, bool) {
	c, err := r.Cookie(accountCookie)
	if err != nil {
		return 0, false
	}
	return formAccount(c.Value)
}

// currentAccount is the account the page is about: ?account=, else the
// remembered one, else 1 (the default every page already used).
func currentAccount(r *http.Request) int64 {
	if n, ok := formAccount(r.URL.Query().Get("account")); ok {
		return n
	}
	if n, ok := rememberedAccount(r); ok {
		return n
	}
	return 1
}

// keepAccount wraps a GET handler of a one-account page. A valid ?account=
// is remembered; without one, the remembered account is filled in, so the
// page and its forms use it. An invalid ?account= is left to the page.
func (h *Handler) keepAccount(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if raw := q.Get("account"); raw != "" {
			if n, ok := formAccount(raw); ok {
				http.SetCookie(w, &http.Cookie{Name: accountCookie, Value: strconv.FormatInt(n, 10), Path: "/admin",
					MaxAge: accountCookieAge, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
			}
		} else if n, ok := rememberedAccount(r); ok {
			r = r.Clone(r.Context())
			q.Set("account", strconv.FormatInt(n, 10))
			r.URL.RawQuery = q.Encode()
		}
		next(w, r)
	}
}

// navHrefFunc builds menu links that keep acct on one-account pages.
func navHrefFunc(acct int64) func(key string) string {
	return func(key string) string {
		if p, ok := accountPagePath[key]; ok {
			return p + "?account=" + strconv.FormatInt(acct, 10)
		}
		return sharedPagePath[key]
	}
}

// navSwitchHref is where the menu's account switch goes from page: the same
// page with the other account, or Home from a shared page.
func navSwitchHref(page, acct string) string {
	return navSwitchAction(page) + "?account=" + acct
}

// navSwitchAction is the form action of the "account ID" box in the switch.
func navSwitchAction(page string) string {
	if p, ok := accountPagePath[page]; ok {
		return p
	}
	return "/admin"
}
