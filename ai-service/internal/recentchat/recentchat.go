// Package recentchat remembers when each Messenger customer last sent a chat
// message, so comment auto-send can leave people who are already chatting to
// a person. Facebook gives a commenter the same id as their Messenger chat
// (the page-scoped id, checked on STAGING 2026-11-14), so the comment's from
// id finds the chat.
//
// It lives in memory. After a restart it knows nothing for one window, so
// until then it answers "maybe chatted" and auto-send holds the comment: a
// missed auto-send costs the owner one click, a wrong one cannot be unsent.
// Issue: DEV-Johnstocker/chatwoot#1
package recentchat

import (
	"sync"
	"time"
)

// Window is how far back a chat counts: Meta's standard messaging window.
const Window = 24 * time.Hour

// DefaultMax caps how many people are remembered.
const DefaultMax = 50000

type key struct {
	account int64
	id      string
}

// Chats records customers' last chat message. The zero value is not usable;
// use New.
type Chats struct {
	mu      sync.Mutex
	last    map[key]time.Time
	started time.Time
	max     int
}

// New returns an empty record that starts its blind window at now.
func New(now time.Time) *Chats {
	return &Chats{last: map[key]time.Time{}, started: now, max: DefaultMax}
}

// Saw records that the customer with this page-scoped id sent a chat message
// in the account at t. Empty ids are ignored.
func (c *Chats) Saw(accountID int64, id string, t time.Time) {
	if id == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	k := key{accountID, id}
	if t.After(c.last[k]) {
		c.last[k] = t
	}
	if len(c.last) > c.max {
		c.prune(t)
	}
}

// prune drops people whose last message is outside the window, then, if
// still over the cap, the oldest ones.
func (c *Chats) prune(now time.Time) {
	for k, t := range c.last {
		if now.Sub(t) > Window {
			delete(c.last, k)
		}
	}
	for len(c.last) > c.max {
		var oldest key
		first := true
		for k, t := range c.last {
			if first || t.Before(c.last[oldest]) {
				oldest, first = k, false
			}
		}
		delete(c.last, oldest)
	}
}

// MaybeChatted reports whether the customer may have sent a chat message in
// the window before now: true when one was seen, or when the record is
// younger than the window and so cannot know. An empty id is unknown: true.
func (c *Chats) MaybeChatted(accountID int64, id string, now time.Time) bool {
	if id == "" {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.last[key{accountID, id}]; ok && now.Sub(t) <= Window {
		return true
	}
	return now.Sub(c.started) < Window
}
