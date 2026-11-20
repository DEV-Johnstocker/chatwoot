package bot

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// A reply that ends with a question goes out as two messages: first the
// answer or proof (with any link), then the one question on its own, the way
// a person types in a chat. The question no longer hides under a link. Never
// more than two: more would read as spam and ping the customer each time.
// The split is done here, not by the model, so it is predictable.
// Issue: DEV-Johnstocker/chatwoot#1

// partGap is the shortest pause before the second message, so the two arrive
// in order and read as typed one after the other.
const partGap = 1200 * time.Millisecond

var lastURL = regexp.MustCompile(`https?://\S+`)

// splitReply returns reply as one or two messages.
func splitReply(reply string) []string {
	text := strings.TrimSpace(reply)
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) >= 2 {
		last := lines[len(lines)-1]
		// "ดูตัวอย่างได้ที่ <link> อยากดูราคาไหมครับ": the link stays with the answer.
		if head, tail, ok := cutAfterLink(last); ok {
			return []string{strings.Join(append(lines[:len(lines)-1], head), "\n"), tail}
		}
		// A long last line that closes with its question: only the question moves.
		if len([]rune(last)) > questionLineMax {
			if head, tail, ok := cutLastQuestion(last, 1); ok {
				return []string{strings.Join(append(lines[:len(lines)-1], head), "\n"), tail}
			}
		}
		if isQuestionLine(last) && hasWords(last) {
			return []string{strings.Join(lines[:len(lines)-1], "\n"), last}
		}
		return []string{text}
	}
	// One line: a link followed by the question ("…/examples อยากดูราคาไหมครับ").
	if head, tail, ok := cutAfterLink(text); ok {
		return []string{head, tail}
	}
	// One line: answer sentences, then the question ("…ดูซ้ำได้ครับ ทำคลิปแนวไหนอยู่ครับ?").
	if head, tail, ok := cutLastQuestion(text, oneLineHeadMin); ok {
		return []string{head, tail}
	}
	return []string{text}
}

// oneLineHeadMin is the shortest answer worth its own message when answer and
// question share one line: "ได้ครับ ใช้คอมรุ่นไหนครับ" stays one message.
const oneLineHeadMin = 40

// sentenceEnd is where a sentence ends inside a line: a Thai polite ending or
// a full stop, then a space.
var sentenceEnd = regexp.MustCompile(`(ครับ|ค่ะ|คะ|นะ|[.!。])\s+`)

// cutLastQuestion splits a line that ends with a question at the start of
// that question's sentence. The answer before it must have at least headMin
// characters and the question at most questionLineMax.
func cutLastQuestion(line string, headMin int) (head, tail string, ok bool) {
	if !asksSomething(line) {
		return "", "", false
	}
	ends := sentenceEnd.FindAllStringIndex(line, -1)
	for i := len(ends) - 1; i >= 0; i-- {
		head, tail = strings.TrimSpace(line[:ends[i][1]]), strings.TrimSpace(line[ends[i][1]:])
		if len([]rune(tail)) > questionLineMax {
			return "", "", false
		}
		if asksSomething(tail) && hasWords(tail) && hasWords(head) && len([]rune(head)) >= headMin && !asksSomething(head) {
			return head, tail, true
		}
	}
	return "", "", false
}

// cutAfterLink splits a line at its last link when a question follows it.
func cutAfterLink(line string) (head, tail string, ok bool) {
	locs := lastURL.FindAllStringIndex(line, -1)
	if len(locs) == 0 {
		return "", "", false
	}
	end := locs[len(locs)-1][1]
	head, tail = strings.TrimSpace(line[:end]), strings.TrimSpace(line[end:])
	if tail == "" || !isQuestionLine(tail) || !hasWords(tail) {
		return "", "", false
	}
	return head, tail, true
}

// hasWords reports whether s holds letters, not only a link or emoji.
func hasWords(s string) bool {
	return strings.ContainsFunc(lastURL.ReplaceAllString(s, ""), unicode.IsLetter)
}

// sendParts sends the reply as one or two messages. The first waits for its
// typing time like any reply; before the second, "typing…" shows again for
// that message's own typing time (typing switch on) or partGap (off).
func (b *Bot) sendParts(ctx context.Context, log *slog.Logger, acct, conv int64, typing bool, started time.Time, text string) error {
	return b.sendList(ctx, log, acct, conv, typing, started, splitReply(text), true)
}

// sendList sends already split messages in order. first says whether the
// first one opens the reply (it waits out the reply's typing time) or follows
// something already sent (typing shows again, then a pause).
func (b *Bot) sendList(ctx context.Context, log *slog.Logger, acct, conv int64, typing bool, started time.Time, parts []string, first bool) error {
	for i, part := range parts {
		if i == 0 && first {
			b.typingPause(ctx, typing, started, part)
		} else {
			if err := b.opts.Chatwoot.SetTyping(ctx, acct, conv, true); err != nil {
				log.Warn("typing on failed", "error", err)
			}
			pause := partGap
			if typing {
				pause = max(pause, typingTime(part, b.opts.TypingSpeed, b.opts.TypingMax))
			}
			b.wait(ctx, pause)
		}
		if err := b.sendReply(ctx, log, acct, conv, part); err != nil {
			return err
		}
	}
	return nil
}

// questionLineMax is the longest last line still read as "the question": a
// short line with a question word anywhere ("ทำคลิปแนวไหนอยู่ครับ จะได้แนะนำ
// ให้ตรงกับงานเลย"). Longer lines must end with the question.
const questionLineMax = 80

// isQuestionLine reports whether a reply's last line is its question.
func isQuestionLine(line string) bool {
	if asksSomething(line) {
		return true
	}
	return len([]rune(line)) <= questionLineMax && (strings.ContainsAny(line, "?？") || thaiQuestion.MatchString(line))
}
