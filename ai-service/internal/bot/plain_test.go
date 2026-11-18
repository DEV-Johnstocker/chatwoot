package bot

import (
	"testing"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
)

func TestPlainChat(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"bold":            {"ใส่คูปอง **JOHNAUTOCUT99** ครับ", "ใส่คูปอง JOHNAUTOCUT99 ครับ"},
		"underscore bold": {"ใช้ __โค้ดนี้__", "ใช้ โค้ดนี้"},
		"markdown link":   {"สมัครที่ [หน้าคอร์ส](https://example.com/c/1) ครับ", "สมัครที่ หน้าคอร์ส https://example.com/c/1 ครับ"},
		"link as its url": {"[https://example.com/c/1](https://example.com/c/1)", "https://example.com/c/1"},
		"heading":         {"### ราคา\n3,900 บาท", "ราคา\n3,900 บาท"},
		"arabic question": {"ทำคลิปแนวไหนครับ؟", "ทำคลิปแนวไหนครับ?"},
		"handoff marker":  {"เดี๋ยวแอดมินตอบครับ\n[[HANDOFF]]", "เดี๋ยวแอดมินตอบครับ\n[[HANDOFF]]"},
		"plain untouched": {"ราคา 3,900 - จ่ายครั้งเดียว * เงื่อนไข", "ราคา 3,900 - จ่ายครั้งเดียว * เงื่อนไข"},
		"hashtag kept":    {"#JohnAutoCut ครับ", "#JohnAutoCut ครับ"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Act
			got := plainChat(tc.in)

			// Assert
			if got != tc.want {
				t.Errorf("plainChat(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDropStrayScripts_RemovesLettersTheCustomerDidNotUse(t *testing.T) {
	cases := []struct{ name, reply, customer, want string }{
		{"arabic inside a thai word", "ใช้กับคอมبيوتر?", "ใช้ได้ไหม", "ใช้กับคอม?"},
		{"russian word at the end", "จะได้ส่งตัวอย่างให้ดูตรงงาน настроение", "รับงานลูกค้าได้ไหม", "จะได้ส่งตัวอย่างให้ดูตรงงาน"},
		{"thai and english stay", "ราคา 3,900 บาท www.example.com 😊", "ราคา", "ราคา 3,900 บาท www.example.com 😊"},
		{"customer wrote russian", "Привет! Цена 3 900 бат.", "Сколько стоит?", "Привет! Цена 3 900 бат."},
		{"japanese stays when asked in japanese", "価格は3,900バーツです", "いくら?", "価格は3,900バーツです"},
		{"stray chinese in english", "The price is 3,900 baht 价格.", "How much?", "The price is 3,900 baht."},
	}
	for _, c := range cases {
		// Act
		got := dropStrayScripts(c.reply, c.customer)

		// Assert
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLastCustomerText_IsTheLatestUserTurn(t *testing.T) {
	// Arrange
	h := []memory.Turn{{Role: "user", Content: "สวัสดี"}, {Role: "assistant", Content: "สวัสดีครับ"}, {Role: "user", Content: "ราคา"}}

	// Act / Assert
	if got := lastCustomerText(h); got != "ราคา" {
		t.Errorf("got %q, want the latest customer message", got)
	}
	if got := lastCustomerText(nil); got != "" {
		t.Errorf("empty history gave %q", got)
	}
}
