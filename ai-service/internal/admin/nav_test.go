package admin

import (
	"regexp"
	"strings"
	"testing"
)

// The side menu groups its pages under small headings, so the owner finds a
// page by what it is for. Issue: DEV-Johnstocker/chatwoot#1

var navGroupRE = regexp.MustCompile(`(?s)<div class="nav-group" role="group" aria-label="([^"]+)">(.*?)</div>`)
var navHrefRE = regexp.MustCompile(`<a href="(/admin[^"]*)"`)

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
	body := serve(e.mux, hget("/admin/nudge?account=3")).Body.String()
	sidebar, _, _ := strings.Cut(body, `<header class="topbar">`)

	// Assert
	groups := navGroupRE.FindAllStringSubmatch(sidebar, -1)
	if len(groups) != len(want) {
		t.Fatalf("got %d menu groups, want %d", len(groups), len(want))
	}
	seen := map[string]int{}
	for _, g := range groups {
		if !strings.Contains(g[2], `<p class="nav-head" aria-hidden="true">`+g[1]+`</p>`) {
			t.Errorf("group %q has no visible heading", g[1])
		}
		var got []string
		for _, m := range navHrefRE.FindAllStringSubmatch(g[2], -1) {
			got = append(got, m[1])
			seen[m[1]]++
		}
		if strings.Join(got, " ") != strings.Join(want[g[1]], " ") {
			t.Errorf("group %q links %v, want %v", g[1], got, want[g[1]])
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
}

func TestNav_GroupHeadingsInThai(t *testing.T) {
	// Arrange
	e := nudgeEnv(t, true)
	r := hget("/admin/nudge?account=3")
	r.Header.Set("Accept-Language", "th")

	// Act
	body := serve(e.mux, r).Body.String()

	// Assert
	for _, want := range []string{`aria-label="ข้อมูลที่บอตรู้"`, `>ปรับปรุงคำตอบ</p>`, `>ยอดขายและลีด</p>`, `>การตั้งค่า</p>`, `>บอต</p>`} {
		if !strings.Contains(body, want) {
			t.Errorf("Thai menu lacks %q", want)
		}
	}
}
