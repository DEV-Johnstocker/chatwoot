package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/ack"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
)

// The bot stays quiet when a reply is not needed yet: a like, a sticker, an
// emoji, or one more "thank you" after the bot already answered one.
// Issue: DEV-Johnstocker/chatwoot#1

func turns(roleText ...string) []memory.Turn {
	var out []memory.Turn
	for i := 0; i+1 < len(roleText); i += 2 {
		out = append(out, memory.Turn{Role: roleText[i], Content: roleText[i+1]})
	}
	return out
}

func TestIsAck_ThanksLikesAndEmojiOnly(t *testing.T) {
	for _, s := range []string{"ขอบคุณครับ", "ขอบคุณเช่นกันครับ", "ขอบคุณมากค่ะ 🙏", "โอเคครับ", "ok", "Thanks!", "thank you", "👍", "🙏🙏", "❤️", "[The customer sent a sticker or like]", "ครับ", "จ้า"} {
		if !ack.IsAck(s) {
			t.Errorf("%q should be an acknowledgement", s)
		}
	}
	for _, s := range []string{"ขอบคุณครับ ราคาเท่าไหร่", "ได้ครับ ซื้อยังไง", "สนใจครับ", "ไม่เอาครับ", "หลายอย่างเลยครับ", "", "อันนี้ยังมีไหม"} {
		if ack.IsAck(s) {
			t.Errorf("%q is not only an acknowledgement", s)
		}
	}
}

func TestAsksSomething_ThaiAndEnglishQuestions(t *testing.T) {
	for _, s := range []string{"ตอนนี้ทำคลิปแนวไหนอยู่ครับ", "อยากดูราคาโปรไหมครับ 😊", "ใช้ Mac หรือ Windows ครับ?", "Which plan do you have?", "สมัครแล้วหรือยังครับ"} {
		if !asksSomething(s) {
			t.Errorf("%q asks something", s)
		}
	}
	for _, s := range []string{"ยินดีครับ 😊", "ดูตัวอย่างได้ที่ https://autocut.johnstockeraistudio.com/examples", "You're welcome!"} {
		if asksSomething(s) {
			t.Errorf("%q asks nothing", s)
		}
	}
}

