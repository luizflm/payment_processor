package batch_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/luizflm/payment-processor/pkg/batch"
)

func TestBatchBySize(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		double := func(_ context.Context, items []int) ([]int, error) {
			calls.Add(1)
			if len(items) != 4 {
				t.Errorf("batch size = %d, want 4", len(items))
			}

			outs := make([]int, len(items))
			for i, item := range items {
				outs[i] = item * 2
			}
			return outs, nil
		}

		b := batch.New(4, time.Hour, double)
		defer b.Close()

		start := time.Now()
		results := make([]int, 8)
		var wg sync.WaitGroup

		for i := range results {
			wg.Go(func() {
				got, err := b.Submit(context.Background(), i)
				if err != nil {
					t.Errorf("Submit(%d) returned unexpected error: %v", i, err)
					return
				}
				results[i] = got
			})
		}
		wg.Wait()

		for i, got := range results {
			if got != i*2 {
				t.Errorf("results[%d] = %d, want %d", i, got, i*2)
			}
		}
		if got := calls.Load(); got != 2 {
			t.Errorf("flusher called %d times, want 2", got)
		}
		if elapsed := time.Since(start); elapsed != 0 {
			t.Errorf("processing took %v of virtual time, want 0", elapsed)
		}
	})
}

func TestBatchByTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const maxWait = 20 * time.Millisecond
		echo := func(_ context.Context, items []int) ([]int, error) {
			if len(items) != 1 {
				t.Errorf("batch size = %d, want 1", len(items))
			}
			return items, nil
		}

		b := batch.New(4, maxWait, echo)
		defer b.Close()

		for _, item := range []int{42, 7} {
			start := time.Now()
			got, err := b.Submit(context.Background(), item)
			elapsed := time.Since(start)

			if err != nil {
				t.Fatalf("Submit(%d) returned unexpected error: %v", item, err)
			}
			if got != item {
				t.Errorf("Submit(%d) = %d, want %d", item, got, item)
			}
			if elapsed != maxWait {
				t.Errorf("Submit(%d) took %v, want %v", item, elapsed, maxWait)
			}
		}
	})
}

func TestCloseFlushesPending(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		b := batch.New(4, time.Hour, func(_ context.Context, items []int) ([]int, error) {
			calls.Add(1)
			return items, nil
		})
		defer b.Close()

		go func() {
			got, err := b.Submit(context.Background(), 42)
			if err != nil || got != 42 {
				t.Errorf("Submit() = (%d, %v), want (42, nil)", got, err)
			}
		}()
		synctest.Wait()

		if got := calls.Load(); got != 0 {
			t.Fatalf("flusher called %d times before Close, want 0", got)
		}
		b.Close()
		if got := calls.Load(); got != 1 {
			t.Errorf("flusher called %d times after Close, want 1", got)
		}
	})
}

func TestSubmitCanceledWhileWaitingForResult(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		release := make(chan struct{})
		b := batch.New(1, time.Hour, func(_ context.Context, items []int) ([]int, error) {
			if items[0] == 42 {
				cancel()
				<-release
			}
			return items, nil
		})
		defer b.Close()

		_, err := b.Submit(ctx, 42)
		close(release)

		if !errors.Is(err, context.Canceled) {
			t.Errorf("Submit() error = %v, want context.Canceled", err)
		}

		got, err := b.Submit(context.Background(), 7)
		if err != nil || got != 7 {
			t.Errorf("subsequent Submit() = (%d, %v), want (7, nil)", got, err)
		}
	})
}

func TestSubmitCanceledUnderLoad(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	b := batch.New(1, time.Hour, func(_ context.Context, items []int) ([]int, error) {
		if items[0] == -1 {
			close(started)
			<-release
		}
		return items, nil
	})
	defer b.Close()

	var first sync.WaitGroup
	defer first.Wait()
	defer close(release)

	first.Go(func() {
		got, err := b.Submit(context.Background(), -1)
		if err != nil || got != -1 {
			t.Errorf("first Submit() = (%d, %v), want (-1, nil)", got, err)
		}
	})
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			got, err := b.Submit(ctx, i)
			if got != 0 || !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("Submit(%d) = (%d, %v), want (0, context.DeadlineExceeded)", i, got, err)
			}
		})
	}
	wg.Wait()
}

