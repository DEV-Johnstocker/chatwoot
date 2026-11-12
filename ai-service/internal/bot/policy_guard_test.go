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
		"plain text", "at most one question", "same thing again", "Thai script", "do not offer a staff member unless they ask for a person",
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

func TestPolicy_SellsBeforeAskingAndUsesEmojiSparingly(t *testing.T) {
	// Arrange
	p := policyPrompt(time.Now())

	// Act + Assert
	for _, want := range []string{
		"say the benefit in their terms",
		"using only facts from the business profile or knowledge excerpts",
		"Never create urgency or scarcity",
		"Never promise results, sales or income",
		"never ask one you already asked or one they skipped",
		"at most two questions about the customer's own situation",
		"do not end two replies in a row with a question",
		"while you do not know what they make or need, one short question about their situation",
		"Emoji: at most one, at the end",
		"Never use one on prices, payment, refunds",
		"no business facts, no question, no handoff",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("policy is missing %q", want)
		}
	}
}

func TestPolicy_ObjectionsGetOneHelpfulCounterThenRespect(t *testing.T) {
	// Arrange
	p := policyPrompt(time.Now())

	// Act + Assert
	for _, want := range []string{
		"accept it warmly first, then once give one or two helpful facts",
		"No pressure, no urgency, no question. If they say no again, stop selling.",
		"usually two or three short lines",
		"address the customer as \"คุณลูกค้า\", or \"คุณ\" followed by their name",
		"never the bare word \"คุณ\" on its own",
		"Use คุณลูกค้า at most once in a reply",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("policy is missing %q", want)
		}
	}
	if strings.Contains(p, "do not repeat the price or push") {
		t.Error("the old accept-and-stop rule must be gone")
	}
}

func TestPolicy_HandoffNamesTheTeamNotASpecialist(t *testing.T) {
	// Arrange: handoff assigns whoever is online with the fewest chats, so the
	// bot must not promise a specialist (DEV-Johnstocker/chatwoot#1).
	now := time.Date(2026, 11, 10, 4, 0, 0, 0, time.UTC)

	// Act
	p := policyPrompt(now)

	// Assert
	for _, banned := range []string{"handles this kind of matter", "ดูแลเรื่องนี้โดยเฉพาะ", "โดยเฉพาะ"} {
		if strings.Contains(p, banned) {
			t.Errorf("policy still promises a specialist: %q", banned)
		}
	}
	for _, want := range []string{"a team member will continue in this chat", "ทีมงาน", "Never say the person is a specialist"} {
		if !strings.Contains(p, want) {
			t.Errorf("policy is missing %q", want)
		}
	}
}

func TestPolicy_OwnerRatingsRound6(t *testing.T) {
	// Arrange: John's ratings of 2026-11-11 (memory: owner-reply-taste).
	p := policyPrompt(time.Now())

	// Act + Assert
	for _, want := range []string{
		"Never write that you do not know",
		"ไม่ทราบ, ยังไม่มีข้อมูล, ยังไม่แน่ใจ, ขอเช็กให้ชัวร์",
		"the first time a customer asks about or for a refund, do not hand off",
		"never offer to cut a sample from their own footage unless the customer asks for one",
		"knowing what they make, or saying they are interested, is not a buying signal",
		"always give the course link and the coupon in a close",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("policy is missing %q", want)
		}
	}
	if strings.Contains(p, "offer to connect them with a staff member") {
		t.Error("'are you a bot' must not offer a staff member any more")
	}
}
