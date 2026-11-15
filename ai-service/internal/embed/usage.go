package embed

import (
	"context"
	"sync"
)

// Counter sums the prompt tokens the provider reported for the embeddings
// calls made with one context, and which model answered last. The spend
// meter puts one in the context, so a fallback provider is counted too.
// Issue: DEV-Johnstocker/chatwoot#1
type Counter struct {
	mu     sync.Mutex
	tokens int
	model  string
}

type counterKey struct{}

// WithCounter returns ctx carrying a new Counter.
func WithCounter(ctx context.Context) (context.Context, *Counter) {
	c := &Counter{}
	return context.WithValue(ctx, counterKey{}, c), c
}

func counterFrom(ctx context.Context) *Counter {
	c, _ := ctx.Value(counterKey{}).(*Counter)
	return c
}

func (c *Counter) add(tokens int, model string) {
	if c == nil || tokens <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokens += tokens
	c.model = model
}

// Get returns the summed tokens and the last model that reported usage.
func (c *Counter) Get() (int, string) {
	if c == nil {
		return 0, ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tokens, c.model
}
