package admin

import (
	"net/http"
	"strconv"
	"strings"
)

const themeCookie = "admin_theme"

// themeOf is "light" or "dark" when the owner picked one, "" to follow the device.
func themeOf(r *http.Request) string {
	c, err := r.Cookie(themeCookie)
	if err != nil {
		return ""
	}
	if c.Value == "light" || c.Value == "dark" {
		return c.Value
	}
	return ""
}

// setTheme stores the colour choice in a cookie and returns to the page the
// switch was pressed on. Only /admin paths are accepted as the way back.
func (h *Handler) setTheme(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	c := &http.Cookie{Name: themeCookie, Path: "/admin", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode}
	switch v := r.PostForm.Get("theme"); v {
	case "light", "dark":
		c.Value, c.MaxAge = v, 365*24*60*60
	default: // auto: follow the device
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
	http.Redirect(w, r, backTo(r.PostForm.Get("back"), r.PostForm.Get("account")), http.StatusSeeOther)
}

// backTo is the page a switch returns to: an /admin path (backPath), with the
// page's account when it is a plain positive number, so the owner stays on the
// account they were looking at.
func backTo(path, account string) string {
	p := backPath(path)
	if n, err := strconv.ParseInt(strings.TrimSpace(account), 10, 64); err == nil && n > 0 {
		p += "?account=" + strconv.FormatInt(n, 10)
	}
	return p
}

func backPath(p string) string {
	if p == "/admin" || (strings.HasPrefix(p, "/admin/") && !strings.ContainsAny(p, "\\?#") && !strings.Contains(p, "//") && !strings.Contains(p, "..")) {
		return p
	}
	return "/admin"
}
