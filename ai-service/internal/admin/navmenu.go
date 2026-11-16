package admin

import (
	"context"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/leads"
)

// The side menu shows how much work is waiting (count badges) and remembers
// which groups the owner folded. Issue: DEV-Johnstocker/chatwoot#1

// navCounts is the work waiting on one account, shown as menu badges.
type navCounts struct {
	Gaps      int  // chats the bot could not answer (Unanswered questions)
	GapsMore  bool // the gap list was full: show Gaps as "N+"
	Approvals int  // AI changes waiting for the owner's yes or no
	Slips     int  // payment slips sent, not yet confirmed by staff
}

// Improve and Sales are the totals a folded group shows on its heading.
func (c navCounts) Improve() int { return c.Gaps + c.Approvals }
func (c navCounts) Sales() int   { return c.Slips }

// Total is the number on the phone's Menu button.
func (c navCounts) Total() int { return c.Improve() + c.Sales() }

const (
	// navCountsTTL keeps the badges cheap: one Chatwoot read and two small
	// queries per account per minute, however many pages are opened.
	navCountsTTL = time.Minute
	// navCountsTimeout bounds each read so a slow Chatwoot never slows a page;
	// a read that fails shows no badge.
	navCountsTimeout = 3 * time.Second
	// navCacheMax bounds the cache; it is cleared when full.
	navCacheMax = 256
)

type navCacheEntry struct {
	at time.Time
	c  navCounts
}

type navCache struct {
	mu sync.Mutex
	m  map[int64]navCacheEntry
}

// menuCounts returns the badges for acct, from the cache when fresh.
func (h *Handler) menuCounts(ctx context.Context, acct int64) navCounts {
	now := time.Now()
	h.nav.mu.Lock()
	if e, ok := h.nav.m[acct]; ok && now.Sub(e.at) < navCountsTTL {
		h.nav.mu.Unlock()
		return e.c
	}
	h.nav.mu.Unlock()

	c := h.loadMenuCounts(ctx, acct)

	h.nav.mu.Lock()
	if h.nav.m == nil || len(h.nav.m) >= navCacheMax {
		h.nav.m = map[int64]navCacheEntry{}
	}
	h.nav.m[acct] = navCacheEntry{at: now, c: c}
	h.nav.mu.Unlock()
	return c
}

func (h *Handler) loadMenuCounts(ctx context.Context, acct int64) navCounts {
	var c navCounts
	if h.Gaps != nil {
		cctx, cancel := context.WithTimeout(ctx, navCountsTimeout)
		list, err := h.Gaps.List(cctx, acct, gapPageLimit)
		cancel()
		if err != nil {
			h.Logger.Warn("menu badge: list gap chats", "account_id", acct, "error", err)
		}
		c.Gaps, c.GapsMore = len(list), len(list) >= gapPageLimit
	}
	if h.approvalsOn() {
		cctx, cancel := context.WithTimeout(ctx, navCountsTimeout)
		list, err := h.Pending.List(cctx, acct)
		cancel()
		if err != nil {
			h.Logger.Warn("menu badge: list ai changes", "account_id", acct, "error", err)
		}
		c.Approvals = len(list)
	}
	if h.Leads != nil {
		cctx, cancel := context.WithTimeout(ctx, navCountsTimeout)
		counts, err := h.Leads.Counts(cctx, acct)
		cancel()
		if err != nil {
			h.Logger.Warn("menu badge: count leads", "account_id", acct, "error", err)
		}
		c.Slips = counts[leads.StageSlip]
	}
	return c
}

// navGroupOf maps each page to its menu group; a group holding the open
// page is never folded, so the owner always sees where they are.
var navGroupOf = map[string]string{
	"hours": "bot", "test": "bot", "nudge": "bot",
	"business": "knows", "knowledge": "knows", "promos": "knows", "images": "knows",
	"replies": "improve", "gaps": "improve", "approvals": "improve",
	"sales": "sales", "leads": "sales", "comments": "sales",
	"alerts": "settings", "spend": "settings", "providers": "settings", "connections": "settings", "history": "settings",
}

