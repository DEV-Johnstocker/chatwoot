package bot

import (
	"errors"
	"testing"
	"time"
)

func TestEnqueueWaitsBrieflyForRoom(t *testing.T) {
	// Arrange: one worker with room for one job, already taken.
	b, _ := newBot(&fakeChatwoot{}, &fakeLLM{})
	b = New(b.opts, 1, 1)
	b.enqueueWait = time.Second
	if err := b.Enqueue(customerMsg("first")); err != nil {
		t.Fatal(err)
	}
	go func() { // the worker frees the slot shortly after
		time.Sleep(50 * time.Millisecond)
		<-b.queues[0]
	}()

	// Act
	err := b.Enqueue(customerMsg("second"))

	// Assert
	if err != nil {
		t.Fatalf("a job must wait for room instead of failing at once: %v", err)
	}
}

func TestEnqueueGivesUpWhenTheQueueStaysFull(t *testing.T) {
	// Arrange
	b, _ := newBot(&fakeChatwoot{}, &fakeLLM{})
	b = New(b.opts, 1, 1)
	b.enqueueWait = 30 * time.Millisecond
	_ = b.Enqueue(customerMsg("first"))

	// Act
	started := time.Now()
	err := b.Enqueue(customerMsg("second"))

	// Assert
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("err = %v, want ErrQueueFull", err)
	}
	if waited := time.Since(started); waited < 30*time.Millisecond || waited > time.Second {
		t.Errorf("waited %v, want about the enqueue wait", waited)
	}
}

func TestCloseWhileEnqueueWaitsDoesNotPanic(t *testing.T) {
	// Arrange: the queue is full and a webhook handler is waiting for room
	// when shutdown closes the bot.
	b, _ := newBot(&fakeChatwoot{}, &fakeLLM{})
	b = New(b.opts, 1, 1)
	b.enqueueWait = 5 * time.Second
	_ = b.Enqueue(customerMsg("first"))
	result := make(chan error, 1)
	go func() { result <- b.Enqueue(customerMsg("second")) }()
	time.Sleep(20 * time.Millisecond)

	// Act
	b.Close()

	// Assert
	select {
	case err := <-result:
		if !errors.Is(err, ErrQueueFull) {
			t.Errorf("err = %v, want ErrQueueFull", err)
		}
	case <-time.After(time.Second):
		t.Fatal("a waiting Enqueue must return when the bot closes")
	}
	if err := b.Enqueue(customerMsg("after close")); !errors.Is(err, ErrQueueFull) {
		t.Errorf("Enqueue after Close = %v, want ErrQueueFull", err)
	}
}
