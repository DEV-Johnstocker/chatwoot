package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/corrections"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/pending"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/webhook"
)

// Staff corrections: a "#fix" private note becomes an FAQ waiting for the
// owner's approval, and the bot answers with a private note so the staff
// member knows it worked. Issue: DEV-Johnstocker/chatwoot#1

// Corrector proposes a staff correction (corrections.Service).
type Corrector interface {
	Propose(ctx context.Context, n corrections.Note) (int64, error)
}

// Notes the bot writes back after a "#fix" note.
const (
	fixSentNote     = "✅ ส่งคำตอบที่แก้ให้เจ้าของตรวจแล้ว (รายการ #%s ในหน้า Approvals)\nบอทจะตอบแบบนี้หลังเจ้าของกดอนุมัติ"
	fixNoAnswerNote = "✏️ พิมพ์คำตอบที่ถูกต่อจาก #fix ในโน้ตเดียวกัน เช่น\n#fix คอร์สนี้ราคา 3,900 บาท ถึง 30 พ.ย."
	fixNoQNote      = "⚠️ ยังไม่ได้ส่ง: ไม่พบข้อความลูกค้าก่อนโน้ตนี้ ให้เพิ่มเป็นคำถาม-คำตอบในหน้า Knowledge base แทน"
	fixFullNote     = "⚠️ ยังไม่ได้ส่ง: มีรายการรออนุมัติเต็มแล้ว ให้เจ้าของตรวจในหน้า Approvals ก่อน แล้วส่ง #fix ใหม่"
	fixFailNote     = "⚠️ ยังไม่ได้ส่ง: ระบบบันทึกไม่สำเร็จ ลองส่ง #fix อีกครั้ง"
	fixOffNote      = "⚠️ ยังไม่ได้ส่ง: การส่งคำตอบที่แก้ยังไม่เปิดใช้ (ต้องเปิดหน้า Approvals ก่อน)"
)

// correct handles a "#fix" note: propose it and say how it went.
func (b *Bot) correct(ctx context.Context, log *slog.Logger, ev webhook.Event) {
	acct, conv := ev.Account.ID, ev.ConversationID()
	answer, _ := corrections.Parse(ev.Content)
	note := fixOffNote
	if b.opts.Corrections != nil {
		id, err := b.opts.Corrections.Propose(ctx, corrections.Note{AccountID: acct, ConversationID: conv, NoteID: ev.ID,
			Staff: ev.Sender.Name, Answer: answer, Memory: b.opts.History.Get(acct, conv)})
		switch {
		case err == nil:
			note = fmt.Sprintf(fixSentNote, strconv.FormatInt(id, 10))
			log.Info("staff correction proposed", "change_id", id)
		case errors.Is(err, corrections.ErrNoAnswer):
			note = fixNoAnswerNote
		case errors.Is(err, corrections.ErrNoQuestion):
			note = fixNoQNote
		case errors.Is(err, pending.ErrFull):
			note = fixFullNote
		default:
			note = fixFailNote
			log.Error("staff correction not saved", "error", err)
		}
	}
	if err := b.opts.Chatwoot.SendPrivateNote(ctx, acct, conv, note); err != nil {
		log.Error("correction note not sent", "error", err)
	}
}
