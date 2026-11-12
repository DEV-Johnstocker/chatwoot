package bot

import (
	"regexp"
	"strings"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/numtext"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
)

// bankMarker matches profile.BankMarker and near-misses ("[[ bank ]]").
var bankMarker = regexp.MustCompile(`(?i)\[\[\s*bank\s*\]\]`)

// guard is what a reply is checked against before it reaches a customer:
// only the business profile, which only the owner can change. Knowledge rows
// are not trusted for numbers: synced pages and sheets can change after the
// owner approved them.
type guard struct {
	bank    string          // the owner's bank-transfer text, put in for the marker
	numbers map[string]bool // every long number in the profile, as digits
}

func newGuard(p profile.Profile) guard {
	g := guard{bank: p.BankTransfer, numbers: map[string]bool{}}
	for _, field := range []string{p.BusinessName, p.About, p.Tone, p.Contact, p.Rules, p.BankTransfer} {
		// Line by line and space by space too, so two numbers the owner wrote
		// side by side ("044-115-229 081-516-9097") are each known alone.
		for _, line := range strings.Split(field, "\n") {
			for _, part := range append([]string{line}, strings.Fields(line)...) {
				for _, n := range numtext.Long(part) {
					g.numbers[n] = true
				}
			}
		}
	}
	return g
}

// check fills in the bank marker and reports whether the reply may be sent:
// a marker with no bank text set, or any long number (phone, account, card,
// however written) that is not in the business profile, holds the reply.
func (g guard) check(reply string) (string, bool) {
	if bankMarker.MatchString(reply) {
		if g.bank == "" {
			return reply, false
		}
		reply = bankMarker.ReplaceAllLiteralString(reply, g.bank)
	}
	for _, n := range numtext.Long(reply) {
		if !g.numbers[n] {
			return reply, false
		}
	}
	return reply, true
}
