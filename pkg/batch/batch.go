// Package batch implements a dynamic batcher. Callers submit items
// individually and the batcher groups them by size or time window,
// whichever happens first. This trades a small bounded latency for
// a large gain in throughput on the downstream call.
package batch

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrClosed              = errors.New("batch: batcher is closed")
	ErrResultCountMismatch = errors.New("batch: flusher returned wrong number of results")
)

// Flusher processes a full batch. The implementation must to be safe for
// concurrent calls because two windows can overlap under load.
type Flusher[In any, Out any] func(ctx context.Context, items []In) ([]Out, error)

// Batcher holds the queue and configuration for processing items in batches.
type Batcher[In any, Out any] struct {
	maxSize int
	maxWait time.Duration
	flush   Flusher[In, Out]
	mu      sync.Mutex
	queue   chan request[In, Out]
	closed  bool
	wg      sync.WaitGroup
}

type request[In any, Out any] struct {
	item In
	resp chan result[Out]
}

type result[Out any] struct {
	err   error
	value Out
}

// New creates a batcher with the given size and time limits.
// Values of maxSize below one are treated as one.
func New[In any, Out any](maxSize int, maxWait time.Duration, flush Flusher[In, Out]) *Batcher[In, Out] {
	if maxSize < 1 {
		maxSize = 1
	}

	b := &Batcher[In, Out]{
		maxSize: maxSize,
		maxWait: maxWait,
		flush:   flush,
		queue:   make(chan request[In, Out], maxSize*4),
	}

	b.wg.Add(1)
	go b.loop()
	return b
}

// Submit enqueues an item, then waits for its result or context cancellation.
// It returns ErrClosed if the batcher is closed.
func (b *Batcher[In, Out]) Submit(ctx context.Context, item In) (Out, error) {
	var zero Out

	b.mu.Lock()

	if b.closed {
		b.mu.Unlock()
		return zero, ErrClosed
	}

	req := request[In, Out]{
		item: item,
		resp: make(chan result[Out], 1),
	}

	select {
	case b.queue <- req:
	case <-ctx.Done():
		b.mu.Unlock()
		return zero, ctx.Err()
	}
	b.mu.Unlock()

	select {
	case res := <-req.resp:
		return res.value, res.err
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

// Close closes the queue and waits for the background goroutine to finish.
// If the batcher is already closed, it returns immediately.
func (b *Batcher[In, Out]) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	close(b.queue)
	b.mu.Unlock()
	b.wg.Wait()
}

func (b *Batcher[In, Out]) dispatch(batch []request[In, Out]) {
	items := make([]In, len(batch))
	for i, req := range batch {
		items[i] = req.item
	}

	outs, err := b.flush(context.Background(), items)
	if err == nil && len(outs) != len(batch) {
		err = ErrResultCountMismatch
	}

	for i, req := range batch {
		if err != nil {
			var zero Out
			req.resp <- result[Out]{err: err, value: zero}
			continue
		}
		req.resp <- result[Out]{err: nil, value: outs[i]}
	}
}

func (b *Batcher[In, Out]) loop() {
	defer b.wg.Done()

	pending := make([]request[In, Out], 0, b.maxSize)
	timer := time.NewTimer(b.maxWait)
	defer timer.Stop()

	flushPending := func() {
		if len(pending) == 0 {
			return
		}

		batch := pending
		pending = make([]request[In, Out], 0, b.maxSize)
		b.dispatch(batch)
	}

	for {
		select {
		case req, ok := <-b.queue:
			if !ok {
				flushPending()
				return
			}

			pending = append(pending, req)

			if len(pending) == 1 {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}

				timer.Reset(b.maxWait)
			}

			if len(pending) == b.maxSize {
				flushPending()
			}
		case <-timer.C:
			flushPending()
		}
	}
}
