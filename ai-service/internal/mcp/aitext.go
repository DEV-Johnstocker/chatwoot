package mcp

import (
	"regexp"

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
)

// checkAIText refuses field text an AI app may not write. The error does not
// say which rule matched, so an injected app cannot probe for a gap.
func checkAIText(field, text string) error {
	s := numtext.Normalize(text)
	if aiLink.MatchString(s) || aiHandle.MatchString(s) || aiWords.MatchString(s) || aiLINE.MatchString(s) || len(numtext.Long(text)) > 0 {
		return userError(field + ": an AI tool cannot add links, @handles, LINE or messenger IDs, or long numbers " +
			"(phone, account or card numbers); the owner adds these on the admin page")
	}
	return nil
}
