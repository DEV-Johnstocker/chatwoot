// Package spend records what every model call uses (tokens, model, purpose)
// and turns it into an estimated cost, so the owner sees AI spend on /admin
// and gets a Slack alert at 80% and 100% of a cap. Caps only alert: the bot
// keeps answering past them (owner decision, 2026-11-14).
// Issue: DEV-Johnstocker/chatwoot#1
package spend

import (
	"context"
	"time"
)

// Purposes: what a model call was for.
const (
	PurposeReply         = "reply"
	PurposeRewrite       = "rewrite"
	PurposeNote          = "note"
	PurposeSticker       = "sticker"
	PurposeComment       = "comment"
	PurposeCommentReview = "comment_review"
	PurposeEmbedding     = "embedding"
	PurposeTranscription = "transcription"
	PurposeNudge         = "nudge" // the AI-written follow-up to a quiet chat
	PurposeTest          = "test"  // the owner's test chat on /admin and MCP test_bot
)

// Purposes lists every purpose in display order.
var Purposes = []string{PurposeReply, PurposeRewrite, PurposeNote, PurposeSticker, PurposeComment,
	PurposeCommentReview, PurposeEmbedding, PurposeTranscription, PurposeNudge, PurposeTest}

// Event is one model call's usage. It never carries message text.
type Event struct {
	AccountID    int64 // 0: not tied to one business
	Purpose      string
	Model        string
	InputTokens  int // all prompt tokens, cached ones included
	CachedTokens int // prompt tokens the provider served from its cache
	OutputTokens int
	AudioSeconds float64 // duration-billed transcription
	At           time.Time
}

// Recorder takes usage events. Record must not block the caller.
type Recorder interface {
	Record(Event)
}

type accountKey struct{}
type purposeKey struct{}

// WithAccount tags ctx with the business (Chatwoot account) a call is for.
func WithAccount(ctx context.Context, accountID int64) context.Context {
	return context.WithValue(ctx, accountKey{}, accountID)
}

// AccountFrom returns the account ctx is tagged with, else 0.
func AccountFrom(ctx context.Context) int64 {
	if id, ok := ctx.Value(accountKey{}).(int64); ok && id > 0 {
		return id
	}
	return 0
}

// WithPurpose overrides the purpose of every call made with ctx (the owner's
// test chat runs the reply pipeline but is not customer traffic).
func WithPurpose(ctx context.Context, purpose string) context.Context {
	return context.WithValue(ctx, purposeKey{}, purpose)
}

func purposeFrom(ctx context.Context, fallback string) string {
	if p, ok := ctx.Value(purposeKey{}).(string); ok && p != "" {
		return p
	}
	return fallback
}
