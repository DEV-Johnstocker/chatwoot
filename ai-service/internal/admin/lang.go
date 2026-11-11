package admin

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// The admin pages speak English or Thai. The English text is the key: tr
// returns the Thai line from the thai catalog (i18n_th.go) when the page is in
// Thai and the line has been translated, and the English text otherwise, so a
// page that is not translated yet still works. Issue: DEV-Johnstocker/chatwoot#1

const langCookie = "admin_lang"

const (
	langEN = "en"
	langTH = "th"
)

// langOf is the owner's choice from the switch, or else the browser's first
// language: Thai when it prefers Thai, English otherwise.
func langOf(r *http.Request) string {
	if c, err := r.Cookie(langCookie); err == nil && (c.Value == langEN || c.Value == langTH) {
		return c.Value
	}
	return preferredLang(r.Header.Get("Accept-Language"))
}

// preferredLang reads the highest-weighted language of an Accept-Language
// header (RFC 9110 §12.5.4). Ties keep the header's order.
func preferredLang(header string) string {
	best, bestQ := langEN, -1.0
	for _, part := range strings.Split(header, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		q := 1.0
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			if _, err := fmt.Sscanf(v, "%g", &q); err != nil {
				continue
			}
		}
		primary, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
		if primary == "" || primary == "*" || q <= bestQ {
			continue
		}
		bestQ = q
		best = langEN
		if primary == langTH {
			best = langTH
		}
	}
	return best
}

// setLang stores the language choice and returns to the page the switch was
// pressed on (only /admin paths, like the theme switch).
func (h *Handler) setLang(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	c := &http.Cookie{Name: langCookie, Path: "/admin", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode}
	switch v := r.PostForm.Get("lang"); v {
	case langEN, langTH:
		c.Value, c.MaxAge = v, 365*24*60*60
	default: // follow the browser again
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
	http.Redirect(w, r, backTo(r.PostForm.Get("back"), r.PostForm.Get("account")), http.StatusSeeOther)
}

// tr translates one English line into lang and fills in args (fmt verbs).
func tr(lang, english string, args ...any) string {
	s := english
	if lang == langTH {
		if t, ok := thai[english]; ok {
			s = t
		}
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

var thaiMonths = [...]string{"ม.ค.", "ก.พ.", "มี.ค.", "เม.ย.", "พ.ค.", "มิ.ย.", "ก.ค.", "ส.ค.", "ก.ย.", "ต.ค.", "พ.ย.", "ธ.ค."}

// stampIn is a short day and time ("12 Nov 03:52", or "12 พ.ย. 03:52").
func (h *Handler) stampIn(lang string, t time.Time) string {
	t = t.In(h.loc())
	if lang != langTH {
		return t.Format("2 Jan 15:04")
	}
	return fmt.Sprintf("%d %s %s", t.Day(), thaiMonths[t.Month()-1], t.Format("15:04"))
}

// dayIn is a date; Thai uses the Buddhist year, as Thai readers expect.
func (h *Handler) dayIn(lang string, t time.Time) string {
	t = t.In(h.loc())
	if lang != langTH {
		return t.Format("2 Jan 2006")
	}
	return fmt.Sprintf("%d %s %d", t.Day(), thaiMonths[t.Month()-1], t.Year()+543)
}

// whenIn is a date and time.
func (h *Handler) whenIn(lang string, t time.Time) string {
	if lang != langTH {
		return h.when(t)
	}
	return h.dayIn(lang, t) + " " + t.In(h.loc()).Format("15:04")
}
