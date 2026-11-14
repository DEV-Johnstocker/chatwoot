package bot

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
)

// A failed or timed-out chat model must hand the chat to staff fast (issue #1).

// stallLLM blocks until its context ends, like a provider that times out.
type stallLLM struct{}

func (stallLLM) Complete(ctx context.Context, _ []llm.Message) (llm.Result, error) {
	<-ctx.Done()
	return llm.Result{}, ctx.Err()
}

// ctxChatwoot fails every call whose context has already ended, as the
// real HTTP client does.
type ctxChatwoot struct{ fakeChatwoot }

func (c *ctxChatwoot) SendMessage(ctx context.Context, a, conv int64, s string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.fakeChatwoot.SendMessage(ctx, a, conv, s)
}

func (c *ctxChatwoot) SendPrivateNote(ctx context.Context, a, conv int64, s string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.fakeChatwoot.SendPrivateNote(ctx, a, conv, s)
}

func (c *ctxChatwoot) Handoff(ctx context.Context, a, conv int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.fakeChatwoot.Handoff(ctx, a, conv)
}

func TestLLMErrorSkipsTheAISummary(t *testing.T) {
	// Arrange: the chat model failed, so the note model (same provider) is
	// not asked to spend another 15 s on a summary.
	cw := &fakeChatwoot{}
	b, _ := newBot(cw, &fakeLLM{err: errors.New("provider timeout")})
	notes := &noteLLM{reply: "เหตุผล: ไม่ควรถูกเรียก"}
	b.opts.NoteLLM = notes

	// Act
	b.Process(context.Background(), customerMsg("ราคาเท่าไหร่"))

	// Assert
	if notes.got != nil {
		t.Error("the note model must not be called after the chat model failed")
	}
	want := "msg:sorry, connecting you | note:🤖 **บอทส่งต่อ:** " + reasonLabels[ReasonLLMError] + "\n" + ReasonLLMError + " | handoff"
	if got := cw.joined(); !strings.Contains(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

func TestTimedOutTurnStillHandsOff(t *testing.T) {
	// Arrange: the model uses up the whole per-message budget.
	cw := &ctxChatwoot{}
	b, _ := newBot(&cw.fakeChatwoot, &fakeLLM{})
	b.opts.Chatwoot, b.opts.LLM, b.opts.Timeout = cw, stallLLM{}, 50*time.Millisecond

	// Act
	b.Process(context.Background(), customerMsg("ราคาเท่าไหร่"))

	// Assert: the failure message, note and handoff still reach Chatwoot.
	got := cw.joined()
	if !strings.Contains(got, "msg:sorry, connecting you") || !strings.Contains(got, "| handoff") {
		t.Fatalf("a timed-out turn must still hand off, calls = %q", got)
	}
}
