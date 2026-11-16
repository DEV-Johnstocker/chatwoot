package admin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/leads"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/pending"
)

// The side menu groups its pages under headings the owner can fold, shows
// an icon per page and counts the work waiting. Issue: DEV-Johnstocker/chatwoot#1

var navGroupRE = regexp.MustCompile(`(?s)<details class="nav-group" data-group="([a-z]+)"( open)?>\s*<summary class="nav-head">([^<]+)(.*?)</details>`)
var navHrefRE = regexp.MustCompile(`<a href="(/admin[^"]*)"`)

type navEnv struct {
	mux   *http.ServeMux
	gaps  *fakeGaps
	leads *fakeLeadList
}

// newNavEnv serves the admin with 3 open gaps, 2 waiting AI changes and
// 1 unconfirmed payment slip on account 3.
func newNavEnv(t *testing.T) navEnv {
	t.Helper()
	fg := &fakeGaps{open: map[int64]gaps.Gap{1: {ConversationID: 1}, 2: {ConversationID: 2}, 3: {ConversationID: 3}}}
	fl := &fakeLeadList{rows: []leads.Lead{
		{AccountID: 3, Stage: leads.StageSlip}, {AccountID: 3, Stage: leads.StageAsked}, {AccountID: 4, Stage: leads.StageSlip},
	}}
	pend := &pending.Memory{}
	for _, target := range []string{"a", "b"} {
		if _, err := pend.Propose(context.Background(), pending.Change{AccountID: 3, Kind: pending.KindDocument, Target: target, Proposed: []byte(`{}`), Actor: "claude"}); err != nil {
			t.Fatal(err)
		}
	}
	h := &Handler{Password: pw, Gaps: fg, Leads: fl, Pending: pend, Approver: &fakeApprover{},
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	mux := http.NewServeMux()
	h.Routes(mux)
	return navEnv{mux: mux, gaps: fg, leads: fl}
}

func sidebarOf(body string) string {
	s, _, _ := strings.Cut(body, `<header class="topbar">`)
	return s
}

// linkHTML is the <a> for href in s.
func linkHTML(s, href string) string {
	i := strings.Index(s, `<a href="`+href+`"`)
	if i < 0 {
		return ""
	}
	return s[i : i+strings.Index(s[i:], "</a>")]
}

func TestNav_GroupsEveryPageOnceUnderAHeading(t *testing.T) {
	// Arrange
	e := nudgeEnv(t, true)
	want := map[string][]string{
		"Bot":                {"/admin/hours", "/admin/test", "/admin/nudge"},
		"What the bot knows": {"/admin/business", "/admin/knowledge", "/admin/promotions", "/admin/images"},
		"Improve answers":    {"/admin/replies", "/admin/gaps", "/admin/approvals"},
		"Sales and leads":    {"/admin/sales", "/admin/leads", "/admin/comments"},
		"Settings":           {"/admin/alerts", "/admin/spend", "/admin/providers", "/admin/connections", "/admin/history"},
	}

	// Act
	sidebar := sidebarOf(serve(e.mux, hget("/admin/nudge?account=3")).Body.String())

	// Assert
	groups := navGroupRE.FindAllStringSubmatch(sidebar, -1)
	if len(groups) != len(want) {
		t.Fatalf("got %d menu groups, want %d", len(groups), len(want))
	}
	seen := map[string]int{}
	for _, g := range groups {
		head := g[3]
		if g[2] != " open" {
			t.Errorf("group %q is folded without a cookie", head)
		}
		if !strings.Contains(g[4], `role="group" aria-label="`+head+`"`) {
			t.Errorf("group %q links are not labelled with its heading", head)
		}
		var got []string
		for _, m := range navHrefRE.FindAllStringSubmatch(g[4], -1) {
			got = append(got, m[1])
			seen[m[1]]++
		}
		if strings.Join(got, " ") != strings.Join(want[head], " ") {
			t.Errorf("group %q links %v, want %v", head, got, want[head])
		}
	}
	for href, n := range seen {
		if n != 1 {
			t.Errorf("%s appears %d times in the sidebar", href, n)
		}
	}
	if !strings.Contains(sidebar, `<a href="/admin"`) {
		t.Error("Home link missing above the groups")
	}
	if !strings.Contains(linkHTML(sidebar, "/admin/nudge"), `aria-current="page"`) {
		t.Error("open page not marked current")
	}
}

func TestNav_EveryLinkHasADecorativeIcon(t *testing.T) {
	// Arrange
	e := nudgeEnv(t, true)

	// Act
	sidebar := sidebarOf(serve(e.mux, hget("/admin/nudge?account=3")).Body.String())

	// Assert
	links := navHrefRE.FindAllString(sidebar, -1)
	icons := strings.Count(sidebar, `<svg class="nav-ico" aria-hidden="true" focusable="false"`)
	if len(links) == 0 || icons != len(links) {
		t.Errorf("%d icons for %d links", icons, len(links))
	}
	if navIcon("no-such-page") != "" {
		t.Error("unknown icon name drew something")
	}
}

func TestNav_GroupHeadingsInThai(t *testing.T) {
	// Arrange
	e := nudgeEnv(t, true)
	r := hget("/admin/nudge?account=3")
	r.Header.Set("Accept-Language", "th")

	// Act
	body := serve(e.mux, r).Body.String()

	// Assert
	for _, want := range []string{`aria-label="ข้อมูลที่บอตรู้"`, `>ปรับปรุงคำตอบ`, `>ยอดขายและลีด`, `>การตั้งค่า</summary>`, `>บอต</summary>`} {
		if !strings.Contains(body, want) {
			t.Errorf("Thai menu lacks %q", want)
		}
	}
}

func TestNav_CountsWaitingWorkOnThePagesAccount(t *testing.T) {
	// Arrange
	e := newNavEnv(t)

	// Act
	body := serve(e.mux, hget("/admin/hours?account=3")).Body.String()
	sidebar := sidebarOf(body)

	// Assert
	for href, want := range map[string]string{
		"/admin/gaps":      `<span class="nav-count">3<span class="visually-hidden"> questions without an answer</span>`,
		"/admin/approvals": `<span class="nav-count">2<span class="visually-hidden"> changes waiting for you</span>`,
		"/admin/leads":     `<span class="nav-count">1<span class="visually-hidden"> payment slips to confirm</span>`,
	} {
		if !strings.Contains(linkHTML(sidebar, href), want) {
			t.Errorf("%s badge: got %q", href, linkHTML(sidebar, href))
		}
	}
	for _, href := range []string{"/admin/hours", "/admin/sales", "/admin/business"} {
		if strings.Contains(linkHTML(sidebar, href), "nav-count") {
			t.Errorf("%s has a badge but has nothing waiting", href)
		}
	}
	if !strings.Contains(body, `<summary>Menu <span class="nav-count">6<`) {
		t.Error("phone Menu button does not show the total of 6")
	}
	if !strings.Contains(sidebar, `Improve answers<span class="nav-count nav-sum">5<`) || !strings.Contains(sidebar, `Sales and leads<span class="nav-count nav-sum">1<`) {
		t.Error("group headings lack the totals shown when folded")
	}
	if len(e.gaps.accts) != 1 || e.gaps.accts[0] != 3 {
		t.Errorf("gap list read for accounts %v, want [3]", e.gaps.accts)
	}
}

func TestNav_CountsAreCachedForAMinute(t *testing.T) {
	// Arrange
	e := newNavEnv(t)

	// Act
	for range 3 {
		serve(e.mux, hget("/admin/hours?account=3"))
	}
	serve(e.mux, hget("/admin/hours?account=4"))

	// Assert
	if got := e.gaps.accts; len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Errorf("Chatwoot gap list read for %v, want once per account [3 4]", got)
	}
}