func TestStaySilent_TheScreenshotChat(t *testing.T) {
	// Arrange: the owner's test chat of 2026-11-18.
	base := turns("user", "สนใจคอร์สครับ", "assistant", "สนใจคอร์ส John Auto Cut ใช่ไหมครับ ตอนนี้ทำคลิปแนวไหนอยู่ครับ",
		"user", "หลายอย่างเลยครับ", "assistant", "ได้เลยครับ ทำได้หลายแนว ดูตัวอย่างได้ที่ https://autocut.johnstockeraistudio.com/examples")
	firstThanks := append(append([]memory.Turn{}, base...), turns("user", "ขอบคุณครับ")...)
	afterThanks := append(append([]memory.Turn{}, firstThanks...), turns("assistant", "ยินดีครับ 😊")...)
	like := append(append([]memory.Turn{}, afterThanks...), turns("user", "👍")...)
	moreThanks := append(append([]memory.Turn{}, afterThanks...), turns("user", "ขอบคุณเช่นกันครับ")...)
	likeAfterQuestion := turns("user", "สนใจครับ", "assistant", "อยากดูราคาโปรตอนนี้ไหมครับ", "user", "👍")
	likeAfterStatement := append(append([]memory.Turn{}, base...), turns("user", "👍")...)
	question := append(append([]memory.Turn{}, afterThanks...), turns("user", "ราคาเท่าไหร่ครับ")...)

	// Act / Assert
	cases := []struct {
		name string
		h    []memory.Turn
		want bool
	}{
		{"first thanks gets a reply", firstThanks, false},
		{"like after the bot answered a thanks", like, true},
		{"second thanks", moreThanks, true},
		{"like after a question means yes", likeAfterQuestion, false},
		{"like after a statement", likeAfterStatement, true},
		{"a real question always gets a reply", question, false},
		{"empty history", nil, false},
	}
	for _, c := range cases {
		if got := staySilent(c.h); got != c.want {
			t.Errorf("%s: staySilent = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestProcess_LikeAfterThanksSendsNothingAndCallsNoModel(t *testing.T) {
	// Arrange: the bot already answered a thank-you.
	cw, l := &fakeChatwoot{}, &fakeLLM{reply: "ยินดีครับ 😊"}
	b, h := newBot(cw, l)
	for _, tt := range turns("user", "ขอดูตัวอย่างครับ", "assistant", "ดูตัวอย่างได้ที่ https://example.com/examples",
		"user", "ขอบคุณครับ", "assistant", "ยินดีครับ 😊") {
		h.Append(1, 7, tt)
	}

	// Act
	b.Process(context.Background(), customerMsg("👍"))

	// Assert
	if got := cw.joined(); strings.Contains(got, "ยินดี") || strings.Contains(got, "typing") {
		t.Errorf("sent or typed after a like: %q", got)
	}
	if l.got != nil {
		t.Error("the model was called for a like that needs no reply")
	}
	if hs := h.Get(1, 7); hs[len(hs)-1].Content != "👍" {
		t.Error("the like must still be kept in the chat history")
	}
}

func TestProcess_FirstThanksStillGetsAReply(t *testing.T) {
	// Arrange
	cw, l := &fakeChatwoot{}, &fakeLLM{reply: "ยินดีครับ อยากดูราคาโปรตอนนี้ไหมครับ"}
	b, h := newBot(cw, l)
	for _, tt := range turns("user", "ขอดูตัวอย่างครับ", "assistant", "ดูตัวอย่างได้ที่ https://example.com/examples") {
		h.Append(1, 7, tt)
	}

	// Act
	b.Process(context.Background(), customerMsg("ขอบคุณครับ"))

	// Assert
	if !strings.Contains(cw.joined(), "อยากดูราคาโปรตอนนี้ไหมครับ") {
		t.Errorf("first thanks got no reply: %q", cw.joined())
	}
}

// Edge cases for "no reply needed". Issue: DEV-Johnstocker/chatwoot#1
func TestStaySilent_EdgeCases(t *testing.T) {
	statement := "ดูตัวอย่างได้ที่ https://example.com/examples"
	question := "อยากดูราคาโปรตอนนี้ไหมครับ"
	cases := []struct {
		name string
		h    []memory.Turn
		want bool
	}{
		// Must reply.
		{"?? alone means the customer is lost", turns("assistant", statement, "user", "??"), false},
		{"a number alone answers something", turns("assistant", statement, "user", "2"), false},
		{"a price with a question mark", turns("assistant", statement, "user", "3900?"), false},
		{"thanks then a question in a burst", turns("assistant", statement, "user", "ขอบคุณครับ", "user", "แล้วผ่อนได้ไหม"), false},
		{"question then thanks in a burst", turns("assistant", statement, "user", "ผ่อนได้ไหม", "user", "ขอบคุณครับ"), false},
		{"a like as the very first message", turns("user", "👍"), false},
		{"thanks as the very first message", turns("user", "ขอบคุณครับ"), false},
		{"ok after the bot asked", turns("assistant", question, "user", "โอเคครับ"), false},
		{"ครับ after the bot asked", turns("assistant", question, "user", "ครับ"), false},
		{"sticker after the bot asked", turns("assistant", question, "user", stickerNote), false},
		{"no thanks is a no, not a thanks", turns("assistant", statement, "user", "ไม่เป็นไรครับ"), false},
		{"thanks with a follow-up in one message", turns("assistant", statement, "user", "ขอบคุณครับ เดี๋ยวโอนนะ"), false},
		{"english thanks plus question", turns("assistant", statement, "user", "Thanks! how much?"), false},
		{"first thanks after a statement", turns("assistant", statement, "user", "ขอบคุณครับ"), false},
		{"a photo note is not an ack", turns("assistant", statement, "user", "[The customer sent 1 photo(s), attached to this message]"), false},
		// May stay quiet.
		{"two likes in a row after a statement", turns("assistant", statement, "user", "👍", "user", "👍"), true},
		{"heart after a statement", turns("assistant", statement, "user", "❤️"), true},
		{"laughing after a statement", turns("assistant", statement, "user", "555"), true},
		{"ok thanks after the bot answered a thanks", turns("assistant", statement, "user", "ขอบคุณครับ", "assistant", "ยินดีครับ", "user", "ok thanks"), true},
	}
	for _, c := range cases {
		if got := staySilent(c.h); got != c.want {
			t.Errorf("%s: staySilent = %v, want %v", c.name, got, c.want)
		}
	}
}
