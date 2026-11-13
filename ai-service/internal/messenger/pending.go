package messenger

import (
	"sync"
	"time"
)

// Pending holds referrals until the bot sees the customer's message in
// Chatwoot and saves them on the conversation. Meta's webhook reaches us
// before Chatwoot has made the conversation, so the referral waits here.
// It is memory only: a restart in that gap of a few seconds loses one
// referral, never a message.
type Pending struct {
	TTL time.Duration // how long a referral waits; 0 means one hour
	Max int           // most referrals held; 0 means 10,000
	Now func() time.Time

	mu    sync.Mutex
	items map[string]entry     // by PSID
	seen  map[string]time.Time // event keys already kept, so a retry is not kept again
}

// entry is a referral and when we received it; expiry counts from receipt,
// not from Meta's timestamp, so a delayed retry is not dropped early.
type entry struct {
	r  Referral
	at time.Time
}

// Put keeps r for the customer psid, replacing an older one. key names the
// Meta event; an event already kept within the TTL (Meta retrying a delivery,
// or a replayed body) is skipped and Put reports false.
func (p *Pending) Put(psid, key string, r Referral) bool {
	if psid == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.items == nil {
		p.items, p.seen = map[string]entry{}, map[string]time.Time{}
	}
	now := p.now()
	if key != "" {
		if at, ok := p.seen[key]; ok && now.Sub(at) <= p.ttl() {
			return false
		}
		if len(p.seen) >= p.max() {
			for k, at := range p.seen {
				if now.Sub(at) > p.ttl() {
					delete(p.seen, k)
				}
			}
			if len(p.seen) >= p.max() {
				clear(p.seen) // worst case a retry is kept twice; never unbounded
			}
		}
		p.seen[key] = now
	}
	if _, ok := p.items[psid]; !ok && len(p.items) >= p.max() {
		p.dropExpired(now)
		if len(p.items) >= p.max() {
			p.dropOldest()
		}
	}
	p.items[psid] = entry{r: r, at: now}
	return true
}

// Take returns and removes the referral for psid, if one is waiting and
// not expired.
func (p *Pending) Take(psid string) (Referral, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.items[psid]
	if !ok {
		return Referral{}, false
	}
	delete(p.items, psid)
	if p.now().Sub(e.at) > p.ttl() {
		return Referral{}, false
	}
	return e.r, true
}

// Len is the number of referrals held, expired ones included.
func (p *Pending) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.items)
}

func (p *Pending) dropExpired(now time.Time) {
	for k, e := range p.items {
		if now.Sub(e.at) > p.ttl() {
			delete(p.items, k)
		}
	}
}

func (p *Pending) dropOldest() {
	var oldest string
	var at time.Time
	for k, e := range p.items {
		if oldest == "" || e.at.Before(at) {
			oldest, at = k, e.at
		}
	}
	delete(p.items, oldest)
}

func (p *Pending) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Pending) ttl() time.Duration {
	if p.TTL > 0 {
		return p.TTL
	}
	return time.Hour
}

func (p *Pending) max() int {
	if p.Max > 0 {
		return p.Max
	}
	return 10000
}
