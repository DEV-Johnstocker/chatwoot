package nudge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/gaps"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
)

type fakeLLM struct {
	reply string
	err   error
	got   []llm.Message
}

func (f *fakeLLM) Complete(_ context.Context, msgs []llm.Message) (llm.Result, error) {
	f.got = msgs
	return llm.Result{Content: f.reply}, f.err
}

func sampleThaiChat() []gaps.Message {
	return []gaps.Message{
		{Incoming: true, Content: "Workshop ตัดต่อราคาเท่าไหร่ครับ"},
		{Outgoing: true, Private: true, Content: "staff-only note"},
		{Outgoing: true, FromBot: true, Content: "Workshop ราคา 4,900 บาทครับ"},
	}
}

func TestCheck_Accepts(t *testing.T) {
	// Arrange
	text := "สวัสดีครับคุณลูกค้า ถ้ามีคำถามเรื่อง Workshop ตัดต่อเพิ่มเติม ทักมาได้เลยนะครับ 🙏"
	// Act
	err := Check(text, sampleThaiChat())
	// Assert
	if err != nil {
		t.Fatalf("Check = %v, want ok", err)
	}
}

func TestCheck_Rejects(t *testing.T) {
	cases := map[string]string{
		"empty":            "  ",
		"too long":         "สวัสดีครับคุณลูกค้า " + strings.Repeat("ก", maxWritten),
		"a number":         "สวัสดีครับคุณลูกค้า ราคา 4,900 บาทครับ",
		"a Thai digit":     "สวัสดีครับคุณลูกค้า เหลือ ๓ ที่ครับ",
		"a link":           "สวัสดีครับคุณลูกค้า ดูที่ autocut.example/shop ได้ครับ",
		"an http link":     "สวัสดีครับคุณลูกค้า https://x.y ครับ",
		"a handle":         "สวัสดีครับคุณลูกค้า ทักไลน์ @shop ได้ครับ",
		"no คุณลูกค้า":     "สวัสดีครับ ถ้ามีคำถามทักมาได้เลยครับ",
		"says not know":    "สวัสดีครับคุณลูกค้า ไม่ทราบว่าสนใจไหมครับ",
		"pressure":         "สวัสดีครับคุณลูกค้า รีบตัดสินใจนะครับ",
		"promo":            "สวัสดีครับคุณลูกค้า ตอนนี้มีโปรพิเศษครับ",
		"bare เห็นว่าสนใจ": "สวัสดีครับ เห็นว่าสนใจ Workshop คุณลูกค้าทักได้ครับ",
		"bot word":         "สวัสดีครับคุณลูกค้า บอทยินดีช่วยครับ",
		"several lines":    "สวัสดีครับคุณลูกค้า\nบรรทัดสอง\nบรรทัดสาม\nบรรทัดสี่",
		"wrong particle":   "คุณลูกค้าสนใจ Workshop ใช่ไหมคะ บอกได้เลยค่ะ",
		"wrong pronoun":    "คุณลูกค้าครับ หนูช่วยแนะนำได้ครับ",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			// Act
			err := Check(text, sampleThaiChat())
			// Assert
			if err == nil {
				t.Fatalf("Check(%q) = nil, want refused", text)
			}
		})
	}
}

func TestCheck_EnglishChatNeedsNoThaiAddress(t *testing.T) {
	// Arrange
	chat := []gaps.Message{{Incoming: true, Content: "how much is the course?"}}
	// Act
	err := Check("Hi! If you have any other questions about the course, just let me know.", chat)
	// Assert
	if err != nil {
		t.Fatalf("Check = %v, want ok for an English chat", err)
	}
}

func TestCheck_FemaleVoice(t *testing.T) {
	// Arrange: the shop answers with ค่ะ.
	chat := []gaps.Message{
		{Incoming: true, Content: "ครีมกันแดดขวดละเท่าไหร่คะ"},
		{Outgoing: true, FromBot: true, Content: "ขวดละ 590 บาทค่ะ"},
	}
	// Act
	ok := Check("คุณลูกค้าสนใจครีมกันแดดไหมคะ ทักได้เลยค่ะ", chat)
	wrong := Check("คุณลูกค้าสนใจครีมกันแดดไหมครับ ผมช่วยได้ครับ", chat)
	// Assert
	if ok != nil || wrong == nil {
		t.Fatalf("ok=%v wrong=%v, want ค่ะ accepted and ครับ refused", ok, wrong)
	}
}

