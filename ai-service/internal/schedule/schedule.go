// Package schedule is each account's main switch for the chat bot: on, off,
// or on during set weekly hours. When the bot is off, new customer messages
// go straight to staff (the conversation moves from pending to open) and the
// bot sends nothing. Issue: DEV-Johnstocker/chatwoot#1
package schedule

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Mode is the main switch.
type Mode string

const (
	ModeOn        Mode = "on"        // the bot answers at all times
	ModeOff       Mode = "off"       // the bot never answers; staff get every chat
	ModeScheduled Mode = "scheduled" // the bot answers only inside Days
)

// DefaultTimeZone is used when an account has not chosen one.
const DefaultTimeZone = "Asia/Bangkok"

// Day is the bot's hours on one weekday, in minutes after local midnight.
// From == To means the whole day. From > To runs past midnight into the next
// day (for example 18:00–09:00).
type Day struct {
	On   bool `json:"on"`
	From int  `json:"from"`
	To   int  `json:"to"`
}

// Schedule is one account's switch and weekly hours.
type Schedule struct {
	AccountID int64
	Mode      Mode
	TimeZone  string
	Days      [7]Day // indexed by time.Weekday: 0 is Sunday
	UpdatedAt time.Time
}

// Default is the schedule of an account that never saved one: the bot is on,
// as it was before this switch existed. The weekly hours are a starting
// point for the form (every day 09:00–18:00).
func Default(accountID int64) Schedule {
	s := Schedule{AccountID: accountID, Mode: ModeOn, TimeZone: DefaultTimeZone}
	for i := range s.Days {
		s.Days[i] = Day{On: true, From: 9 * 60, To: 18 * 60}
	}
	return s
}

// Store keeps schedules.
type Store interface {
	// Get returns the account's schedule, or Default when none is saved.
	Get(ctx context.Context, accountID int64) (Schedule, error)
	Save(ctx context.Context, s Schedule) error
}

// Validate checks a schedule before it is saved.
func (s Schedule) Validate() error {
	var errs []error
	if s.AccountID < 1 {
		errs = append(errs, errors.New("account ID must be a positive number"))
	}
	switch s.Mode {
	case ModeOn, ModeOff, ModeScheduled:
	default:
		errs = append(errs, fmt.Errorf("mode must be on, off or scheduled, not %q", s.Mode))
	}
	if _, err := location(s.TimeZone); err != nil {
		errs = append(errs, fmt.Errorf("time zone %q is not known", s.TimeZone))
	}
	for i, d := range s.Days {
		if d.From < 0 || d.From >= 24*60 || d.To < 0 || d.To >= 24*60 {
			errs = append(errs, fmt.Errorf("%s: hours must be between 00:00 and 23:59", time.Weekday(i)))
		}
	}
	return errors.Join(errs...)
}

// Active reports whether the bot answers at t.
func (s Schedule) Active(t time.Time) bool {
	switch s.Mode {
	case ModeOff:
		return false
	case ModeScheduled:
	default: // ModeOn, or an unknown value from an older or newer version
		return true
	}
	loc, err := location(s.TimeZone)
	if err != nil {
		loc, _ = location(DefaultTimeZone)
	}
	lt := t.In(loc)
	day, m := lt.Weekday(), lt.Hour()*60+lt.Minute()
	today := s.Days[day]
	if today.On {
		switch {
		case today.From == today.To:
			return true
		case today.From < today.To:
			if m >= today.From && m < today.To {
				return true
			}
		case m >= today.From: // runs past midnight; this is the evening part
			return true
		}
	}
	prev := s.Days[(day+6)%7]
	return prev.On && prev.From > prev.To && m < prev.To
}

func location(name string) (*time.Location, error) {
	// "Local" would mean the server's own zone, which the owner cannot see.
	if strings.TrimSpace(name) == "" || name == "Local" || len(name) > 64 {
		return nil, errors.New("not an IANA time zone name")
	}
	return time.LoadLocation(name)
}

// ParseClock reads "HH:MM" (24-hour) as minutes after midnight.
func ParseClock(s string) (int, error) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return 0, fmt.Errorf("time %q must look like 09:00", s)
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || len(m) != 2 || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, fmt.Errorf("time %q must look like 09:00", s)
	}
	return hh*60 + mm, nil
}

// Clock writes minutes after midnight as "HH:MM".
func Clock(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}
