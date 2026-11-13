package main

import (
	"strings"
	"testing"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
)

func TestJudgeProfile_IncludesTheBankBox(t *testing.T) {
	// Arrange
	p := profile.Profile{BusinessName: "Shop", BankTransfer: "SCB 000-0-00000-0"}

	// Act
	got := judgeProfile(p)

	// Assert
	if !strings.Contains(got, "SCB 000-0-00000-0") {
		t.Fatalf("the judge must see the account the system inserts: %s", got)
	}
}

func TestJudgeProfile_NoBankBoxAddsNothing(t *testing.T) {
	// Arrange
	p := profile.Profile{BusinessName: "Shop"}

	// Act
	got := judgeProfile(p)

	// Assert
	if got != profile.Prompt("", p) {
		t.Fatalf("without a bank box the judge sees the bot's profile unchanged")
	}
}
