package schedule

import (
	"testing"
	"time"
)

var bkk = time.FixedZone("ICT", 7*3600)

// at is a time in Bangkok on the week of Monday 2026-11-09.
func at(weekday time.Weekday, hh, mm int) time.Time {
	day := 8 + int(weekday) // 2026-11-08 is a Sunday
	return time.Date(2026, 11, day, hh, mm, 0, 0, bkk)
}

func scheduled(days map[time.Weekday]Day) Schedule {
	s := Schedule{AccountID: 1, Mode: ModeScheduled, TimeZone: DefaultTimeZone}
	for d, v := range days {
		s.Days[d] = v
	}
	return s
}

func TestActive_OnAndOffIgnoreHours(t *testing.T) {
	// Arrange
	on, off := Default(1), Default(1)
	off.Mode = ModeOff
	night := at(time.Wednesday, 3, 0)

	// Act / Assert
	if !on.Active(night) {
		t.Error("mode on: want active at any time")
	}
	if off.Active(at(time.Wednesday, 12, 0)) {
		t.Error("mode off: want inactive at any time")
	}
}

func TestActive_DaytimeWindow(t *testing.T) {
	// Arrange
	s := scheduled(map[time.Weekday]Day{time.Monday: {On: true, From: 9 * 60, To: 18 * 60}})
	cases := []struct {
		t    time.Time
		want bool
	}{
		{at(time.Monday, 8, 59), false},
		{at(time.Monday, 9, 0), true},
		{at(time.Monday, 17, 59), true},
		{at(time.Monday, 18, 0), false},
		{at(time.Tuesday, 10, 0), false}, // Tuesday is not on
	}
	for _, c := range cases {
		// Act
		got := s.Active(c.t)

		// Assert
		if got != c.want {
			t.Errorf("%s: got %v, want %v", c.t.Format("Mon 15:04"), got, c.want)
		}
	}
}

func TestActive_OvernightWindowRunsIntoNextDay(t *testing.T) {
	// Arrange: Friday 18:00 until Saturday 09:00.
	s := scheduled(map[time.Weekday]Day{time.Friday: {On: true, From: 18 * 60, To: 9 * 60}})
	cases := []struct {
		t    time.Time
		want bool
	}{
		{at(time.Friday, 17, 59), false},
		{at(time.Friday, 18, 0), true},
		{at(time.Friday, 23, 59), true},
		{at(time.Saturday, 0, 30), true},
		{at(time.Saturday, 8, 59), true},
		{at(time.Saturday, 9, 0), false},
		{at(time.Thursday, 23, 0), false},
	}
	for _, c := range cases {
		// Act
		got := s.Active(c.t)

		// Assert
		if got != c.want {
			t.Errorf("%s: got %v, want %v", c.t.Format("Mon 15:04"), got, c.want)
		}
	}
}

func TestActive_SaturdayNightRunsIntoSunday(t *testing.T) {
	// Arrange: the week wraps from Saturday (6) to Sunday (0).
	s := scheduled(map[time.Weekday]Day{time.Saturday: {On: true, From: 20 * 60, To: 6 * 60}})

	// Act / Assert
	if !s.Active(at(time.Sunday, 5, 0)) {
		t.Error("Sunday 05:00 should still be inside Saturday's hours")
	}
}

func TestActive_SameFromAndToIsWholeDay(t *testing.T) {
	// Arrange
	s := scheduled(map[time.Weekday]Day{time.Sunday: {On: true}})

	// Act / Assert
	for _, h := range []int{0, 12, 23} {
		if !s.Active(at(time.Sunday, h, 0)) {
			t.Errorf("Sunday %02d:00: want active all day", h)
		}
	}
	if s.Active(at(time.Monday, 1, 0)) {
		t.Error("a whole day does not run into the next day")
	}
}

func TestActive_UsesTheScheduleTimeZone(t *testing.T) {
	// Arrange: 09:00–18:00 in Bangkok; 03:00 UTC is 10:00 in Bangkok.
	s := scheduled(map[time.Weekday]Day{time.Monday: {On: true, From: 9 * 60, To: 18 * 60}})
	utc := time.Date(2026, 11, 9, 3, 0, 0, 0, time.UTC)

	// Act / Assert
	if !s.Active(utc) {
		t.Error("want the hours read in the schedule's time zone")
	}
}

func TestActive_UnknownModeKeepsTheBotOn(t *testing.T) {
	// Arrange
	s := Default(1)
	s.Mode = "later-version"

	// Act / Assert
	if !s.Active(at(time.Monday, 3, 0)) {
		t.Error("an unknown mode must not silence the bot")
	}
}

func TestValidate(t *testing.T) {
	// Arrange
	bad := []Schedule{
		{AccountID: 0, Mode: ModeOn, TimeZone: DefaultTimeZone},
		{AccountID: 1, Mode: "maybe", TimeZone: DefaultTimeZone},
		{AccountID: 1, Mode: ModeOn, TimeZone: "Mars/Olympus"},
		{AccountID: 1, Mode: ModeOn, TimeZone: ""},
		{AccountID: 1, Mode: ModeOn, TimeZone: "Local"},
		func() Schedule { s := Default(1); s.Days[2].To = 24 * 60; return s }(),
		func() Schedule { s := Default(1); s.Days[3].From = -1; return s }(),
	}

	// Act / Assert
	if err := Default(1).Validate(); err != nil {
		t.Fatalf("default should be valid: %v", err)
	}
	for i, s := range bad {
		if s.Validate() == nil {
			t.Errorf("case %d: want an error", i)
		}
	}
}

func TestParseClock(t *testing.T) {
	// Arrange
	good := map[string]int{"00:00": 0, "09:30": 570, "23:59": 1439, " 7:05 ": 425}
	bad := []string{"", "9", "24:00", "12:60", "ab:cd", "9:5", "-1:00"}

	// Act / Assert
	for in, want := range good {
		if got, err := ParseClock(in); err != nil || got != want {
			t.Errorf("ParseClock(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range bad {
		if _, err := ParseClock(in); err == nil {
			t.Errorf("ParseClock(%q): want an error", in)
		}
	}
	if Clock(570) != "09:30" {
		t.Errorf("Clock(570) = %q", Clock(570))
	}
}
