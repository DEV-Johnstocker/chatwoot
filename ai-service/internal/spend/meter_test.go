package spend

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DEV-Johnstocker/chatwoot/ai-service/internal/llm"
)

type memRec struct {
	mu     sync.Mutex
	events []Event
}

func (m *memRec) Record(e Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
}

type fakeLLM struct {
	res llm.Result
	err error
}

func (f fakeLLM) Complete(context.Context, []llm.Message) (llm.Result, error) { return f.res, f.err }

type fakeEmbedder struct {
	model string
	err   error
}

func (f fakeEmbedder) Model() string { return f.model }
func (f fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	return make([][]float32, len(texts)), nil
}

var fixed = time.Date(2026, 11, 16, 3, 0, 0, 0, time.UTC)

func TestLLM_RecordsAnsweredCallWithAccountAndPurpose(t *testing.T) {
	// Arrange
	rec := &memRec{}
	m := &LLM{Next: fakeLLM{res: llm.Result{Content: "hi", Model: "gpt-6-luna", PromptTokens: 900, CachedTokens: 500, CompletionTokens: 40}},
		Purpose: PurposeReply, Rec: rec, Now: func() time.Time { return fixed }}

	// Act
	res, err := m.Complete(WithAccount(context.Background(), 7), nil)

	// Assert
	want := Event{AccountID: 7, Purpose: PurposeReply, Model: "gpt-6-luna", InputTokens: 900, CachedTokens: 500, OutputTokens: 40, At: fixed}
	if err != nil || res.Content != "hi" || len(rec.events) != 1 || rec.events[0] != want {
		t.Fatalf("res=%+v err=%v events=%+v", res, err, rec.events)
	}
}

func TestLLM_FailedCallRecordsNothing(t *testing.T) {
	// Arrange
	rec := &memRec{}
	m := &LLM{Next: fakeLLM{err: errors.New("429")}, Purpose: PurposeReply, Rec: rec}

	// Act
	_, err := m.Complete(context.Background(), nil)

	// Assert
	if err == nil || len(rec.events) != 0 {
		t.Fatalf("err=%v events=%+v", err, rec.events)
	}
}

func TestLLM_FallbackAccountAndPurposeOverride(t *testing.T) {
	// Arrange: the comment queue has no account in ctx; the owner's test chat overrides the purpose
	rec := &memRec{}
	m := &LLM{Next: fakeLLM{res: llm.Result{Model: "m"}}, Purpose: PurposeComment, Rec: rec, Account: 3}

	// Act
	_, _ = m.Complete(context.Background(), nil)
	_, _ = m.Complete(WithPurpose(WithAccount(context.Background(), 9), PurposeTest), nil)

	// Assert
	if len(rec.events) != 2 || rec.events[0].AccountID != 3 || rec.events[0].Purpose != PurposeComment ||
		rec.events[1].AccountID != 9 || rec.events[1].Purpose != PurposeTest {
		t.Fatalf("events=%+v", rec.events)
	}
}

func TestEmbedder_EstimatesWhenProviderSendsNoUsage(t *testing.T) {
	// Arrange
	rec := &memRec{}
	m := &Embedder{Next: fakeEmbedder{model: "text-embedding-3-large"}, Purpose: PurposeEmbedding, Rec: rec}

	// Act
	_, err := m.Embed(WithAccount(context.Background(), 2), []string{"abcdef", "ราคา"}) // 6 + 12 bytes

	// Assert
	if err != nil || len(rec.events) != 1 {
		t.Fatalf("err=%v events=%+v", err, rec.events)
	}
	e := rec.events[0]
	if e.InputTokens != 6 || e.Model != "text-embedding-3-large" || e.AccountID != 2 || e.Purpose != PurposeEmbedding {
		t.Fatalf("event=%+v", e)
	}
}

func TestEmbedder_FailedCallRecordsNothing(t *testing.T) {
	// Arrange
	rec := &memRec{}
	m := &Embedder{Next: fakeEmbedder{err: errors.New("down")}, Rec: rec}

	// Act
	_, err := m.Embed(context.Background(), []string{"x"})

	// Assert
	if err == nil || len(rec.events) != 0 {
		t.Fatalf("err=%v events=%+v", err, rec.events)
	}
}

func TestWithAccount_IgnoresNonPositive(t *testing.T) {
	// Arrange
	ctx := WithAccount(context.Background(), -4)

	// Act
	got := AccountFrom(ctx)

	// Assert
	if got != 0 || AccountFrom(context.Background()) != 0 {
		t.Fatalf("got %d", got)
	}
}

type failInserter struct{ calls int }

func (f *failInserter) Insert(context.Context, []Event) error {
	f.calls++
	return errors.New("db down")
}

func TestWriter_FullQueueDropsWithoutBlocking(t *testing.T) {
	// Arrange: room for one event, writer not started
	ins := &failInserter{}
	w := NewWriter(ins, quiet(), 1)

	// Act
	w.Record(Event{Purpose: "reply"})
	w.Record(Event{Purpose: "reply"}) // queue full: dropped, returns at once
	w.Start()
	w.Close() // flush fails: counted as dropped too

	// Assert
	if got := w.Dropped.Load(); got != 2 || ins.calls != 1 {
		t.Fatalf("dropped=%d inserts=%d", got, ins.calls)
	}
}
