package bot

import (
	"context"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/commentecho"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/webhook"
)

// CommentLeadLabel marks a chat that a private reply to a Facebook comment
// started or joined, so staff can filter those chats.
const CommentLeadLabel = "comment-lead"

// maxNoteComment caps how much of the customer's comment the note quotes.
const maxNoteComment = 200

// CommentEchoes returns the comment behind a private reply by the Meta
// message id Chatwoot stores as the echo's source_id (commentecho.Echoes).
type CommentEchoes interface {
	Take(messageID string) (commentecho.Lead, bool)
}

// commentEcho reports whether ev is the echo of a private reply the owner
// sent from the comment queue, and the comment it answered. It is used once.
func (b *Bot) commentEcho(ev webhook.Event) (commentecho.Lead, bool) {
	if b.opts.CommentEchoes == nil || ev.Event != "message_created" || ev.MessageType != "outgoing" ||
		ev.Private || !ev.ContentAttributes.ExternalEcho || ev.SourceID == "" {
		return commentecho.Lead{}, false
	}
	return b.opts.CommentEchoes.Take(ev.SourceID)
}

// noteCommentLead labels the chat and tells staff, in a private note, that it
// came from a comment and why Chatwoot may say they cannot reply yet: Meta
// allows one private reply per comment until the customer answers, and then
// replies within 24 hours of the customer's last message. Best effort.
func (b *Bot) noteCommentLead(ctx context.Context, log *slog.Logger, acct, conv int64, lead commentecho.Lead) {
	if l, ok := b.opts.Chatwoot.(Labeler); ok {
		if err := l.AddLabel(ctx, acct, conv, CommentLeadLabel); err != nil {
			log.Warn("comment lead label failed", "error", err)
		}
	}
	if err := b.opts.Chatwoot.SendPrivateNote(ctx, acct, conv, commentLeadNote(lead)); err != nil {
		log.Warn("comment lead note failed", "error", err)
	}
}

// commentLeadNote is the staff note for a chat a private reply landed in.
func commentLeadNote(lead commentecho.Lead) string {
	var b strings.Builder
	b.WriteString("💬 **แชทนี้มาจากการตอบคอมเมนต์ใน Facebook** (ส่งจากหน้าคอมเมนต์)\n")
	if c := quoteLine(lead.Message); c != "" {
		b.WriteString("คอมเมนต์ของลูกค้า: \"" + c + "\"\n")
	}
	// Chatwoot shows the banner when the customer has sent no chat message in
	// the last 24 hours (Conversations::MessageWindowService); a comment does
	// not count, so a private reply alone never opens the window.
	b.WriteString("ถ้าเห็นแถบ \"24 hour message window\" แปลว่าลูกค้ายังไม่ได้ทักแชทมาใน 24 ชม.ล่าสุด (คอมเมนต์ไม่นับ) " +
		"พอลูกค้าตอบกลับ ตอบในแชทนี้ได้ตามปกติ")
	return b.String()
}

// quoteLine turns customer text into one short line that cannot break the
// note's layout: no newlines or Markdown markers, at most maxNoteComment runes.
func quoteLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.NewReplacer("*", "", "_", "", "`", "", "#", "", "[", "", "]", "", "\"", "'").Replace(s)
	if utf8.RuneCountInString(s) > maxNoteComment {
		s = string([]rune(s)[:maxNoteComment]) + "…"
	}
	return s
}
