package bot

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/ack"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
)

// The bot does not answer what needs no answer: a like, a sticker or an
// emoji after a reply that asked nothing, or one more "thank you" after the
// bot already answered one. A person on the team would let those go; a
// chain of "ยินดีครับ 😊" reads like a machine (owner's test, 2026-11-18).
// Issue: DEV-Johnstocker/chatwoot#1

// thaiQuestion are question words; a Thai reply ends with one, sometimes
// followed by a word or two (แนวไหนอยู่ครับ, ได้บ้างคะ), never a question mark.
var thaiQuestion = regexp.MustCompile(`(ไหม|มั้ย|ไหน|อะไร|ยังไง|อย่างไร|กี่|เท่าไหร่|เท่าไร|หรือเปล่า|รึเปล่า|หรือยัง|ไหร่)`)

// questionTail is how many characters from the end a Thai question word may
// sit, after the polite ending is taken off.
const questionTail = 12

// asksSomething reports whether a bot reply ends by asking the customer.
func asksSomething(reply string) bool {
	t := strings.TrimRightFunc(strings.TrimSpace(reply), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.Is(unicode.Thai, r) && r != '?' && r != '？'
	})
	if strings.HasSuffix(t, "?") || strings.HasSuffix(t, "？") {
		return true
	}
	t = ack.TrimPoliteEnding(t)
	r := []rune(t)
	if len(r) > questionTail {
		r = r[len(r)-questionTail:]
	}
	return thaiQuestion.MatchString(string(r))
}

// staySilent reports whether the customer's latest messages need no reply.
// history ends with the customer's turns since the bot last spoke.
func staySilent(history []memory.Turn) bool {
	last := len(history) - 1
	if last < 0 || history[last].Role != "user" {
		return false
	}
	// The customer's turns since the bot last spoke must all be acknowledgements.
	i := last
	for ; i >= 0 && history[i].Role == "user"; i-- {
		if !ack.IsAck(history[i].Content) {
			return false
		}
	}
	if i < 0 {
		return false // nothing said by the bot yet: greet them
	}
	botSaid := history[i].Content
	if asksSomething(botSaid) {
		return false // a like or "ok" answers the bot's question
	}
	// The bot already answered an acknowledgement: let this one go.
	if j := i - 1; j >= 0 && history[j].Role == "user" && ack.IsAck(history[j].Content) {
		return true
	}
	// A like, sticker or emoji after a reply that asked nothing.
	for k := i + 1; k <= last; k++ {
		if !ack.EmojiOnly(history[k].Content) {
			return false
		}
	}
	return true
}

// stickerNote is what readMessage writes for a sticker or a like.
const stickerNote = ack.StickerNote
