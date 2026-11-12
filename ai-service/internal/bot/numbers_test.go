package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/profile"
)

func TestUnverifiedNumbers(t *testing.T) {
	trusted := "บัญชีไทยพาณิชย์ พรพิมล พวงใส เลขบัญชี 9952310945\nโทร 044-115-229 หรือคุณชมพู่ 081-516-9097"
	cases := map[string]struct {
		reply string
		bad   int
	}{
		"exact account":                {"โอนเข้า ไทยพาณิชย์ 9952310945 ได้เลยครับ", 0},
		"account with dashes":          {"เลขบัญชี 995-2-31094-5 ครับ", 0},
		"account with spaces":          {"เลขบัญชี 995 231 0945", 0},
		"thai digits":                  {"เลขบัญชี ๙๙๕๒๓๑๐๙๔๕", 0},
		"phones from the profile":      {"โทร 044-115-229 หรือ 0815169097 ครับ", 0},
		"prices and dates pass":        {"ราคา 9,900 บาท โปร 3,900 ถึง 30 พ.ย. 2026 (2026-11-30) 30 ที่นั่ง", 0},
		"one wrong digit":              {"เลขบัญชี 9952310946 ครับ", 1},
		"one digit missing":            {"เลขบัญชี 995231094 ครับ", 1},
		"a number from nowhere":        {"โอนเข้า 123-4-56789-0 นะครับ", 1},
		"two wrong numbers":            {"1234567890 หรือ 0987654321", 2},
		"customer's number not echoed": {"เบอร์ 0899999999 ใช่ไหมครับ", 1},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Act
			got := unverifiedNumbers(c.reply, trusted)

			// Assert
			if len(got) != c.bad {
				t.Fatalf("unverifiedNumbers(%q) = %v, want %d", c.reply, got, c.bad)
			}
		})
	}
}

func TestUnverifiedNumbers_NothingTrusted(t *testing.T) {
	// Arrange: no profile and no knowledge: any account-like number is held.
	reply := "โอนเข้า 9952310945"

	// Act
	got := unverifiedNumbers(reply)

	// Assert
	if len(got) != 1 {
		t.Fatalf("got %v, want the number held", got)
	}
}

func bankBot(reply string) (*Bot, *fakeChatwoot) {
	cw := &fakeChatwoot{}
	b, _ := newBot(cw, &fakeLLM{reply: reply})
	b.opts.Profiles = &fakeProfiles{byAccount: map[int64]profile.Profile{
		1: {AccountID: 1, BusinessName: "Shop", BankTransfer: "ไทยพาณิชย์ พรพิมล พวงใส 9952310945"},
	}}
	return b, cw
}

func TestProcess_SendsTheProfilesBankAccount(t *testing.T) {
	// Arrange
	b, cw := bankBot("โอนเข้าไทยพาณิชย์ 995-2-31094-5 ได้เลยครับ")

	// Act
	b.Process(context.Background(), customerMsg("ขอเลขบัญชี"))

	// Assert
	if got := cw.joined(); got != "typing:on | msg:โอนเข้าไทยพาณิชย์ 995-2-31094-5 ได้เลยครับ | typing:off" {
		t.Errorf("calls = %s, want the reply sent", got)
	}
}

func TestProcess_HoldsAWrongAccountNumber(t *testing.T) {
	for name, reply := range map[string]string{
		"one digit off":                 "โอนเข้าไทยพาณิชย์ 9952310946 ได้เลยครับ",
		"inside a handoff message too":  "โอนเข้า 1234567890 แล้วส่งสลิปนะครับ\n[[HANDOFF]]",
		"customer-supplied in the chat": "โอนเข้าบัญชี 0812345678 ตามที่แจ้งได้เลยครับ",
	} {
		t.Run(name, func(t *testing.T) {
			// Arrange
			b, cw := bankBot(reply)

			// Act
			b.Process(context.Background(), customerMsg("โอนเข้า 0812345678 ได้ไหม"))

			// Assert
			got := cw.joined()
			if strings.Contains(got, "99523109") || strings.Contains(got, "1234567890") || strings.Contains(got, "msg:โอน") {
				t.Fatalf("a held reply reached the customer: %s", got)
			}
			if !strings.Contains(got, "msg:connecting you") || !strings.Contains(got, "handoff") || !strings.Contains(got, ReasonNumberHeld) {
				t.Errorf("calls = %s, want the handoff message, a handoff and the reason note", got)
			}
		})
	}
}

func TestPreviewAndAnswer_HoldAWrongAccountNumber(t *testing.T) {
	// Arrange
	b, _ := bankBot("โอนเข้า 9952310946 ครับ")
	history := []memory.Turn{{Role: "user", Content: "ขอเลขบัญชี"}}

	// Act
	p := b.Preview(context.Background(), 1, 0, history)
	res, _, err := b.Answer(context.Background(), 1, 0, history)

	// Assert
	if !p.Handoff || p.Reason != ReasonNumberHeld || strings.Contains(p.Reply, "9952310946") {
		t.Errorf("preview = %+v, want a held handoff", p)
	}
	if text, handoff := SplitHandoff(res.Content); err != nil || !handoff || text != "" {
		t.Errorf("answer = %q, %v; want a bare handoff", res.Content, err)
	}
}
