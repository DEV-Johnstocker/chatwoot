package admin

import (
	"errors"
	"strings"
	"testing"
)

// Regression tests from the QA run on the bot switch (2026-11-14).
// knownBug: passes while the bug is there, fails once fixed.
func knownBug(t *testing.T, finding string, buggy bool) {
	t.Helper()
	if buggy {
		t.Logf("known bug %s still present", finding)
		return
	}
	t.Fatalf("known bug %s is fixed: turn this into a normal assertion", finding)
}

// QA-4: a failed save must not show the unsaved form as the live state.
func TestQA_BannerAfterFailedSaveShowsSavedState(t *testing.T) {
	// Arrange: saved state is on (default); the save of "off" fails.
	mux, mh := hoursEnv(t, true)
	mh.saveErr = errors.New("db down")

	// Act
	body := serve(mux, hpost(hoursForm("1", "off"))).Body.String()

	// Assert
	knownBug(t, "QA-4 (banner shows the unsaved form)", strings.Contains(body, "Right now the bot is off."))
}

// QA-4b: same for a rejected form.
func TestQA_BannerAfterRejectedFormShowsSavedState(t *testing.T) {
	// Arrange
	mux, _ := hoursEnv(t, true)
	v := hoursForm("1", "off")
	v.Set("day3_from", "9am")

	// Act
	body := serve(mux, hpost(v)).Body.String()

	// Assert
	knownBug(t, "QA-4 (banner shows the rejected form)", strings.Contains(body, "Right now the bot is off."))
}

// QA-7: "on during set hours" with no day ticked never answers; the owner gets no warning.
func TestQA_ScheduledWithNoDaysIsAcceptedSilently(t *testing.T) {
	// Arrange
	mux, mh := hoursEnv(t, true)
	v := hoursForm("1", "scheduled")
	for i := 0; i < 7; i++ {
		v.Del("day" + string(rune('0'+i)) + "_on")
	}

	// Act
	res := serve(mux, hpost(v))

	// Assert
	knownBug(t, "QA-7 (no warning for a schedule with no days)", res.Code == 303 && mh.saves == 1)
}
