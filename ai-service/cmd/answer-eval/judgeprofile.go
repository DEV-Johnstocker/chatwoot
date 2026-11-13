package main

import "github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"

// judgeProfile is the business profile the judge checks replies against. The
// bot writes profile.BankMarker and the system puts the owner's bank box in its
// place, so the judge must see that text too; otherwise every reply that sends
// the account is graded as inventing a bank account.
func judgeProfile(p profile.Profile) string {
	s := profile.Prompt("", p)
	if p.BankTransfer != "" {
		s += "\n\n# Bank account the system inserts where the bot writes " + profile.BankMarker +
			" (owner's own text, a real fact)\n" + p.BankTransfer
	}
	return s
}
