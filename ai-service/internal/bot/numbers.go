package bot

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/numtext"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
)

var (
	// bankMarker matches profile.BankMarker and near-misses ("[[ bank ]]").
	bankMarker = regexp.MustCompile(`(?i)\[\[\s*bank\s*\]\]`)
	// link is a web address written as one word.
	link = regexp.MustCompile(`(?i)(?:https?://|www\.)\S+`)
	// paymentWords mark a reply about paying: there, the only account or
	// phone number allowed is the bank text the bot puts in for the marker.
	paymentWords = regexp.MustCompile(`(?i)โอน|บัญชี|พร้อมเพย์|พร้อมเพ|promptpay|prompt\s*pay|ธนาคาร|ชำระ|จ่ายเงิน|สลิป|qr|account|transfer|bank|wallet|วอลเล็ท|ทรูมันนี่`)
)

// maxPaymentDigits is the most loose digits a reply about paying may hold
// outside prices, times, years and the bank text: well below any phone,
// account or PromptPay ID (9 digits and up, so half of one is caught too),
// above a day, a seat count and a few instalments.
const maxPaymentDigits = 6

// guard is what a reply is checked against before it reaches a customer:
// only the business profile, which only the owner can change. Knowledge rows
// are not trusted for numbers: synced pages and sheets can change after the
// owner approved them.
type guard struct {
	bank    string          // the owner's bank-transfer text, put in for the marker
	numbers map[string]bool // every long number in the profile, as digits
	// words are the owner's own links, coupon codes and IDs: profile words
	// with a letter and a digit. Copied exactly, their digits are not an
	// account ("JOHNAUTOCUT99", a course link with an ID). A bare number is
	// never one of them, so a profile phone still counts in a payment reply.
	words []string
	// plain are the profile's long numbers written outside links: phones and
	// bank numbers. A link that holds one of them is not exempt.
	plain map[string]bool
}

func newGuard(p profile.Profile) guard {
	g := guard{bank: p.BankTransfer, numbers: map[string]bool{}, plain: map[string]bool{}}
	seen := map[string]bool{}
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
	for _, field := range []string{p.BusinessName, p.About, p.Tone, p.Contact, p.Rules, p.BankTransfer} {
		for _, n := range numtext.Long(link.ReplaceAllString(field, " ")) {
			g.plain[n] = true
		}
	}
	for _, field := range []string{p.BusinessName, p.About, p.Tone, p.Contact, p.Rules} {
		for _, w := range strings.Fields(field) {
			w = strings.Trim(w, "()[]<>\"',.;")
			if !seen[w] && g.ownWord(w) {
				seen[w] = true
				g.words = append(g.words, w)
			}
		}
	}
	// Longest first, so a link is removed whole before any word inside it.
	sort.Slice(g.words, func(i, j int) bool { return len(g.words[i]) > len(g.words[j]) })
	return g
}

// check reports whether reply may be sent, and returns it with the bank text
// put in for the marker. It holds the reply when:
//   - it has a bank marker but the owner set no bank text;
//   - it is about paying and, outside the marker, has digits enough for an
//     account, phone or PromptPay ID, however split (profile phones too: a
//     staff phone must never become a payment target);
//   - otherwise, it has a long number (however written) not in the profile.
func (g guard) check(reply string) (string, bool) {
	hasMarker := bankMarker.MatchString(reply)
	if hasMarker && g.bank == "" {
		return reply, false
	}
	rest := bankMarker.ReplaceAllLiteralString(reply, " ")
	for _, w := range g.words {
		rest = strings.ReplaceAll(rest, w, " ")
	}
	if hasMarker || paymentWords.MatchString(reply) || paymentWords.MatchString(numtext.Normalize(reply)) {
		// Both checks: no long number of any kind (profile phones included),
		// and not too many loose digits however they are split.
		if len(numtext.Long(rest)) > 0 || numtext.PaymentDigits(rest) > maxPaymentDigits {
			return reply, false
		}
	} else {
		for _, n := range numtext.Long(rest) {
			if !g.numbers[n] {
				return reply, false
			}
		}
	}
	return bankMarker.ReplaceAllLiteralString(reply, g.bank), true
}

// ownWord reports whether a profile word may be taken out of a reply before
// counting digits: a link (whole, and holding none of the profile's phone or
// bank numbers), or a code with letters and at most a few digits
// ("JOHNAUTOCUT99"). A word that carries a phone or account number ("โทร081-
// 516-9097" written without a space) is never one.
func (g guard) ownWord(w string) bool {
	if len(w) < 6 || strings.IndexFunc(w, unicode.IsLetter) < 0 || strings.IndexFunc(w, unicode.IsDigit) < 0 {
		return false
	}
	if link.MatchString(w) {
		digits := numtext.Digits(numtext.Normalize(w))
		for n := range g.plain {
			if strings.Contains(digits, n) {
				return false
			}
		}
		return true
	}
	return len(numtext.Long(w)) == 0 && numtext.PaymentDigits(w) <= maxPaymentDigits
}
