package mcp

import (
	"regexp"
	"strings"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/numtext"
)

// Text an AI app writes over MCP into what customers are told (the business
// profile and knowledge documents) must not carry payment or contact details:
// a prompt-injected app could otherwise send customers to someone else's bank
// account or LINE. intake.Detect is tuned for customer messages and avoids
// false alarms; this check is deliberately stricter, because the owner can
// always make the same edit on /admin. It is a filter, not a proof: wording
// alone ("pay via our Facebook page") still gets through, which is why owner
// approval of AI edits is the follow-up.
var (
	aiLink = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://|\bwww\.|\b[a-z0-9-]+(?:\.[a-z0-9-]+)*\.[a-z]{2,}\b`)
	// aiWords are contact channels that work without a link or @: a LINE ID
	// or a messenger name. Payment words alone (PromptPay, bank, transfer)
	// point nowhere and are common in real shop text, so they are allowed;
	// the number they would need is caught by numtext.Long.
	aiWords  = regexp.MustCompile(`(?i)\bline\s*(?:id|oa|official)\b|\bline\s*[:@]|ไลน|ไอดี|ไอจี|\b(?:telegram|whatsapp|wechat|instagram|ig)\b`)
	aiLINE   = regexp.MustCompile(`\b(?:LINE|Line)\b`) // case-sensitive: "in line with" is fine
	aiHandle = regexp.MustCompile(`@\s*[\p{L}\p{N}_]`)
	// aiAddLine is "add (me/us) (on) line" in any case: an invitation to a
	// LINE chat even when written in lower case.
	aiAddLine = regexp.MustCompile(`(?i)\badd\s+(?:(?:me|us)\s+)?(?:on\s+)?line\b`)
	// aiIDN is a domain with a Thai (or other non-ASCII) name, or the Thai
	// top-level domain: ร้านค้า.ไทย, ตัวอย่าง.com, shop.ไทย. aiLink only sees
	// ASCII names.
	aiIDN = regexp.MustCompile(`(?i)[\p{L}\p{M}\p{N}-]+\.(?:[a-z]{2,}\b|ไทย)`)
	// aiDotWord is a dot spelled out so no link pattern sees it:
	// "shop dot com", "shop (dot) com", "shop[.]com", "shop ดอท คอม".
	aiDotWord = regexp.MustCompile(`(?i)\bdot\s*[\])}]?\s*(?:com|net|org|co|me|io|th|shop|app|xyz|info|biz|link)\b|` +
		`[\[({]\s*(?:\.|dot)\s*[\])}]|(?:ดอท|จุด)\s*(?:คอม|com|เน็ต|net|org|co\b|โค|ไทย|th\b)`)
	// aiDigitLetter is one letter between digits ("081x234x5678"): read as a
	// separator so the number is seen whole.
	aiDigitLetter = regexp.MustCompile(`(\d)[^\S\n]*[a-zA-Z][^\S\n]*(\d)`)
)

// confusables maps Cyrillic and Greek letters that look like Latin ones to
// the Latin letter, so "scam-shop.сom" (Cyrillic с, о) reads as a domain.
var confusables = strings.NewReplacer(
	"а", "a", "в", "b", "е", "e", "к", "k", "м", "m", "н", "h", "о", "o", "р", "p", "с", "c", "т", "t",
	"у", "y", "х", "x", "ѕ", "s", "і", "i", "ј", "j", "ԁ", "d", "ӏ", "l", "ԛ", "q", "ԝ", "w", "һ", "h",
	"А", "A", "В", "B", "Е", "E", "К", "K", "М", "M", "Н", "H", "О", "O", "Р", "P", "С", "C", "Т", "T",
	"У", "Y", "Х", "X", "Ѕ", "S", "І", "I", "Ј", "J",
	"α", "a", "β", "b", "ε", "e", "ι", "i", "κ", "k", "ν", "v", "ο", "o", "ρ", "p", "τ", "t", "υ", "u", "χ", "x",
	"Α", "A", "Β", "B", "Ε", "E", "Ζ", "Z", "Η", "H", "Ι", "I", "Κ", "K", "Μ", "M", "Ν", "N", "Ο", "O",
	"Ρ", "P", "Τ", "T", "Υ", "Y", "Χ", "X",
)

// aiNormalize is the text the AI checks read: numtext.Normalize (digit
// forms, invisible characters, NFKD), then look-alike letters as Latin and
// a single letter between digits as a dash.
func aiNormalize(text string) string {
	s := confusables.Replace(numtext.Normalize(text))
	for { // matches overlap ("1x2x3"), so repeat until nothing changes
		next := aiDigitLetter.ReplaceAllString(s, "$1-$2")
		if next == s {
			return s
		}
		s = next
	}
}

// checkAIText refuses field text an AI app may not write. The error does not
// say which rule matched, so an injected app cannot probe for a gap.
func checkAIText(field, text string) error {
	s := aiNormalize(text)
	if aiLink.MatchString(s) || aiHandle.MatchString(s) || aiWords.MatchString(s) || aiLINE.MatchString(s) ||
		aiAddLine.MatchString(s) || aiIDN.MatchString(s) || aiDotWord.MatchString(s) ||
		len(numtext.Long(text)) > 0 || len(numtext.Long(s)) > 0 {
		return userError(field + ": an AI tool cannot add links, @handles, LINE or messenger IDs, or long numbers " +
			"(phone, account or card numbers); the owner adds these on the admin page")
	}
	return nil
}
