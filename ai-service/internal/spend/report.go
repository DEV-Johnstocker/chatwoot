package spend

import (
	"sort"
	"time"
)

// TrendDays is how many days the trend chart shows, today included.
const TrendDays = 30

// LocalDay is t's date in loc, as midnight UTC (how Postgres dates scan).
func LocalDay(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// MonthStart is the first day of day's month.
func MonthStart(day time.Time) time.Time {
	return time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// Since is the first local day the report and cap checks need: the month
// start or the trend start, whichever is earlier.
func Since(today time.Time) time.Time {
	trend := today.AddDate(0, 0, -(TrendDays - 1))
	if m := MonthStart(today); m.Before(trend) {
		return m
	}
	return trend
}

// Instant turns a local day back into the moment it starts in loc.
func Instant(day time.Time, loc *time.Location) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
}

// Line is one model's or purpose's share of the month.
type Line struct {
	Name   string
	Calls  int64
	Usage  Usage
	Cost   Micros
	Priced bool // false: a model without a price, so Cost is short
}

// DayCost is one day of the trend.
type DayCost struct {
	Day  time.Time
	Cost Micros
}

// Report is the /admin/spend dashboard for one business or all of them.
type Report struct {
	Today, Month           Micros
	TodayCalls, MonthCalls int64
	ByModel, ByPurpose     []Line // this month, most expensive first
	Days                   []DayCost
	Unpriced               []string // models with no price this month
}

func costOf(r Row, prices []Price) (Micros, bool) {
	p, ok := Lookup(prices, r.Model, r.Day)
	if !ok {
		return 0, false
	}
	return p.Cost(r.Usage), true
}

// Build turns usage rows (from Since(today)) into the dashboard.
func Build(rows []Row, prices []Price, today time.Time) Report {
	var rep Report
	month := MonthStart(today)
	first := today.AddDate(0, 0, -(TrendDays - 1))
	byDay := map[time.Time]Micros{}
	byModel, byPurpose := map[string]*Line{}, map[string]*Line{}
	unpriced := map[string]bool{}
	for _, r := range rows {
		cost, priced := costOf(r, prices)
		if !r.Day.Before(first) && !r.Day.After(today) {
			byDay[r.Day] += cost
		}
		if r.Day.Before(month) || r.Day.After(today) {
			continue
		}
		rep.Month += cost
		rep.MonthCalls += r.Calls
		if r.Day.Equal(today) {
			rep.Today += cost
			rep.TodayCalls += r.Calls
		}
		add(byModel, r.Model, r, cost, priced)
		add(byPurpose, r.Purpose, r, cost, priced)
		if !priced {
			unpriced[r.Model] = true
		}
	}
	for d := first; !d.After(today); d = d.AddDate(0, 0, 1) {
		rep.Days = append(rep.Days, DayCost{Day: d, Cost: byDay[d]})
	}
	rep.ByModel, rep.ByPurpose = sorted(byModel), sorted(byPurpose)
	for m := range unpriced {
		rep.Unpriced = append(rep.Unpriced, m)
	}
	sort.Strings(rep.Unpriced)
	return rep
}

func add(lines map[string]*Line, name string, r Row, cost Micros, priced bool) {
	l := lines[name]
	if l == nil {
		l = &Line{Name: name, Priced: true}
		lines[name] = l
	}
	l.Calls += r.Calls
	l.Usage = l.Usage.Add(r.Usage)
	l.Cost += cost
	l.Priced = l.Priced && priced
}

func sorted(lines map[string]*Line) []Line {
	out := make([]Line, 0, len(lines))
	for _, l := range lines {
		out = append(out, *l)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Cost != out[j].Cost {
			return out[i].Cost > out[j].Cost
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Periods of a cap.
const (
	PeriodDay   = "day"
	PeriodMonth = "month"
)

// Status is how far one cap is used up in its current period.
type Status struct {
	AccountID   int64 // 0: the whole service
	Period      string
	PeriodStart time.Time
	Cap, Spent  Micros
	Percent     int
	Level       int // 0, 80 or 100: the highest alert level reached
}

// Statuses works out every cap's use for today and this month.
func Statuses(caps []Cap, rows []Row, prices []Price, today time.Time) []Status {
	month := MonthStart(today)
	var out []Status
	for _, c := range caps {
		var day, mon Micros
		for _, r := range rows {
			if c.AccountID != 0 && r.AccountID != c.AccountID {
				continue
			}
			if r.Day.Before(month) || r.Day.After(today) {
				continue
			}
			cost, _ := costOf(r, prices)
			mon += cost
			if r.Day.Equal(today) {
				day += cost
			}
		}
		if c.Daily > 0 {
			out = append(out, status(c.AccountID, PeriodDay, today, c.Daily, day))
		}
		if c.Monthly > 0 {
			out = append(out, status(c.AccountID, PeriodMonth, month, c.Monthly, mon))
		}
	}
	return out
}

func status(acct int64, period string, start time.Time, limit, spent Micros) Status {
	s := Status{AccountID: acct, Period: period, PeriodStart: start, Cap: limit, Spent: spent}
	s.Percent = int(int64(spent) * 100 / int64(limit))
	switch {
	case s.Percent >= 100:
		s.Level = 100
	case s.Percent >= 80:
		s.Level = 80
	}
	return s
}
