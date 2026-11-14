// Package ratelimit is a fixed one-minute window counter per key. When it is
// full it evicts only expired windows; if still full it refuses new keys
// (fails closed) instead of forgetting counts, so flooding it with new keys
// cannot reset anyone's limit.
package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ClientIP is the caller's address for rate limits and logs. Behind the
// Cloudflare tunnel every request arrives from the tunnel container on a
// private network, so the visitor's address is taken from CF-Connecting-IP,
// but only then: a direct caller cannot pick its own key.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback()) {
		if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); net.ParseIP(cf) != nil {
			return cf
		}
	}
	return host
}

// Window is the counting period.
const Window = time.Minute

// Limiter counts calls per key.
type Limiter struct {
	Max    int           // most keys tracked at once; 0 means 10000
	Period time.Duration // counting window; 0 means Window (one minute)

	mu   sync.Mutex
	keys map[string]*entry
	now  func() time.Time
}

type entry struct {
	start time.Time
	n     int
}

// Allow counts one call for key and reports whether it is within limit.
func (l *Limiter) Allow(key string, limit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entryLocked(key)
	if e == nil {
		return false
	}
	e.n++
	return e.n <= limit
}

// Reserve counts one call for key only if it is still under limit, in one
// step, so concurrent callers can never pass more than limit between them.
// undo gives the slot back (e.g. the attempt turned out fine); calling it
// more than once is harmless. When the table is full and key is new, Reserve
// refuses (fails closed) and undo is nil.
func (l *Limiter) Reserve(key string, limit int) (undo func(), ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entryLocked(key)
	if e == nil || e.n >= limit {
		return nil, false
	}
	e.n++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.keys[key] == e && e.n > 0 {
				e.n--
			}
		})
	}, true
}

// entryLocked returns key's entry for the current window, starting a new
// one if needed; nil if the table is full of live keys. l.mu must be held.
func (l *Limiter) entryLocked(key string) *entry {
	now := time.Now()
	if l.now != nil {
		now = l.now()
	}
	if l.keys == nil {
		l.keys = map[string]*entry{}
	}
	e := l.keys[key]
	period := l.period()
	if e == nil || now.Sub(e.start) >= period {
		if e == nil && len(l.keys) >= l.max() {
			for k, old := range l.keys {
				if now.Sub(old.start) >= period {
					delete(l.keys, k)
				}
			}
			if len(l.keys) >= l.max() {
				return nil
			}
		}
		e = &entry{start: now}
		l.keys[key] = e
	}
	return e
}

// PrefixKey is the rate-limit key of an address: an IPv4 address as is, an
// IPv6 address as its /64 network, since one subscriber usually holds a
// whole /64 and could otherwise rotate addresses to reset a limit.
func PrefixKey(addr string) string {
	ip := net.ParseIP(addr)
	switch {
	case ip == nil:
		return addr
	case ip.To4() != nil:
		return ip.To4().String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// Reached reports whether key has already used limit calls in the current
// window, without counting a call.
func (l *Limiter) Reached(key string, limit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.now != nil {
		now = l.now()
	}
	e := l.keys[key]
	return e != nil && now.Sub(e.start) < l.period() && e.n >= limit
}

// Refund takes back one call counted for key in the current window, for a
// call reserved with Allow that turned out not to count (a right password).
func (l *Limiter) Refund(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e := l.keys[key]; e != nil && e.n > 0 {
		e.n--
	}
}

func (l *Limiter) period() time.Duration {
	if l.Period > 0 {
		return l.Period
	}
	return Window
}

func (l *Limiter) max() int {
	if l.Max > 0 {
		return l.Max
	}
	return 10000
}
