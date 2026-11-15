// Package alerts watches the bot for its own failures (replies Chatwoot
// refused, model errors, a full queue, a spike in hand-offs, chats that hit
// the reply limit) and tells the owner: one Slack message when a problem
// starts, a reminder while it lasts, and one when it clears.
//
// Counts live in memory, so a restart starts them afresh. Messages name the
// problem, counts and account ids only: never message text or customer data.
package alerts

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Kind is one thing the monitor watches.
type Kind string

// The kinds, in the order the admin page lists them.
const (
	SendFailed   Kind = "send_failed"   // Chatwoot would not take a bot reply, even on a retry
	LLMError     Kind = "llm_error"     // the AI model (and its fallback) failed
	QueueFull    Kind = "queue_full"    // a webhook was answered 429: the bot is overloaded
	HandoffSpike Kind = "handoff_spike" // an unusual share of chats went to staff
	ReplyLimit   Kind = "reply_limit"   // a chat hit the per-customer reply limit
)

var kinds = []Kind{SendFailed, LLMError, QueueFull, HandoffSpike, ReplyLimit}

// Window is how far back event counts look. A problem fires once per window
// at most, and clears when a whole window passes without the event.
const Window = 15 * time.Minute

// SpikeWindow is how far back the hand-off share looks: long enough for a
// small shop to have SpikeMinTurns answered messages.
const SpikeWindow = time.Hour

// Defaults for the hand-off spike: at least SpikeMinTurns answered messages
// in SpikeWindow, and at least this share of them handed to staff.
const (
	SpikeMinTurns     = 10
	DefaultSpikeShare = 0.5
)

// Remind repeats an alert this often while it lasts (as the provider check does).
const Remind = 6 * time.Hour

// minEvents is how many events in Window raise each event kind. One 429 or
// one rate-limited chat is already worth knowing; a single failed send or
// model error is usually a blip.
var minEvents = map[Kind]int{SendFailed: 3, LLMError: 3, QueueFull: 1, ReplyLimit: 1}

// Titles name each kind for the owner (Slack and the admin page).
var Titles = map[Kind]string{
	SendFailed:   "Bot replies are not reaching Chatwoot",
	LLMError:     "The AI model is failing",
	QueueFull:    "The bot is overloaded (queue full)",
	HandoffSpike: "Unusually many chats handed to staff",
	ReplyLimit:   "Chats hit the reply limit",
}

// advice says what the owner should look at, per kind (Slack only).
var advice = map[Kind]string{
	SendFailed:   "Customers may be waiting with no answer. Check that Chatwoot is up and the bot token is valid.",
	LLMError:     "Customers get the failure message and staff take over. Check the AI providers page and the provider's credit.",
	QueueFull:    "Chatwoot is asked to retry later. Check the ai-service log and the AI provider's speed.",
	HandoffSpike: "Check the unanswered questions page and the AI providers page.",
	ReplyLimit:   "The bot stopped answering these chats and passed them to staff, so one customer cannot use up AI credit.",
}

// Notifier sends one alert line (Slack).
type Notifier interface {
	Notify(ctx context.Context, text string) error
}

// Status is a problem going on now, for the admin page.
type Status struct {
	Kind  Kind
	Title string
	Since time.Time
}

// Monitor counts bot events and raises alerts. The zero value is not ready:
// use New.
type Monitor struct {
	notifier   Notifier // nil: alerts show on the admin page and in the log only
	label      string   // environment shown in every message, e.g. "STAGING"
	spikeShare float64
	now        func() time.Time
	log        *slog.Logger

	mu     sync.Mutex
	events map[Kind][]event
	turns  []turn
	active map[Kind]*state
}

type event struct {
	at   time.Time
	acct int64
}

type turn struct {
	at        time.Time
	handedOff bool
}

type state struct {
	since, sent time.Time
}

// Options configures a Monitor.
type Options struct {
	Notifier   Notifier
	Label      string           // environment name for messages; "" leaves it out
	SpikeShare float64          // 0 uses DefaultSpikeShare
	Now        func() time.Time // nil uses time.Now
	Logger     *slog.Logger
}

// New returns a Monitor.
func New(o Options) *Monitor {
	m := &Monitor{notifier: o.Notifier, label: o.Label, spikeShare: o.SpikeShare, now: o.Now, log: o.Logger,
		events: map[Kind][]event{}, active: map[Kind]*state{}}
	if m.spikeShare <= 0 || m.spikeShare > 1 {
		m.spikeShare = DefaultSpikeShare
	}
	if m.now == nil {
		m.now = time.Now
	}
	if m.log == nil {
		m.log = slog.Default()
	}
	return m
}

// Record counts one event of kind for an account. Safe on a nil Monitor.
func (m *Monitor) Record(kind Kind, acct int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.events[kind] = append(prune(m.events[kind], now.Add(-Window)), event{now, acct})
}

