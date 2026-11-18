package bot

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
)

// Prompt versions label every traced reply, so Langfuse can filter and
// compare replies by the prompt that wrote them. The prompt itself stays in
// code (reviewed, tested, promoted); only a short fingerprint is sent.
// Issue: DEV-Johnstocker/chatwoot#1

// shortHash is the first 8 hex characters of the SHA-256 of the parts.
func shortHash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0}) // keeps ("ab","c") apart from ("a","bc")
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}

// rulesVersion fingerprints the rules every reply follows: the configured
// prompt and the built-in policy. The policy is rendered at a fixed time so
// the date and clock in it do not change the version.
func rulesVersion(systemPrompt string) string {
	return shortHash(systemPrompt, policyPrompt(time.Time{}.In(time.UTC)))
}

// profileVersion fingerprints the parts of a business profile that reach the
// model. Pictures and the save time are left out.
func profileVersion(p profile.Profile) string {
	return shortHash(p.BusinessName, p.About, p.Tone, p.Contact, p.Rules, p.BankTransfer)
}

// promptVersion is "r<rules>-p<profile>".
func promptVersion(rules string, p profile.Profile) string {
	return "r" + rules + "-p" + profileVersion(p)
}
