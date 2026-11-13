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

// Quiet is how long the record may hear no customer message in an account
// before it stops trusting itself: no message for that long usually means
// Chatwoot stopped sending events (bot detached, webhook failing), not that
// nobody chatted. Until the next message every answer is "maybe".
const Quiet = 6 * time.Hour

type key struct {
	account int64
	id      string
}

// Chats records customers' last chat message. The zero value is not usable;
// use New.
type Chats struct {
	mu      sync.Mutex
	last    map[key]time.Time
	heard   map[int64]time.Time // last customer message seen, per account
	started time.Time
	max     int
}

// New returns an empty record that starts its blind window at now.
func New(now time.Time) *Chats {
	return &Chats{last: map[key]time.Time{}, heard: map[int64]time.Time{}, started: now, max: DefaultMax}
}

// Saw records that the customer with this page-scoped id sent a chat message
// in the account at t. Empty ids are ignored.
func (c *Chats) Saw(accountID int64, id string, t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.After(c.heard[accountID]) {
		c.heard[accountID] = t
	}
	if id == "" {
		return
	}
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
	if len(c.last) > c.max {
		// Forgetting someone who chatted inside the window would let
		// auto-send reach them: fail closed by restarting the blind window.
		c.started = now
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
// the window before now: true when one was seen, when the record is younger
// than the window, or when the account has been Quiet, so it cannot know.
// An empty id is unknown: true.
func (c *Chats) MaybeChatted(accountID int64, id string, now time.Time) bool {
	if id == "" {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.last[key{accountID, id}]; ok && now.Sub(t) <= Window {
		return true
	}
	if h, ok := c.heard[accountID]; !ok || now.Sub(h) > Quiet {
		return true
	}
	return now.Sub(c.started) < Window
}