func TestNav_FullGapListShowsPlus(t *testing.T) {
	// Arrange
	e := newNavEnv(t)
	for i := range int64(gapPageLimit) {
		e.gaps.open[100+i] = gaps.Gap{ConversationID: 100 + i}
	}

	// Act
	sidebar := sidebarOf(serve(e.mux, hget("/admin/hours?account=3")).Body.String())

	// Assert
	if !strings.Contains(linkHTML(sidebar, "/admin/gaps"), `class="nav-count">28+<`) {
		t.Errorf("full gap list badge: %q", linkHTML(sidebar, "/admin/gaps"))
	}
}

func TestNav_FailedReadsShowNoBadgeAndKeepThePage(t *testing.T) {
	// Arrange
	e := newNavEnv(t)
	e.gaps.listErr = errors.New("chatwoot down")
	e.leads.err = errors.New("db down")

	// Act
	rec := serve(e.mux, hget("/admin/hours?account=3"))
	sidebar := sidebarOf(rec.Body.String())

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d", rec.Code)
	}
	for _, href := range []string{"/admin/gaps", "/admin/leads"} {
		if strings.Contains(linkHTML(sidebar, href), "nav-count") {
			t.Errorf("%s shows a badge after a failed read", href)
		}
	}
	if !strings.Contains(linkHTML(sidebar, "/admin/approvals"), `class="nav-count">2<`) {
		t.Error("working count lost because another failed")
	}
}

func TestNav_FoldCookieFoldsGroupsButNeverTheOpenPagesGroup(t *testing.T) {
	// Arrange
	e := nudgeEnv(t, true)
	r := hget("/admin/nudge?account=3")
	r.AddCookie(&http.Cookie{Name: navFoldCookie, Value: "bot.settings.knows.nope"})

	// Act
	sidebar := sidebarOf(serve(e.mux, r).Body.String())

	// Assert
	open := map[string]bool{}
	for _, g := range navGroupRE.FindAllStringSubmatch(sidebar, -1) {
		open[g[1]] = g[2] == " open"
	}
	want := map[string]bool{"bot": true, "knows": false, "improve": true, "sales": true, "settings": false}
	for g, w := range want {
		if open[g] != w {
			t.Errorf("group %s open=%v, want %v", g, open[g], w)
		}
	}
}

func TestNav_BadFoldCookieIsIgnored(t *testing.T) {
	// Arrange
	e := nudgeEnv(t, true)
	r := hget("/admin/hours?account=3")
	r.AddCookie(&http.Cookie{Name: navFoldCookie, Value: `x"onmouseover="alert(1)`})

	// Act
	body := serve(e.mux, r).Body.String()

	// Assert
	if strings.Contains(body, "onmouseover") {
		t.Fatal("cookie text reached the page")
	}
	if n := len(navGroupRE.FindAllStringSubmatch(sidebarOf(body), -1)); n != 5 || strings.Count(sidebarOf(body), `class="nav-group" data-group=`) != strings.Count(sidebarOf(body), `" open>`) {
		t.Errorf("a bad cookie folded groups (%d groups)", n)
	}
}
