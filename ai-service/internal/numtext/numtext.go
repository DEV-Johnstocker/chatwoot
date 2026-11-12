// Package numtext finds long numbers (phone, account and card numbers) in text
// written to customers, however they are disguised: Thai, full-width or other
// digit forms, zero-width characters, dots, dashes or line breaks between the
// digits. Times, dates and prices are not long numbers. Used by the MCP field
// check and the bot's reply guard, so both read numbers the same way.
// Issue: DEV-Johnstocker/chatwoot#1
package numtext

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// MaxShortDigits is the most digits a number may have before it could be a
// phone, account or card number (Thai phones have 9-10, accounts 10+).
const MaxShortDigits = 6

var (
	// numberLines is one number, with up to six non-letter characters
	// (spaces, dashes, commas, colons, line breaks...) between digit groups.
	numberLines = regexp.MustCompile(`\d(?:[^\p{L}\p{N}]{0,6}\d)*`)
	bigPrice    = regexp.MustCompile(`(?:^|[^\d,])[1-9]\d{0,2}(?:,\d{3}){2}`) // any price in a range or list
	currency    = regexp.MustCompile(`(?i)฿|บาท|\bbaht\b|\bthb\b`)
	spaces      = regexp.MustCompile(`[^\S\n]+`)
	punctRun    = regexp.MustCompile(`[^\p{L}\p{N}\s]{3,}`) // "081.......234" stays one number
	breaks      = regexp.MustCompile(`\s*\n\s*`)
	// safeNumber is a time, a date, a date with a time or a price, or a
	// range or list of one kind: long, but not an account or phone number.
	safeNumber = func() *regexp.Regexp {
		year := `(?:19|20|25)\d{2}`
		month := `(?:0?[1-9]|1[0-2])`
		day := `(?:0?[1-9]|[12]\d|3[01])`
		kinds := []string{
			`(?:[01]?\d|2[0-4])[:.][0-5]\d`, // time
			`(?:` + year + `-` + month + `-` + day + `|` + day + `/` + month + `/(?:` + year + `|\d{2}))`, // date
			`[1-9]\d{0,2}(?:,\d{3}){1,2}(?:\.\d{1,2})?`,                                                   // price up to 999,999,999
		}
		timeOrRange := kinds[0] + `(?:\s*[-–]\s*` + kinds[0] + `)?`
		kinds = append(kinds,
			kinds[1]+`\s+`+timeOrRange, // date and time, or a date and its hours
			year+`\s+`+timeOrRange)     // "12 ธ.ค. 2569 09:00-17:00": the year, then the hours
		// One kind repeated: a range or a list ("09:00-12:00, 13:00-18:00").
		var alts []string
		for _, k := range kinds {
			alts = append(alts, k+`(?:\s*[-–,/;&]\s*`+k+`){0,3}`) // at most four items: two ranges
		}
		return regexp.MustCompile(`^(?:` + strings.Join(alts, `|`) + `)$`)
	}()
)

// Long returns the long numbers in text, as their digits only: each number
// with more than MaxShortDigits digits that is not a time, date or price on
// one line. A number that runs over a line break is never treated as safe, so
// "08:12" on one line and "345678" on the next is one ten-digit number. A
// price over six digits needs a currency on its line.
func Long(text string) []string {
	s := punctRun.ReplaceAllString(Normalize(text), "--")
	var out []string
	for _, loc := range numberLines.FindAllStringIndex(s, -1) {
		m := s[loc[0]:loc[1]]
		if digitCount(m) <= MaxShortDigits {
			continue
		}
		if !strings.Contains(m, "\n") && safeNumber.MatchString(m) &&
			(!bigPrice.MatchString(m) || currency.MatchString(lineAt(s, loc[0]))) {
			continue
		}
		out = append(out, Digits(m))
	}
	return out
}

// Digits keeps only the ASCII digits of s.
func Digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// lineAt is the line of s that holds byte i.
func lineAt(s string, i int) string {
	start := strings.LastIndexByte(s[:i], '\n') + 1
	end := strings.IndexByte(s[i:], '\n')
	if end < 0 {
		return s[start:]
	}
	return s[start : i+end]
}

func digitCount(s string) int { return len(Digits(s)) }

// Normalize writes every digit form (Thai, full-width, circled...) as ASCII,
// drops invisible characters, and reads O, o, I and l next to a digit as 0 or
// 1 ("O81-234-5678").
func Normalize(text string) string {
	// NFKD turns full-width and other look-alike forms (＠ ﹫ ｗ ． ① ¹) into
	// plain ones and splits accents off their letters (Ń → N + mark) so
	// visible can drop them; a few dots it leaves are mapped by hand. It also
	// splits Thai sara am (ำ → ํ + า): no pattern here may contain ำ.
	text = strings.NewReplacer("。", ".", "｡", ".", "︒", ".").Replace(norm.NFKD.String(text))
	text = breaks.ReplaceAllString(spaces.ReplaceAllString(visible(text), " "), "\n")
	rs := []rune(text)
	out := make([]rune, len(rs))
	for i, r := range rs {
		out[i] = asciiDigit(r)
	}
	isDigit := func(i int) bool { return i >= 0 && i < len(out) && out[i] >= '0' && out[i] <= '9' }
	for i, r := range out {
		if !isDigit(i-1) && !isDigit(i+1) {
			continue
		}
		switch r {
		case 'O', 'o':
			out[i] = '0'
		case 'I', 'l':
			out[i] = '1'
		}
	}
	return string(out)
}

// visible drops invisible characters (zero-width spaces and joiners, soft
// hyphens, direction marks) and every combining mark except a Thai mark on
// a Thai letter, so "L\u200dINE" and "LIN\u0301E" read as LINE. Thai vowel
// and tone marks stay.
func visible(text string) string {
	var b strings.Builder
	prev := rune(0)
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Cf, unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point):
			continue
		case unicode.In(r, unicode.Mn, unicode.Me) && !(unicode.Is(unicode.Thai, r) && unicode.Is(unicode.Thai, prev)):
			continue
		}
		b.WriteRune(r)
		prev = r
	}
	return b.String()
}

func asciiDigit(r rune) rune {
	switch {
	case r >= '0' && r <= '9':
		return r
	case r >= '๐' && r <= '๙':
		return '0' + (r - '๐')
	case r >= '０' && r <= '９':
		return '0' + (r - '０')
	case unicode.IsNumber(r):
		return '9' // another digit form: still counted as a digit
	}
	return r
}
