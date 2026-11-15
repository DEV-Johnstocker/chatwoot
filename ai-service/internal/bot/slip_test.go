package bot

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/sales"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/trace"
)

type fakeSlips struct {
	mu  sync.Mutex
	got []sales.Sale
	err error
}

func (f *fakeSlips) RecordSlip(_ context.Context, s sales.Sale) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, s)
	return f.err
}

func TestSlipPhotoMarksTheChatUnconfirmedPaid(t *testing.T) {
	// Arrange
	cw := &fakeChatwoot{}
	b, _ := newBot(cw, &fakeLLM{reply: "ขอส่งให้ทีมงานตรวจนะคะ\n" + HandoffMarker})
	b.opts.Chatwoot = routingChatwoot{cw}
	b.opts.NoteLLM = &noteLLM{reply: "หมวด: ชำระเงิน\nเหตุผล: ลูกค้าส่งสลิป\nจากไฟล์: สลิปโอน 3,900 บาท 15 พ.ย. 14:02"}
	slips := &fakeSlips{}
	b.opts.Sales = slips

	// Act
	b.reply(context.Background(), b.opts.Logger, 1, 2, 7, &trace.Turn{}, []string{"data:image/png;base64,AAAA"})

	// Assert
	if got := cw.joined(); !strings.Contains(got, "label:"+sales.LabelSlip) || !strings.Contains(got, "💳 **"+sales.PaymentNoteTitle+"**") {
		t.Fatalf("calls = %q", got)
	}
	if len(slips.got) != 1 {
		t.Fatalf("slips = %+v", slips.got)
	}
	s := slips.got[0]
	if s.AccountID != 1 || s.ConversationID != 7 || s.InboxID != 2 || s.SlipAmount == nil || *s.SlipAmount != 390000 || s.SlipAt.IsZero() {
		t.Fatalf("slip = %+v", s)
	}
}

func TestNotASlipWithoutAPhotoOrPaymentCategory(t *testing.T) {
	cases := map[string]struct {
		reply  string
		images []string
	}{
		"payment talk, no photo":      {"หมวด: ชำระเงิน\nเหตุผล: ถามวิธีโอน", nil},
		"photo of something else":     {"หมวด: อื่นๆ\nจากไฟล์: รูปหน้าจอ error", []string{"data:image/png;base64,AAAA"}},
		"refund with a photo":         {"หมวด: คืนเงิน\nจากไฟล์: สลิป 3,900 บาท", []string{"data:image/png;base64,AAAA"}},
		"note model failed, no label": {"", []string{"data:image/png;base64,AAAA"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			cw := &fakeChatwoot{}
			b, _ := newBot(cw, &fakeLLM{reply: "ส่งต่อนะคะ\n" + HandoffMarker})
			b.opts.Chatwoot = routingChatwoot{cw}
			b.opts.NoteLLM = &noteLLM{reply: c.reply}
			slips := &fakeSlips{}
			b.opts.Sales = slips

			// Act
			b.reply(context.Background(), b.opts.Logger, 1, 2, 7, &trace.Turn{}, c.images)

			// Assert
			if strings.Contains(cw.joined(), sales.LabelSlip) || len(slips.got) != 0 {
				t.Fatalf("calls = %q slips = %+v", cw.joined(), slips.got)
			}
		})
	}
}

func TestSlipSaveFailureStillLabelsTheChat(t *testing.T) {
	// Arrange
	cw := &fakeChatwoot{}
	b, _ := newBot(cw, &fakeLLM{reply: "ส่งต่อนะคะ\n" + HandoffMarker})
	b.opts.Chatwoot = routingChatwoot{cw}
	b.opts.NoteLLM = &noteLLM{reply: "หมวด: ชำระเงิน\nจากไฟล์: สลิป"}
	b.opts.Sales = &fakeSlips{err: errors.New("db down")}

	// Act
	b.reply(context.Background(), b.opts.Logger, 1, 2, 7, &trace.Turn{}, []string{"data:image/png;base64,AAAA"})

	// Assert
	if got := cw.joined(); !strings.Contains(got, "label:"+sales.LabelSlip) || !strings.Contains(got, "handoff") {
		t.Fatalf("calls = %q", got)
	}
}
