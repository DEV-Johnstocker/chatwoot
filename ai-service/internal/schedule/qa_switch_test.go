package schedule

import (
	"testing"
	"time"
)

// QA-8 (2026-11-14): ParseClock takes exactly HH:MM, as <input type=time> sends.
func TestQA_ParseClockRejectsOddForms(t *testing.T) {
	for _, in := range []string{"+9:00", "-0:00", "0009:00", "+09:+5", "9:00", "09:0"} {
		// Arrange / Act
		_, err := ParseClock(in)

		// Assert
		if err == nil {
			t.Errorf("ParseClock(%q): want an error", in)
		}
	}
}

// QA-6: an end of 23:59 runs to midnight.
func TestQA_EndAt2359CoversTheLastMinute(t *testing.T) {
	// Arrange
	s := scheduled(map[time.Weekday]Day{time.Monday: {On: true, From: 0, To: 23*60 + 59}})

	// Act / Assert
	if !s.Active(time.Date(2026, 11, 9, 23, 59, 30, 0, bkk)) {
		t.Error("Monday 23:59:30 should be inside 00:00–23:59")
	}
	if s.Active(time.Date(2026, 11, 10, 0, 0, 30, 0, bkk)) {
		t.Error("Tuesday 00:00 is not part of Monday's hours")
	}
}

// QA-7: scheduled with no day ticked is refused; off with none is fine.
func TestQA_ScheduledNeedsADay(t *testing.T) {
	// Arrange
	s := Default(1)
	for i := range s.Days {
		s.Days[i].On = false
	}
	off := s
	s.Mode, off.Mode = ModeScheduled, ModeOff

	// Act / Assert
	if s.Validate() == nil {
		t.Error("scheduled with no days: want an error")
	}
	if err := off.Validate(); err != nil {
		t.Errorf("off with no days: %v", err)
	}
}