func TestCheck_EnglishChatRefusesBotPressureAndLinks(t *testing.T) {
	chat := []gaps.Message{{Incoming: true, Content: "how much is the course?"}, {Outgoing: true, FromBot: true, Content: "It is 9,900 baht."}}
	for _, text := range []string{
		"Hi! Our ai can help with the course.",
		"Hi! This is the shop bot, ask me anything about the course.",
		"Hurry, the course offer is limited!",
		"Hi! There is a discount on the course this week.",
		"Hi! See shop.xyz for the course.",
		"Hi! I don't know if you saw the course details.",
	} {
		t.Run(text, func(t *testing.T) {
			// Act
			err := Check(text, chat)
			// Assert
			if err == nil {
				t.Fatalf("Check(%q) = nil, want refused", text)
			}
		})
	}
}

func TestCheck_EnglishChatRefusesThai(t *testing.T) {
	// Arrange
	chat := []gaps.Message{{Incoming: true, Content: "how much is the course?"}, {Outgoing: true, FromBot: true, Content: "It is 9,900 baht."}}
	// Act
	err := Check("คุณลูกค้าสนใจคอร์สไหมครับ", chat)
	// Assert
	if err == nil {
		t.Fatal("Check = nil, want a Thai follow-up refused in an English chat")
	}
}

func TestLLMWriter_TellsVoiceAndLanguage(t *testing.T) {
	cases := map[string]struct {
		chat []gaps.Message
		want string
	}{
		"male Thai":   {sampleThaiChat(), "ครับ"},
		"female Thai": {[]gaps.Message{{Incoming: true, Content: "ราคาเท่าไหร่"}, {Outgoing: true, FromBot: true, Content: "590 บาทค่ะ"}}, "ค่ะ"},
		"English":     {[]gaps.Message{{Incoming: true, Content: "price?"}, {Outgoing: true, FromBot: true, Content: "590 baht"}}, "English"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			f := &fakeLLM{reply: "x"}
			// Act
			_, _ = (&LLMWriter{LLM: f}).Write(context.Background(), tc.chat)
			// Assert
			if !strings.Contains(f.got[0].Content, tc.want) {
				t.Fatalf("system prompt does not name %q:\n%s", tc.want, f.got[0].Content)
			}
		})
	}
}

func TestLLMWriter_SendsPublicTranscriptOnly(t *testing.T) {
	// Arrange
	f := &fakeLLM{reply: "  สวัสดีครับคุณลูกค้า ทักมาได้เลยนะครับ \n"}
	w := &LLMWriter{LLM: f}
	// Act
	got, err := w.Write(context.Background(), sampleThaiChat())
	// Assert
	if err != nil || got != "สวัสดีครับคุณลูกค้า ทักมาได้เลยนะครับ" {
		t.Fatalf("got %q err %v", got, err)
	}
	if len(f.got) != 2 || f.got[0].Role != "system" || strings.Contains(f.got[1].Content, "staff-only") {
		t.Fatalf("prompt = %+v, want system + transcript without private notes", f.got)
	}
	if !strings.Contains(f.got[1].Content, "Customer: Workshop") || !strings.Contains(f.got[1].Content, "Assistant: Workshop ราคา") {
		t.Fatalf("transcript = %q", f.got[1].Content)
	}
}

func TestWriteOnlyWhenTheRuleAsks(t *testing.T) {
	// Arrange
	s := newSweeper(nil, nil)
	f := &fakeLLM{reply: "สวัสดีครับคุณลูกค้า ทักมาได้เลยครับ"}
	s.Writer = &LLMWriter{LLM: f}
	// Act
	got, written := s.message(context.Background(), Rule{On: true, Text: "fixed"}, sampleThaiChat())
	// Assert
	if got != "fixed" || written || f.got != nil {
		t.Fatalf("got %q written=%v calls=%v, want the fixed text and no model call", got, written, f.got)
	}
}

func TestWriteOrFallback(t *testing.T) {
	cases := map[string]struct {
		w    Writer
		want string
	}{
		"no writer":     {nil, "fixed"},
		"model error":   {&LLMWriter{LLM: &fakeLLM{err: errors.New("down")}}, "fixed"},
		"fails a check": {&LLMWriter{LLM: &fakeLLM{reply: "ราคา 4,900 บาทครับคุณลูกค้า"}}, "fixed"},
		"good":          {&LLMWriter{LLM: &fakeLLM{reply: "สวัสดีครับคุณลูกค้า ทักมาได้เลยครับ"}}, "สวัสดีครับคุณลูกค้า ทักมาได้เลยครับ"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			s := newSweeper(nil, nil)
			s.Writer = tc.w
			// Act
			got, _ := s.message(context.Background(), Rule{On: true, Text: "fixed", Write: true}, sampleThaiChat())
			// Assert
			if got != tc.want {
				t.Fatalf("message = %q, want %q", got, tc.want)
			}
		})
	}
}
