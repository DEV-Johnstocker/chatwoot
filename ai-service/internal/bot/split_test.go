package bot

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
)

// A reply that ends with a question goes out as two messages: the answer
// (with any link), then the question on its own. Issue: DEV-Johnstocker/chatwoot#1

func TestSplitReply(t *testing.T) {
	cases := []struct {
		name, reply string
		want        []string
	}{
		{"answer lines then a question line",
			"รองรับหลายแนวเลยครับ ทั้งคลิปขายของ ไลฟ์ และพอดแคสต์\nดูตัวอย่างได้ที่ https://autocut.example.com/examples\nอยากดูราคาโปรตอนนี้ไหมครับ",
			[]string{"รองรับหลายแนวเลยครับ ทั้งคลิปขายของ ไลฟ์ และพอดแคสต์\nดูตัวอย่างได้ที่ https://autocut.example.com/examples", "อยากดูราคาโปรตอนนี้ไหมครับ"}},
		{"link and question on one line",
			"ดูตัวอย่างได้ที่ https://autocut.example.com/examples อยากดูราคาโปรไหมครับ",
			[]string{"ดูตัวอย่างได้ที่ https://autocut.example.com/examples", "อยากดูราคาโปรไหมครับ"}},
		{"one short line stays one message", "ราคา 3,900 บาทครับ สนใจไหมครับ", []string{"ราคา 3,900 บาทครับ สนใจไหมครับ"}},
		{"no question at the end stays one", "ราคา 3,900 บาทครับ\nสมัครได้ที่ https://x.example.com", []string{"ราคา 3,900 บาทครับ\nสมัครได้ที่ https://x.example.com"}},
		{"english", "Yes, it runs on Mac and Windows.\nWhich computer do you use?", []string{"Yes, it runs on Mac and Windows.", "Which computer do you use?"}},
		{"never more than two", "บรรทัดหนึ่ง\nบรรทัดสอง\nบรรทัดสาม\nใช้คอมรุ่นไหนครับ", []string{"บรรทัดหนึ่ง\nบรรทัดสอง\nบรรทัดสาม", "ใช้คอมรุ่นไหนครับ"}},
		{"a link alone after the question is not split off", "ทำคลิปแนวไหนครับ\nhttps://x.example.com", []string{"ทำคลิปแนวไหนครับ\nhttps://x.example.com"}},
		{"question word before a reason", "คอร์สสอนใช้ AI ช่วยตัดคลิป ดูซ้ำได้ครับ\nตอนนี้ทำคลิปแนวไหนอยู่ครับ จะได้แนะนำให้ตรงกับงานเลย",
			[]string{"คอร์สสอนใช้ AI ช่วยตัดคลิป ดูซ้ำได้ครับ", "ตอนนี้ทำคลิปแนวไหนอยู่ครับ จะได้แนะนำให้ตรงกับงานเลย"}},
		{"a statement last line is not split", "ราคา 3,900 บาทครับ\nสมัครได้ที่หน้าคอร์สเลยครับ", []string{"ราคา 3,900 บาทครับ\nสมัครได้ที่หน้าคอร์สเลยครับ"}},
		{"answer line, then link and question on the last line",
			"รองรับหลายแนวครับ\nดูตัวอย่างได้ที่ https://autocut.example.com/examples อยากดูราคาโปรตอนนี้ไหมครับ?",
			[]string{"รองรับหลายแนวครับ\nดูตัวอย่างได้ที่ https://autocut.example.com/examples", "อยากดูราคาโปรตอนนี้ไหมครับ?"}},
		{"empty", "", []string{""}},
	}
	for _, c := range cases {
		got := splitReply(c.reply)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestProcess_SendsAnswerThenQuestionWithAPauseBetween(t *testing.T) {
	// Arrange
	cw, l := &fakeChatwoot{}, &fakeLLM{reply: "ดูตัวอย่างได้ที่ https://example.com/examples\nอยากดูราคาโปรตอนนี้ไหมครับ"}
	b, h := newBot(cw, l)
	var pauses []time.Duration
	b.wait = func(_ context.Context, d time.Duration) { pauses = append(pauses, d) }

	// Act
	b.Process(context.Background(), customerMsg("ขอดูตัวอย่างหน่อยครับ"))

	// Assert
	got := cw.joined()
	first := strings.Index(got, "msg:ดูตัวอย่างได้ที่ https://example.com/examples")
	second := strings.Index(got, "msg:อยากดูราคาโปรตอนนี้ไหมครับ")
	if first < 0 || second < first {
		t.Fatalf("want two messages, answer first: %q", got)
	}
	if len(pauses) != 1 || pauses[0] < partGap {
		t.Errorf("pauses %v, want one pause of at least %v before the question", pauses, partGap)
	}
	hs := h.Get(1, 7)
	if last := hs[len(hs)-1]; last.Role != "assistant" || !strings.Contains(last.Content, "\nอยากดูราคา") {
		t.Errorf("history must keep the reply as one turn: %+v", last)
	}
	_ = memory.Turn{}
}
