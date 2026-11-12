package bot

import (
	"regexp"
	"strings"
)

// numberRun is a run of nine or more digits (ASCII or Thai), allowing one
// space or dash between digits: bank account, phone and ID numbers. Prices
// ("9,900"), dates ("2026-11-30") and counts are shorter or broken by other
// characters, so they never match.
var numberRun = regexp.MustCompile(`[0-9๐-๙](?:[ \-]?[0-9๐-๙]){8,}`)

// unverifiedNumbers lists the long numbers in reply that appear in none of the
// trusted texts (the business profile and the knowledge rows the bot was
// given). A bank account the model mistyped, or one a customer or a web page
// slipped into the chat, is never in them, so the reply is held. Matching is
// on the digits alone, so "995-2-31094-5" equals "9952310945".
func unverifiedNumbers(reply string, trusted ...string) []string {
	known := map[string]bool{}
	for _, t := range trusted {
		for _, m := range numberRun.FindAllString(t, -1) {
			known[digitsOnly(m)] = true
		}
	}
	var bad []string
	for _, m := range numberRun.FindAllString(reply, -1) {
		if !known[digitsOnly(m)] {
			bad = append(bad, m)
		}
	}
	return bad
}

// digitsOnly keeps the digits of s, with Thai digits turned into ASCII.
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= '๐' && r <= '๙':
			b.WriteRune('0' + (r - '๐'))
		}
	}
	return b.String()
}
