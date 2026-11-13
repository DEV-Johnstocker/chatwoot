// Package commentecho links a private reply sent from the comment queue to
// the Chatwoot chat it lands in. Meta answers the send with a message id and
// echoes the same message to Chatwoot, which stores that id as the message's
// source_id. Matching the two lets the bot tell staff the chat started from a
// comment, and why Chatwoot says they cannot reply yet.
package commentecho

import (
	"sync"
	"time"
)

// Defaults: the echo reaches Chatwoot within seconds, so half an hour is
// generous, and the cap bounds memory if echoes never arrive.
const (
	DefaultTTL = 30 * time.Minute
	DefaultMax = 5000
)

// Lead is what staff are told about the comment behind a private reply.
type Lead struct {
	CommentID   string
	FromName    string
	Message     string // the customer's comment
	CommentedAt time.Time
}

// Echoes holds sent private replies by Meta message id until their echo
// arrives. The zero value is ready to use. It lives in memory only: after a
// restart, echoes of replies sent before it get no note, which is harmless.
type Echoes struct {
	TTL time.Duration    // zero: DefaultTTL
	Max int              // zero: DefaultMax
	Now func() time.Time // nil: time.Now

	mu sync.Mutex
	m  map[string]entry
}

type entry struct {
	lead Lead
	at   time.Time
}

// Put remembers lead under Meta's message id. An empty id is ignored.
func (e *Echoes) Put(messageID string, lead Lead) {
	if messageID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	if e.m == nil {
		e.m = map[string]entry{}
	}
	if len(e.m) >= e.max() {
		e.prune(now)
	}
	e.m[messageID] = entry{lead, now}
}

// Take returns the lead for a message id once; a second call, an unknown id
// or an expired one reports false.
func (e *Echoes) Take(messageID string) (Lead, bool) {
	if messageID == "" {
		return Lead{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	en, ok := e.m[messageID]
	if !ok {
		return Lead{}, false
	}
	delete(e.m, messageID)
	if e.now().Sub(en.at) > e.ttl() {
		return Lead{}, false
	}
	return en.lead, true
}

// Len reports how many replies wait for their echo.
func (e *Echoes) Len() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.m)
}

// prune drops expired entries, then the oldest until there is room for one.
func (e *Echoes) prune(now time.Time) {
	for k, en := range e.m {
		if now.Sub(en.at) > e.ttl() {
			delete(e.m, k)
		}
	}
	for len(e.m) >= e.max() {
		oldest, first := "", true
		var at time.Time
		for k, en := range e.m {
			if first || en.at.Before(at) {
				oldest, at, first = k, en.at, false
			}
		}
		delete(e.m, oldest)
	}
}

func (e *Echoes) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Echoes) ttl() time.Duration {
	if e.TTL > 0 {
		return e.TTL
	}
	return DefaultTTL
}

func (e *Echoes) max() int {
	if e.Max > 0 {
		return e.Max
	}
	return DefaultMax
}
