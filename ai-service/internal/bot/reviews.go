package bot

// The review image: the owner uploads one picture of real customer reviews;
// where the model writes profile.ReviewMarker the bot sends it, at most once
// per chat. The model never describes reviews itself.
// Issue: DEV-Johnstocker/chatwoot#1

import (
	"context"
	"log/slog"
	"regexp"
	"strings"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/memory"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/reviews"
)

// ReviewImages gives the bot each account's review image (reviews.Store).
type ReviewImages interface {
	Get(ctx context.Context, accountID int64) (reviews.Image, bool, error)
	Has(ctx context.Context, accountID int64) (bool, error)
}

// AttachmentSender sends a file to the customer (chatwoot.Client).
type AttachmentSender interface {
	SendAttachment(ctx context.Context, accountID, conversationID int64, content, filename, contentType string, data []byte) error
}

// ReviewImageSent stands for the image in Answer's text (answer-eval), so
// the judge sees that real reviews were sent.
const ReviewImageSent = "[ส่งรูปรีวิวจากลูกค้าจริงที่เจ้าของอัปโหลดไว้]"

// reviewMarker matches profile.ReviewMarker and near-misses ("[[ review ]]").
var reviewMarker = regexp.MustCompile(`(?i)\[\[\s*reviews?\s*\]\]`)

// takeReviewMarker removes every review marker and reports whether there was one.
func takeReviewMarker(s string) (string, bool) {
	if !reviewMarker.MatchString(s) {
		return s, false
	}
	// The marker usually sits on its own line: take the line break before it too.
	return strings.TrimSpace(reviewMarkerLine.ReplaceAllLiteralString(s, "")), true
}

// reviewMarkerLine is a review marker with the break and spaces before it.
var reviewMarkerLine = regexp.MustCompile(`(?i)\n?[ \t]*\[\[\s*reviews?\s*\]\][ \t]*`)

// reviewSentBefore reports whether an earlier bot reply in this chat already
// asked for the review image.
func reviewSentBefore(history []memory.Turn) bool {
	for _, t := range history {
		if t.Role == "assistant" && reviewMarker.MatchString(t.Content) {
			return true
		}
	}
	return false
}

// hasReviewImage reports whether the account has a review image. A lookup
// failure counts as no: the bot then never offers it.
func (b *Bot) hasReviewImage(ctx context.Context, log *slog.Logger, acct int64) bool {
	if b.opts.Reviews == nil {
		return false
	}
	if _, ok := b.opts.Chatwoot.(AttachmentSender); !ok && b.opts.Chatwoot != nil {
		return false
	}
	ok, err := b.opts.Reviews.Has(ctx, acct)
	if err != nil {
		log.Warn("review image lookup failed", "error", err)
		return false
	}
	return ok
}

// sendReviewImage sends the account's review image. It reports whether the
// image went out; a failure is logged and the chat goes on without it.
func (b *Bot) sendReviewImage(ctx context.Context, log *slog.Logger, acct, conv int64) bool {
	sender, ok := b.opts.Chatwoot.(AttachmentSender)
	if !ok || b.opts.Reviews == nil {
		return false
	}
	img, ok, err := b.opts.Reviews.Get(ctx, acct)
	if err != nil || !ok {
		log.Warn("review image not sent: none set or lookup failed", "error", err)
		return false
	}
	if err := sender.SendAttachment(ctx, acct, conv, img.Caption, img.Filename(), img.ContentType, img.Data); err != nil {
		log.Error("send review image failed", "error", err)
		return false
	}
	log.Info("review image sent", "bytes", len(img.Data))
	return true
}
