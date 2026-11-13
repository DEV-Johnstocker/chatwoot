package schedule

import "testing"

// QA-8 (2026-11-14): ParseClock should take exactly HH:MM, as <input type=time> sends.
func TestQA_ParseClockAcceptsOddForms(t *testing.T) {
	for _, in := range []string{"+9:00", "-0:00", "0009:00", "+09:+5"} {
		// Arrange / Act
		_, err := ParseClock(in)

		// Assert: these are accepted today (known bug); flip once fixed.
		if err != nil {
			t.Fatalf("known bug QA-8 is fixed for %q: turn this into a rejection test", in)
		}
	}
}