// navFoldCookie lists the folded groups, set by admin.js when a heading is
// clicked ("bot.settings"). Without it, or without JavaScript, every group is open.
const navFoldCookie = "admin_nav"

// foldedGroups reads the cookie, keeping only known group names.
func foldedGroups(r *http.Request) map[string]bool {
	out := map[string]bool{}
	c, err := r.Cookie(navFoldCookie)
	if err != nil {
		return out
	}
	known := map[string]bool{}
	for _, g := range navGroupOf {
		known[g] = true
	}
	for _, g := range strings.Split(c.Value, ".") {
		if known[g] {
			out[g] = true
		}
	}
	return out
}

// navOpenFunc says whether a group renders open for this page.
func navOpenFunc(folded map[string]bool) func(group, page string) bool {
	return func(group, page string) bool {
		return !folded[group] || navGroupOf[page] == group
	}
}

// navIconPaths are 24×24 stroke icons, one per menu page.
var navIconPaths = map[string]string{
	"home":        "M3 10.5 12 3l9 7.5V21H3z M9 21v-6h6v6",
	"hours":       "M12 3v9 M6.3 6.3a8 8 0 1 0 11.4 0",
	"test":        "M4 5h16v11H8l-4 4z",
	"nudge":       "M12 7v5l3 2 M21 12a9 9 0 1 1-9-9 M17 3h4v4",
	"business":    "M3 9l2-5h14l2 5 M4 9v11h16V9 M9 20v-6h6v6",
	"knowledge":   "M4 4h6a2 2 0 0 1 2 2v14a2 2 0 0 0-2-2H4z M20 4h-6a2 2 0 0 0-2 2v14a2 2 0 0 1 2-2h6z",
	"promos":      "M3 12V3h9l9 9-9 9z M7.5 7.5h.01",
	"images":      "M3 4h18v16H3z M3 16l5-5 5 5 3-3 5 5 M15.5 8.5h.01",
	"replies":     "M12 3l2.8 5.7 6.2.9-4.5 4.4 1.1 6.2L12 17.3 6.4 20.2l1.1-6.2L3 9.6l6.2-.9z",
	"gaps":        "M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z M9.5 9a2.5 2.5 0 1 1 3.5 2.3c-.6.3-1 .9-1 1.7 M12 17h.01",
	"approvals":   "M4 4h16v16H4z M8 12l3 3 5-6",
	"sales":       "M3 17l6-6 4 4 8-8 M15 7h6v6",
	"leads":       "M15 20v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2 M8.5 10a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7z M19 8v6 M16 11h6",
	"comments":    "M4 5h16v11H8l-4 4z M8 10.5h.01 M12 10.5h.01 M16 10.5h.01",
	"alerts":      "M6 9a6 6 0 1 1 12 0c0 6 3 7 3 7H3s3-1 3-7 M10 20a2 2 0 0 0 4 0",
	"spend":       "M3 7h18v13H3z M3 7l3-4h12l3 4 M16 13.5h.01",
	"providers":   "M7 7h10v10H7z M10 3v4 M14 3v4 M10 17v4 M14 17v4 M3 10h4 M3 14h4 M17 10h4 M17 14h4",
	"connections": "M9 3v5 M15 3v5 M6 8h12v3a6 6 0 0 1-12 0z M12 17v4",
	"history":     "M3 12a9 9 0 1 0 3-6.7L3 8 M3 3v5h5 M12 7v5l3 2",
}

// navIcon is the decorative icon for a menu page; empty for an unknown name.
// The paths are constants above, never user input.
func navIcon(name string) template.HTML {
	d, ok := navIconPaths[name]
	if !ok {
		return ""
	}
	return template.HTML(`<svg class="nav-ico" aria-hidden="true" focusable="false" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="` + d + `"/></svg>`) // #nosec G203 -- constant markup
}