func TestSubmitAfterClose(t *testing.T) {
	var calls atomic.Int32
	b := batch.New(1, time.Second, func(_ context.Context, items []int) ([]int, error) {
		calls.Add(1)
		return items, nil
	})
	defer b.Close()

	got, err := b.Submit(context.Background(), 42)
	if err != nil || got != 42 {
		t.Fatalf("Submit() before Close = (%d, %v), want (42, nil)", got, err)
	}
	b.Close()

	got, err = b.Submit(context.Background(), 24)

	if !errors.Is(err, batch.ErrClosed) {
		t.Errorf("Submit() error = %v, want ErrClosed", err)
	}
	if got != 0 {
		t.Errorf("Submit() value = %d, want 0", got)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("flusher called %d times, want 1", got)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	b := batch.New(1, time.Second, func(_ context.Context, items []int) ([]int, error) {
		return items, nil
	})

	b.Close()
	b.Close()
}

func TestNewWithNonPositiveMaxSize(t *testing.T) {
	tests := []struct {
		name    string
		maxSize int
	}{
		{name: "zero", maxSize: 0},
		{name: "negative", maxSize: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				b := batch.New(tt.maxSize, time.Hour, func(_ context.Context, items []int) ([]int, error) {
					if len(items) != 1 {
						t.Errorf("batch size = %d, want 1", len(items))
					}
					return items, nil
				})
				defer b.Close()

				start := time.Now()
				got, err := b.Submit(context.Background(), 42)
				if err != nil || got != 42 {
					t.Fatalf("Submit() = (%d, %v), want (42, nil)", got, err)
				}
				if elapsed := time.Since(start); elapsed != 0 {
					t.Errorf("Submit() took %v of virtual time, want 0", elapsed)
				}
			})
		})
	}
}

func TestFlusherResultCountMismatch(t *testing.T) {
	b := batch.New(1, time.Second, func(_ context.Context, items []int) ([]int, error) {
		return []int{1, 2}, nil
	})
	defer b.Close()

	got, err := b.Submit(context.Background(), 42)
	if !errors.Is(err, batch.ErrResultCountMismatch) {
		t.Errorf("Submit() error = %v, want ErrResultCountMismatch", err)
	}
	if got != 0 {
		t.Errorf("Submit() value = %d, want 0", got)
	}
}

func TestFlusherError(t *testing.T) {
	tests := []struct {
		name string
		outs []int
	}{
		{name: "no results"},
		{name: "partial results", outs: []int{10}},
		{name: "full results", outs: []int{10, 20, 30}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const batchSize = 3
				errFlusher := errors.New("flusher failed")
				var calls atomic.Int32
				b := batch.New(batchSize, time.Hour, func(_ context.Context, items []int) ([]int, error) {
					calls.Add(1)
					if len(items) != batchSize {
						t.Errorf("batch size = %d, want %d", len(items), batchSize)
					}
					return tt.outs, errFlusher
				})
				defer b.Close()

				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				var wg sync.WaitGroup
				for i := range batchSize {
					wg.Go(func() {
						got, err := b.Submit(ctx, i)
						if !errors.Is(err, errFlusher) {
							t.Errorf("Submit(%d) error = %v, want %v", i, err, errFlusher)
						}
						if got != 0 {
							t.Errorf("Submit(%d) value = %d, want 0", i, got)
						}
					})
				}
				wg.Wait()

				if got := calls.Load(); got != 1 {
					t.Errorf("flusher called %d times, want 1", got)
				}
			})
		})
	}
}
