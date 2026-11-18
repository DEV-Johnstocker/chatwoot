// Package ack recognises customer messages that only acknowledge: thanks,
// OK, a like, a sticker, laughter or emoji alone. The bot stays quiet on
// them when no reply is needed, and the follow-up nudge looks past them.
// Issue: DEV-Johnstocker/chatwoot#1
package ack

import (
	"regexp"
	"strings"
	"unicode"
)

// StickerNote is what the bot writes for a sticker or a like.
const StickerNote = "[The customer sent a sticker or like]"

// ackWords are whole messages that only acknowledge, once polite particles,
// emoji and punctuation are taken off. Lower case.
var ackWords = map[string]bool{
	"ขอบคุณ": true, "ขอบคุณมาก": true, "ขอบคุณมากๆ": true, "ขอบคุณเช่นกัน": true, "ขอบใจ": true,
	"โอเค": true, "ok": true, "okay": true, "โอเคเลย": true, "รับทราบ": true, "จ้า": true, "จ้ะ": true,
	"thanks": true, "thank you": true, "thx": true, "ty": true, "": true,
}

// ackParticles are polite endings taken off before matching.
var ackParticles = regexp.MustCompile(`(ครับผม|ครับ|คับ|ค่ะ|คะ|ค่า|นะ|จ้า|จ้ะ|เลย|มากๆ)+$`)

// IsAck reports whether text only acknowledges: thanks, OK, a like, a
// sticker, or emoji alone.
func IsAck(text string) bool {
	t := strings.TrimSpace(text)
	if t == StickerNote {
		return true
	}
	if t == "" {
		return false
	}
	// A question mark or a number says something ("??", "2", "3900?");
	// only Thai laughter (555) is a number that acknowledges.
	if strings.ContainsAny(t, "?？") || (strings.ContainsFunc(t, unicode.IsDigit) && !laughter.MatchString(t)) {
		return false
	}
	letters := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.Is(unicode.Thai, r) || r == ' ' {
			return unicode.ToLower(r)
		}
		return -1
	}, t)
	letters = strings.TrimSpace(letters)
	if letters == "" {
		return true // emoji or punctuation only
	}
	if letters == "ครับ" || letters == "ค่ะ" || letters == "คับ" || letters == "จ้า" {
		return true
	}
	if ackWords[strings.TrimSpace(ackParticles.ReplaceAllString(letters, ""))] {
		return true
	}
	// "ok thanks", "โอเค ขอบคุณครับ": every word acknowledges.
	words := strings.Fields(letters)
	for _, w := range words {
		if !ackWords[ackParticles.ReplaceAllString(w, "")] && !ackWords[w] {
			return false
		}
	}
	return len(words) > 1
}

// laughter is Thai "hahaha" written as fives (555, 5555+).
var laughter = regexp.MustCompile(`^5{3,}[+]*$`)

// EmojiOnly reports a like, a sticker or emoji with no words.
func EmojiOnly(text string) bool {
	t := strings.TrimSpace(text)
	return t == StickerNote || (t != "" && !strings.ContainsFunc(t, unicode.IsLetter))
}

// TrimPoliteEnding takes the polite particles (ครับ, ค่ะ, นะ…) off the end.
func TrimPoliteEnding(s string) string {
	return ackParticles.ReplaceAllString(s, "")
}