// Turn counts one answered customer message: replied, or handed to staff.
// Safe on a nil Monitor.
func (m *Monitor) Turn(handedOff bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.turns = append(pruneTurns(m.turns, now.Add(-SpikeWindow)), turn{now, handedOff})
}

// Active lists the problems going on now. Safe on a nil Monitor.
func (m *Monitor) Active() []Status {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Status
	for _, k := range kinds {
		if s := m.active[k]; s != nil {
			out = append(out, Status{Kind: k, Title: Titles[k], Since: s.since})
		}
	}
	return out
}

// Run checks every interval until ctx ends.
func (m *Monitor) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Check(ctx)
		}
	}
}

// message is one alert decided by Check, sent after the lock is released.
type message struct {
	kind  Kind
	text  string
	start bool // a new problem: only marked active once the message went out
}

// Check compares the counts with the rules and sends what changed: a new
// problem, a reminder every Remind while it lasts, or its recovery.
func (m *Monitor) Check(ctx context.Context) {
	for _, msg := range m.decide() {
		err := m.send(ctx, msg.text)
		if err != nil {
			m.log.Warn("bot alert not sent", "kind", string(msg.kind), "error", err)
		}
		m.commit(msg, err == nil)
	}
}

// decide works out what to send, and applies the recoveries and reminders.
func (m *Monitor) decide() []message {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	var out []message
	for _, k := range kinds {
		firing, detail := m.firing(k, now)
		s := m.active[k]
		switch {
		case firing && s == nil:
			out = append(out, message{k, m.line(":warning:", "Bot alert: "+Titles[k], detail+" "+advice[k]), true})
		case firing && now.Sub(s.sent) >= Remind:
			s.sent = now
			out = append(out, message{k, m.line(":warning:", "Still going on: "+Titles[k], detail), false})
		case !firing && s != nil && m.cleared(k, now):
			delete(m.active, k)
			out = append(out, message{k, m.line(":white_check_mark:", "Cleared: "+Titles[k], "since "+s.since.Format("15:04 MST")+"."), false})
		}
	}
	return out
}

// commit marks a new problem active once its message went out; a failed
// message is tried again on the next check. With no notifier the problem is
// still marked, so the admin page shows it.
func (m *Monitor) commit(msg message, sent bool) {
	if !msg.start || !sent {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active[msg.kind] == nil {
		now := m.now()
		m.active[msg.kind] = &state{since: now, sent: now}
	}
}

func (m *Monitor) send(ctx context.Context, text string) error {
	if m.notifier == nil {
		m.log.Warn("bot alert", "text", text) // the text holds counts and account ids only
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return m.notifier.Notify(ctx, text)
}

// firing reports whether kind's rule holds now, with the counts behind it.
func (m *Monitor) firing(k Kind, now time.Time) (bool, string) {
	if k == HandoffSpike {
		m.turns = pruneTurns(m.turns, now.Add(-SpikeWindow))
		handed := 0
		for _, t := range m.turns {
			if t.handedOff {
				handed++
			}
		}
		total := len(m.turns)
		if total < SpikeMinTurns || float64(handed) < m.spikeShare*float64(total) {
			return false, ""
		}
		return true, fmt.Sprintf("%d of the last %d answered messages (%d%%) in the last hour went to staff.", handed, total, handed*100/total)
	}
	m.events[k] = prune(m.events[k], now.Add(-Window))
	evs := m.events[k]
	if len(evs) < minEvents[k] {
		return false, ""
	}
	return true, fmt.Sprintf("%d in the last %d min (%s).", len(evs), int(Window.Minutes()), accounts(evs))
}

// cleared is true when the problem has really gone: no event in a whole
// window, or (for the spike) the share fell back under the line.
func (m *Monitor) cleared(k Kind, now time.Time) bool {
	if k == HandoffSpike {
		return true
	}
	return len(prune(m.events[k], now.Add(-Window))) == 0
}

func (m *Monitor) line(icon, title, detail string) string {
	env := ""
	if m.label != "" {
		env = "[" + m.label + "] "
	}
	return strings.TrimSpace(icon + " " + env + title + ". " + detail)
}

// accounts names the accounts behind the events, e.g. "account 1" or "accounts 1, 3".
func accounts(evs []event) string {
	var ids []int64
	for _, e := range evs {
		if !slices.Contains(ids, e.acct) {
			ids = append(ids, e.acct)
		}
	}
	slices.Sort(ids)
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	if len(ids) == 1 {
		return "account " + parts[0]
	}
	return "accounts " + strings.Join(parts, ", ")
}

func prune(evs []event, after time.Time) []event {
	i := 0
	for i < len(evs) && !evs[i].at.After(after) {
		i++
	}
	return evs[i:]
}

func pruneTurns(ts []turn, after time.Time) []turn {
	i := 0
	for i < len(ts) && !ts[i].at.After(after) {
		i++
	}
	return ts[i:]
}
