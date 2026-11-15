package spend

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Inserter writes usage events (Postgres).
type Inserter interface {
	Insert(ctx context.Context, events []Event) error
}

// Writer is the Recorder the meters use: Record only queues, and one
// goroutine writes batches, so a slow database never delays a customer's
// reply. When the queue is full, events are dropped and counted.
type Writer struct {
	store  Inserter
	logger *slog.Logger
	ch     chan Event
	done   chan struct{}
	mu     sync.RWMutex // guards closed against a Record racing Close
	closed bool
	// Dropped counts events lost to a full queue or a failed write.
	Dropped atomic.Int64
	// FlushEvery and BatchSize tune batching (defaults 2s and 200).
	FlushEvery time.Duration
	BatchSize  int
}

// NewWriter returns a Writer with room for queue events; call Start.
func NewWriter(store Inserter, logger *slog.Logger, queue int) *Writer {
	return &Writer{store: store, logger: logger, ch: make(chan Event, queue), done: make(chan struct{}),
		FlushEvery: 2 * time.Second, BatchSize: 200}
}

// Record implements Recorder. It never blocks.
func (w *Writer) Record(e Event) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return
	}
	select {
	case w.ch <- e:
	default:
		if w.Dropped.Add(1)%100 == 1 {
			w.logger.Warn("ai usage queue full, usage not recorded", "dropped_total", w.Dropped.Load())
		}
	}
}

// Start runs the batch writer until Close.
func (w *Writer) Start() {
	go w.loop()
}

// Close stops taking events, writes what is queued and returns.
func (w *Writer) Close() {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.ch)
	}
	w.mu.Unlock()
	<-w.done
}

func (w *Writer) loop() {
	defer close(w.done)
	tick := time.NewTicker(w.FlushEvery)
	defer tick.Stop()
	batch := make([]Event, 0, w.BatchSize)
	for {
		select {
		case e, ok := <-w.ch:
			if !ok {
				w.flush(batch)
				return
			}
			batch = append(batch, e)
			if len(batch) >= w.BatchSize {
				w.flush(batch)
				batch = batch[:0]
			}
		case <-tick.C:
			w.flush(batch)
			batch = batch[:0]
		}
	}
}

func (w *Writer) flush(batch []Event) {
	if len(batch) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := w.store.Insert(ctx, batch); err != nil {
		w.Dropped.Add(int64(len(batch)))
		w.logger.Warn("ai usage not recorded", "events", len(batch), "error", err)
	}
}
