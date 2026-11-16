package spend

import (
	"context"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/embed"
	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
)

// LLM meters a Completer: every answered call becomes one Event. A failed
// call is not recorded (the provider bills nothing without a completion).
type LLM struct {
	Next    llm.Completer
	Purpose string
	Rec     Recorder
	Account int64 // used when ctx carries no account (the comment queue's business)
	Now     func() time.Time
}

// Complete implements llm.Completer.
func (m *LLM) Complete(ctx context.Context, messages []llm.Message) (llm.Result, error) {
	res, err := m.Next.Complete(ctx, messages)
	if err == nil && m.Rec != nil {
		m.Rec.Record(Event{AccountID: account(ctx, m.Account), Purpose: purposeFrom(ctx, m.Purpose), Model: res.Model,
			InputTokens: res.PromptTokens, CachedTokens: res.CachedTokens, OutputTokens: res.CompletionTokens, At: now(m.Now)})
	}
	return res, err
}

// Embedder meters an embed.Embedder. Token counts come from the provider's
// usage field; when it sends none, they are estimated from the text.
type Embedder struct {
	Next    embed.Embedder
	Purpose string
	Rec     Recorder
	Now     func() time.Time
}

// Model implements embed.Embedder.
func (m *Embedder) Model() string { return m.Next.Model() }

// Embed implements embed.Embedder.
func (m *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	ctx, counter := embed.WithCounter(ctx)
	out, err := m.Next.Embed(ctx, texts)
	if m.Rec == nil {
		return out, err
	}
	tokens, model := counter.Get()
	if err == nil && tokens == 0 {
		tokens = estimateTokens(texts)
	}
	if tokens == 0 {
		return out, err // a failed call that the provider never counted
	}
	if model == "" {
		model = m.Next.Model()
	}
	m.Rec.Record(Event{AccountID: AccountFrom(ctx), Purpose: purposeFrom(ctx, m.Purpose), Model: model,
		InputTokens: tokens, At: now(m.Now)})
	return out, err
}

// estimateTokens is a deliberately high guess (one token per 3 bytes;
// Thai runs about 3 bytes a character) for providers that report no usage.
func estimateTokens(texts []string) int {
	n := 0
	for _, t := range texts {
		n += (len(t) + 2) / 3
	}
	return n
}

func account(ctx context.Context, fallback int64) int64 {
	if id := AccountFrom(ctx); id > 0 {
		return id
	}
	return max(fallback, 0)
}

func now(f func() time.Time) time.Time {
	if f != nil {
		return f()
	}
	return time.Now()
}
