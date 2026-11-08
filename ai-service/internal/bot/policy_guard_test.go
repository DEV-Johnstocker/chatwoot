package bot

import (
	"strings"
	"testing"
	"time"
)

func TestPolicy_ForbidsCollectingSecretsInChat(t *testing.T) {
	// Arrange
	now := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)

	// Act
	p := policyPrompt(now)

	// Assert
	for _, want := range []string{"card numbers", "CVV", "OTP", "passwords", "ID card numbers", "hand off"} {
		if !strings.Contains(p, want) {
			t.Errorf("policy is missing %q", want)
		}
	}
	if !strings.Contains(p, "even if the business profile") {
		t.Error("policy does not say the profile cannot lift the rule")
	}
}

func TestPolicy_ChatsLikeAPerson(t *testing.T) {
	// Arrange
	now := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)

	// Act
	p := policyPrompt(now)

	// Assert
	for _, want := range []string{
		"plain text", "at most one question", "same thing again", "Thai script", "offer to connect them with a staff member",
		"hand off in the same message", "flirting", "never say staff will reply", "too expensive", "hand off now instead of offering", "several messages since your last reply",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("policy is missing %q", want)
		}
	}
}

func TestPolicy_MediaDoesItsPartBeforeHandoff(t *testing.T) {
	// Arrange
	p := policyPrompt(time.Now())

	// Act + Assert: slips are read and checked, never confirmed; error screens are helped first.
	for _, want := range []string{
		"say the amount, date and who received it",
		"never say the payment is confirmed",
		"then hand off",
		"If a photo shows an error or a problem screen",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("policy is missing %q", want)
		}
	}
}
