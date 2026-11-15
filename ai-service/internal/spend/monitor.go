package spend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Store is what the monitor and the admin page need from Postgres.
type Store interface {
	Rows(ctx context.Context, from, to time.Time, tz string, accountID int64) ([]Row, error)
	Caps(ctx context.Context) ([]Cap, error)
	SaveCap(ctx context.Context, c Cap) error
	MarkAlert(ctx context.Context, a Alert) (bool, error)
	UnmarkAlert(ctx context.Context, a Alert) error
	Prune(ctx context.Context, before time.Time) (int64, error)
}

// Notifier posts an alert (Slack).
type Notifier interface {
	Notify(ctx context.Context, text string) error
}

// Retention is how long usage rows are kept.
const Retention = 400 * 24 * time.Hour

// Monitor checks the caps on a timer, sends each 80% / 100% alert once per
// period and keeps the latest statuses for the /admin banner. It never
// stops the bot.
type Monitor struct {
	Store   Store
	Prices  []Price
	Loc     *time.Location
	Notify  Notifier // nil: no Slack, the banner still shows
	Label   string   // which deployment, e.g. the Chatwoot host
	PageURL string   // link to /admin/spend in the alert; optional
	Logger  *slog.Logger
	Now     func() time.Time

	mu        sync.Mutex
	statuses  []Status
	checked   bool
	lastPrune time.Time
}

// TZ is the Postgres time zone name of loc.
func TZ(loc *time.Location) string {
	if loc == nil || loc.String() == "Local" {
		return "UTC"
	}
	return loc.String()
}

func (m *Monitor) now() time.Time { return now(m.Now) }

// Statuses returns the cap statuses of the last check; ok is false before
// the first check finished.
func (m *Monitor) Statuses() (out []Status, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append(out, m.statuses...), m.checked
}

// Warnings returns the caps at 80% or more, for the banner.
func (m *Monitor) Warnings() []Status {
	all, _ := m.Statuses()
	var out []Status
	for _, s := range all {
		if s.Level > 0 {
			out = append(out, s)
		}
	}
	return out
}

// Run checks now and then every interval until ctx ends.
func (m *Monitor) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		cctx, cancel := context.WithTimeout(ctx, time.Minute)
		if err := m.Check(cctx); err != nil {
			m.Logger.Warn("ai spend check failed", "error", err)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Check recomputes every cap and sends the alerts that are due.
func (m *Monitor) Check(ctx context.Context) error {
	now := m.now()
	today := LocalDay(now, m.Loc)
	caps, err := m.Store.Caps(ctx)
	if err != nil {
		return err
	}
	var statuses []Status
	if len(caps) > 0 {
		rows, err := m.Store.Rows(ctx, Instant(MonthStart(today), m.Loc), Instant(today.AddDate(0, 0, 1), m.Loc), TZ(m.Loc), -1)
		if err != nil {
			return err
		}
		statuses = Statuses(caps, rows, m.Prices, today)
	}
	m.mu.Lock()
	m.statuses, m.checked = statuses, true
	prune := now.Sub(m.lastPrune) > 24*time.Hour
	if prune {
		m.lastPrune = now
	}
	m.mu.Unlock()
	var errs []error
	for _, s := range statuses {
		if s.Level > 0 {
			errs = append(errs, m.alert(ctx, s))
		}
	}
	if prune {
		if n, err := m.Store.Prune(ctx, now.Add(-Retention)); err != nil {
			errs = append(errs, err)
		} else if n > 0 {
			m.Logger.Info("old ai usage pruned", "rows", n)
		}
	}
	return errors.Join(errs...)
}

// alert sends one message for the highest new level of s; levels already
// sent this period are skipped. A failed send is forgotten so the next
// check retries.
func (m *Monitor) alert(ctx context.Context, s Status) error {
	var fresh []Alert
	for _, level := range []int{80, 100} {
		if level > s.Level {
			continue
		}
		a := Alert{AccountID: s.AccountID, Period: s.Period, PeriodStart: s.PeriodStart, Level: level, Cap: s.Cap, Spent: s.Spent}
		ok, err := m.Store.MarkAlert(ctx, a)
		if err != nil {
			return err
		}
		if ok {
			fresh = append(fresh, a)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	if m.Notify == nil {
		m.Logger.Warn("ai spend cap reached, no Slack webhook set", "account_id", s.AccountID, "period", s.Period, "percent", s.Percent)
		return nil
	}
	if err := m.Notify.Notify(ctx, Message(s, m.Label, m.PageURL, m.Loc)); err != nil {
		for _, a := range fresh {
			if uerr := m.Store.UnmarkAlert(context.WithoutCancel(ctx), a); uerr != nil {
				err = errors.Join(err, uerr)
			}
		}
		return fmt.Errorf("spend alert not sent: %w", err)
	}
	m.Logger.Info("ai spend alert sent", "account_id", s.AccountID, "period", s.Period, "level", s.Level)
	return nil
}

// Money formats an amount in US dollars: two decimals, four below a cent.
func Money(m Micros) string {
	usd := m.USD()
	if usd != 0 && usd < 0.01 && usd > -0.01 {
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

// Who names a cap's scope.
func Who(accountID int64) string {
	if accountID == 0 {
		return "whole service"
	}
	return fmt.Sprintf("account %d", accountID)
}

// Message is the Slack text of an alert. It holds amounts and the account
// number only: no customer data.
func Message(s Status, label, pageURL string, loc *time.Location) string {
	icon, verb := "⚠️", "is at"
	if s.Level >= 100 {
		icon, verb = "🚨", "has reached"
	}
	when := s.PeriodStart.Format("2 Jan 2006")
	if s.Period == PeriodMonth {
		when = s.PeriodStart.Format("January 2006")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s AI spend %s %d%% of the %s cap", icon, verb, s.Percent, map[string]string{PeriodDay: "daily", PeriodMonth: "monthly"}[s.Period])
	if label != "" {
		fmt.Fprintf(&b, " on %s", label)
	}
	fmt.Fprintf(&b, " (%s): %s of %s, %s, %s time.\nThe bot keeps answering; caps only alert.",
		Who(s.AccountID), Money(s.Spent), Money(s.Cap), when, TZ(loc))
	if pageURL != "" {
		fmt.Fprintf(&b, " <%s|Open AI spend>", pageURL)
	}
	return b.String()
}

// Slack posts to a Slack incoming webhook.
type Slack struct {
	URL    string
	Client *http.Client
}

// ErrBadWebhook is a webhook address that is not a Slack incoming webhook.
var ErrBadWebhook = errors.New("SPEND_SLACK_WEBHOOK_URL must be an https://hooks.slack.com/ address")

// NewSlack checks raw and returns the notifier.
func NewSlack(raw string) (*Slack, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host != "hooks.slack.com" || u.User != nil || len(u.Path) < 2 {
		return nil, ErrBadWebhook
	}
	return &Slack{URL: u.String(), Client: &http.Client{Timeout: 10 * time.Second}}, nil
}

// Notify implements Notifier. Errors never include the webhook address.
func (s *Slack) Notify(ctx context.Context, text string) error {
	body, _ := json.Marshal(map[string]string{"text": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(body))
	if err != nil {
		return errors.New("slack request failed")
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.Client.Do(req)
	if err != nil {
		return errors.New("slack post failed") // the raw error carries the secret URL
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4<<10))
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("slack status %d", res.StatusCode)
	}
	return nil
}
