package admin

import (
	"errors"
	"strings"
	"testing"
)

// Regression tests from the QA run on the bot switch (2026-11-14).
// QA-4: a failed save must not show the unsaved form as the live state.
func TestQA_BannerAfterFailedSaveShowsSavedState(t *testing.T) {
	// Arrange: saved state is on (default); the save of "off" fails.
	mux, mh := hoursEnv(t, true)
	mh.saveErr = errors.New("db down")

	// Act
	body := serve(mux, hpost(hoursForm("1", "off"))).Body.String()

	// Assert: the bar shows the saved state (on), not the failed form (off).
	if !strings.Contains(body, "Right now the bot is answering.") || !strings.Contains(body, "the form below was not saved") {
		t.Error("status bar does not show the saved state after a failed save")
	}
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
	if strings.Contains(body, "Right now the bot is off.") || !strings.Contains(body, "Right now the bot is answering.") {
		t.Error("status bar shows the rejected form instead of the saved state")
	}
}

// QA-7: "on during set hours" with no day ticked would never answer: refused.
func TestQA_ScheduledWithNoDaysIsRefused(t *testing.T) {
	// Arrange
	mux, mh := hoursEnv(t, true)
	v := hoursForm("1", "scheduled")
	for i := 0; i < 7; i++ {
		v.Del("day" + string(rune('0'+i)) + "_on")
	}

	// Act
	res := serve(mux, hpost(v))

	// Assert
	if res.Code != 422 || mh.saves != 0 || !strings.Contains(res.Body.String(), "tick at least one day") {
		t.Errorf("status %d, saves %d; want 422 with a hint and nothing saved", res.Code, mh.saves)
	}
}

// QA-4: when the saved switch cannot be read either, say so.
func TestQA_BannerWhenSavedStateUnreadable(t *testing.T) {
	// Arrange
	mux, mh := hoursEnv(t, true)
	mh.saveErr = errors.New("db down")
	mh.getErr = errors.New("db down")

	// Act
	body := serve(mux, hpost(hoursForm("1", "off"))).Body.String()

	// Assert
	if !strings.Contains(body, "Could not read the saved switch") || strings.Contains(body, "Right now the bot is off.") {
		t.Error("want the unknown-state notice, not a guessed state")
	}
}

// QA-9: an unticked day's blank times do not block the save.
func TestQA_UntickedDayWithBlankTimesSaves(t *testing.T) {
	// Arrange
	mux, mh := hoursEnv(t, true)
	v := hoursForm("1", "scheduled")
	v.Del("day0_on")
	v.Set("day0_from", "")
	v.Set("day0_to", "")

	// Act
	res := serve(mux, hpost(v))

	// Assert
	if res.Code != 303 || mh.saved[1].Days[0].On || mh.saved[1].Days[0].From != 9*60 {
		t.Errorf("status %d, day0 %+v", res.Code, mh.saved[1].Days[0])
	}
}
