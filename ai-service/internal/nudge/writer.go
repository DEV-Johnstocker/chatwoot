package nudge

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
)

// Writer drafts a follow-up from the chat itself. A draft that fails Check
// is not sent; the fixed Text goes out instead.
type Writer interface {
	Write(ctx context.Context, msgs []gaps.Message) (string, error)
}

// maxWritten caps a written follow-up (characters).
const maxWritten = 220

// transcriptMessages is how many recent public messages the writer sees.
const transcriptMessages = 12

const writeInstruction = "You write one short follow-up message from a shop's chat assistant to a customer " +
	"who stopped replying about a day ago, after the assistant told them a price. Read the chat and write the message.\n" +
	"- One or two short sentences, at most 200 characters.\n" +
	"- Name the product or need the customer asked about, in their words, and offer help: a question left open, " +
	"choosing the right package, or how to order.\n" +
	"- In Thai, address the customer as \"คุณลูกค้า\". You may start with a short greeting.\n" +
	"- No pressure: no urgency, deadlines, discounts or promotions. No prices, numbers or links. " +
	"Do not say you do not know, do not mention being a bot or AI, and do not mention how much time has passed.\n" +
	"- At most one emoji.\n" +
	"Reply with the message only."

// LLMWriter drafts follow-ups with a (small) chat model.
type LLMWriter struct {
	LLM     llm.Completer
	Timeout time.Duration // 0 = 20s
}

// Write returns the model's draft, trimmed. Only public messages are sent
// to the model: staff notes stay inside Chatwoot.
func (w *LLMWriter) Write(ctx context.Context, msgs []gaps.Message) (string, error) {
	timeout := w.Timeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := w.LLM.Complete(ctx, []llm.Message{
		{Role: "system", Content: writeInstruction + "\n" + voiceRule(msgs)},
		{Role: "user", Content: transcript(msgs)},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Content), nil
}

func transcript(msgs []gaps.Message) string {
	var lines []string
	for _, m := range msgs {
		if m.Private || strings.TrimSpace(m.Content) == "" {
			continue
		}
		switch {
		case m.Incoming:
			lines = append(lines, "Customer: "+m.Content)
		case m.Outgoing:
			lines = append(lines, "Assistant: "+m.Content)
		}
	}
	if len(lines) > transcriptMessages {
		lines = lines[len(lines)-transcriptMessages:]
	}
	return strings.Join(lines, "\n")
}

// voice is how the shop speaks in this chat.
type voice int

const (
	voiceEnglish voice = iota
	voiceMale          // Thai, ครับ
	voiceFemale        // Thai, ค่ะ / คะ
)

// voiceOf reads the language from the customer and the polite particle from
// the shop's earlier replies (ครับ when unclear: the default bot voice).
func voiceOf(msgs []gaps.Message) voice {
	if !thaiChat(msgs) {
		return voiceEnglish
	}
	male, female := 0, 0
	for _, m := range msgs {
		if m.Outgoing && !m.Private {
			male += strings.Count(m.Content, "ครับ")
			female += strings.Count(m.Content, "ค่ะ") + strings.Count(m.Content, "คะ")
		}
	}
	if female > male {
		return voiceFemale
	}
	return voiceMale
}

func voiceRule(msgs []gaps.Message) string {
	switch voiceOf(msgs) {
	case voiceFemale:
		return "- Write in Thai. The shop speaks as a woman: end with ค่ะ or คะ; never ครับ or ผม."
	case voiceMale:
		return "- Write in Thai. The shop speaks as a man: end with ครับ; never ค่ะ, คะ, หนู or ดิฉัน."
	default:
		return "- Write in English, like the customer, and address them as \"you\" (no Thai words)."
	}
}

// wrongVoice lists words that break the shop's voice.
var wrongVoice = map[voice][]string{
	voiceMale:   {"ค่ะ", "คะ", "หนู", "ดิฉัน"},
	voiceFemale: {"ครับ", "ผม"},
}

var (
	linkLike = regexp.MustCompile(`(?i)https?:|www\.|@|\b[a-z0-9-]+\.(com|net|org|co|th|io|me|ly|shop|example)\b`)
	// forbidden phrases: unknowns the owner dislikes, pressure and
	// promotions, and words that say a bot wrote it.
	forbidden = []string{
		"ไม่ทราบ", "ยังไม่มีข้อมูล", "ไม่แน่ใจ",
		"รีบ", "ด่วน", "หมดเขต", "วันสุดท้าย", "จำนวนจำกัด", "ส่วนลด", "โปรโมชั่น", "โปรพิเศษ", "จัดโปร", "ลดราคา",
		"บอท", "AI", "ระบบอัตโนมัติ",
	}
)

// Check refuses a written follow-up that could mislead or annoy: empty or
// long, any digit (no prices or numbers the shop did not approve), links or
// handles, pressure or promotion words, saying it does not know, naming a
// bot, more than two lines, and, in a Thai chat, not addressing the
// customer as "คุณลูกค้า" (a bare "เห็นว่าสนใจ" is impolite).
func Check(text string, msgs []gaps.Message) error {
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return errors.New("empty")
	case utf8.RuneCountInString(text) > maxWritten:
		return errors.New("too long")
	case strings.Count(text, "\n") > 1:
		return errors.New("too many lines")
	case strings.IndexFunc(text, unicode.IsDigit) >= 0:
		return errors.New("contains a number")
	case linkLike.MatchString(text):
		return errors.New("contains a link or handle")
	}
	for _, f := range forbidden {
		if strings.Contains(text, f) {
			return fmt.Errorf("contains a forbidden phrase")
		}
	}
	v := voiceOf(msgs)
	if v == voiceEnglish {
		if strings.ContainsFunc(text, isThai) {
			return errors.New("thai in an English chat")
		}
		return nil
	}
	if !strings.Contains(text, "คุณลูกค้า") || strings.Contains(text, "เห็นว่าสนใจ") {
		return errors.New("does not address the customer as คุณลูกค้า")
	}
	for _, w := range wrongVoice[v] {
		if strings.Contains(text, w) {
			return errors.New("not the shop's voice")
		}
	}
	return nil
}

func isThai(r rune) bool { return unicode.Is(unicode.Thai, r) }

// thaiChat reports whether the customer wrote Thai.
func thaiChat(msgs []gaps.Message) bool {
	for _, m := range msgs {
		if m.Incoming && strings.ContainsFunc(m.Content, isThai) {
			return true
		}
	}
	return false
}

// message is the follow-up for this chat: the Writer's draft when it passes
// Check, else the fixed Text. written reports which one.
func (s *Sweeper) message(ctx context.Context, msgs []gaps.Message) (text string, written bool) {
	if s.Writer == nil {
		return s.Text, false
	}
	draft, err := s.Writer.Write(ctx, msgs)
	if err != nil {
		s.Logger.Warn("nudge: write follow-up; sending the fixed text", "error", err)
		return s.Text, false
	}
	if err := Check(draft, msgs); err != nil {
		s.Logger.Info("nudge: written follow-up refused; sending the fixed text", "reason", err.Error())
		return s.Text, false
	}
	return draft, true
}
