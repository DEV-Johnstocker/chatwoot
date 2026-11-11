package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/comments"
)

func langPost(v url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/admin/lang", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.SetBasicAuth(Username, pw)
	return r
}

func TestPreferredLang_FollowsTheBrowser(t *testing.T) {
	cases := map[string]string{
		"":                        langEN,
		"th":                      langTH,
		"th-TH,th;q=0.9,en;q=0.8": langTH,
		"en-US,en;q=0.9,th;q=0.8": langEN,
		"en;q=0.5, th;q=0.9":      langTH, // weight wins over order
		"fr-FR,fr;q=0.9":          langEN, // not offered: English
		"*":                       langEN,
		"th;q=abc, en":            langEN, // a broken weight is skipped
		"TH":                      langTH,
	}
	for header, want := range cases {
		// Act
		got := preferredLang(header)

		// Assert
		if got != want {
			t.Errorf("preferredLang(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestLang_SwitchSetsCookieAndPageRendersThai(t *testing.T) {
	// Arrange
	e := setup(t, nil)

	// Act
	rec := e.do(langPost(url.Values{"lang": {"th"}, "back": {"/admin/comments"}}))
	page := httptest.NewRequest(http.MethodGet, "/admin", nil)
	page.SetBasicAuth(Username, pw)
	page.Header.Set("Accept-Language", "en-US") // the switch beats the browser
	for _, c := range rec.Result().Cookies() {
		page.AddCookie(c)
	}
	body := e.do(page).Body.String()

	// Assert
	c := rec.Result().Cookies()
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/comments" {
		t.Fatalf("status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if len(c) != 1 || c[0].Value != "th" || !c[0].HttpOnly || c[0].SameSite != http.SameSiteStrictMode || c[0].Path != "/admin" {
		t.Fatalf("cookie %+v", c)
	}
	for _, want := range []string{`<html lang="th"`, "หน้าแรก", "ลีดจากคอมเมนต์", `value="th" lang="th" aria-pressed="true"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestLang_EnglishByDefaultAndUnknownCookieIgnored(t *testing.T) {
	// Arrange
	e := setup(t, nil)
	page := httptest.NewRequest(http.MethodGet, "/admin", nil)
	page.SetBasicAuth(Username, pw)
	page.AddCookie(&http.Cookie{Name: langCookie, Value: "fr"})

	// Act
	body := e.do(page).Body.String()

	// Assert
	if !strings.Contains(body, `<html lang="en"`) || !strings.Contains(body, ">Comment leads</a>") || strings.Contains(body, "หน้าแรก") {
		t.Fatal("page is not in English")
	}
}

func TestLang_SwitchOnlyReturnsToAdminAndClearsOnUnknown(t *testing.T) {
	for _, back := range []string{"https://evil.example/admin", "//evil.example/admin", "/admin/../x", `/admin\evil`, "javascript:alert(1)"} {
		// Arrange
		e := setup(t, nil)

		// Act
		rec := e.do(langPost(url.Values{"lang": {"xx"}, "back": {back}}))

		// Assert
		c := rec.Result().Cookies()
		if rec.Header().Get("Location") != "/admin" || len(c) != 1 || c[0].MaxAge >= 0 {
			t.Errorf("back %q: location %q cookies %+v", back, rec.Header().Get("Location"), c)
		}
	}
}

func TestLang_SwitchRequiresAuthAndSameOrigin(t *testing.T) {
	// Arrange
	e := setup(t, nil)
	noAuth := langPost(url.Values{"lang": {"th"}})
	noAuth.Header.Del("Authorization")
	crossSite := langPost(url.Values{"lang": {"th"}})
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")

	// Act
	a, b := e.do(noAuth), e.do(crossSite)

	// Assert
	if a.Code != http.StatusUnauthorized || len(a.Result().Cookies()) != 0 {
		t.Errorf("no auth: %d %v", a.Code, a.Result().Cookies())
	}
	if b.Code != http.StatusForbidden || len(b.Result().Cookies()) != 0 {
		t.Errorf("cross site: %d %v", b.Code, b.Result().Cookies())
	}
}

func TestComments_PageInThai(t *testing.T) {
	// Arrange
	e := newCommentsEnv(t, true, true)
	e.waiting(t, 2, "c1", "ราคา", time.Now())
	r := commentsGet("account=2")
	r.Header.Set("Accept-Language", "th-TH,th;q=0.9")

	// Act
	body := serve(e.mux, r).Body.String()

	// Assert
	for _, want := range []string{`<html lang="th"`, "ลีดจากคอมเมนต์", "ส่งข้อความส่วนตัว", "ข้าม", "น่าจะซื้อ", "วันให้ตอบ"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	for _, english := range []string{"Send private reply", "Likely buyer", "days left to reply"} {
		if strings.Contains(body, english) {
			t.Errorf("page still says %q", english)
		}
	}
}

func TestThaiDates(t *testing.T) {
	// Arrange
	h := &Handler{}
	at := time.Date(2026, 11, 12, 3, 52, 0, 0, time.UTC)

	// Act
	stamp, day, when := h.stampIn(langTH, at), h.dayIn(langTH, at), h.whenIn(langTH, at)

	// Assert
	if stamp != "12 พ.ย. 03:52" || day != "12 พ.ย. 2569" || when != "12 พ.ย. 2569 03:52" || h.stampIn(langEN, at) != "12 Nov 03:52" {
		t.Fatalf("stamp %q day %q when %q", stamp, day, when)
	}
}

// Every line Home, Comment leads and the shared menu show must have Thai,
// and each Thai line keeps the English line's fmt verbs in the same order.
var templateText = regexp.MustCompile(`\{\{T "([^"]+)"`)

func TestThai_CatalogCoversStepOnePages(t *testing.T) {
	// Arrange
	var keys []string
	for _, f := range []string{"home.html", "comments.html", "layout.html"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range templateText.FindAllStringSubmatch(string(b), -1) {
			keys = append(keys, m[1])
		}
	}
	for _, m := range []map[comments.Intent]string{intentLabels} {
		for _, v := range m {
			keys = append(keys, v)
		}
	}
	for _, v := range statusLabels {
		keys = append(keys, v)
	}
	for _, m := range []map[string]string{agreementLabels, holdReasons} {
		for _, v := range m {
			keys = append(keys, v)
		}
	}
	for _, err := range []error{comments.ErrNotRecorded, comments.ErrEmptyReply, comments.ErrLongReply, comments.ErrSendingOff,
		comments.ErrExpired, comments.ErrChanged, comments.ErrNotFound, &comments.SendError{Permanent: true}, http.ErrHandlerTimeout} {
		_, msg := commentError(err)
		keys = append(keys, msg.text)
	}
	keys = append(keys, "Connect an AI model", "The model that writes replies.", "Set up", "Turn on knowledge search",
		"An embeddings model, so the bot can search your documents.", "Describe your business", "Name, what you sell, tone and rules.",
		"Edit profile", "Add knowledge", "Prices, schedules, policies and FAQs the bot may quote.",
		"Could not load the comments. Check the database connection.", "would send this reply as written.",
		"would wait for you, because %s.", "%d days left to reply", "%d hours left to reply", "Less than 2 hours left to reply", "Facebook user")

	// Act
	var missing []string
	for _, k := range keys {
		if _, ok := thai[k]; !ok {
			missing = append(missing, k)
		}
	}

	// Assert
	if len(keys) < 100 || len(missing) > 0 {
		t.Fatalf("%d keys; no Thai for %q", len(keys), missing)
	}
}

var fmtVerb = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z%]`)

func TestThai_KeepsFormatVerbs(t *testing.T) {
	for en, th := range thai {
		// Act
		a, b := fmtVerb.FindAllString(en, -1), fmtVerb.FindAllString(th, -1)

		// Assert
		if strings.Join(a, " ") != strings.Join(b, " ") {
			t.Errorf("%q has %v, Thai %q has %v", en, a, th, b)
		}
	}
}
