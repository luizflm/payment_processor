package memcache_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/luizflm/payment-processor/pkg/memcache"
)

func TestSetGet(t *testing.T) {
	c := memcache.New[string](time.Minute, time.Hour)
	defer c.Close()

	c.Set("k", "v")

	got, ok, err := c.Get("k")
	if err != nil {
		t.Fatalf("Get(%q): %v", "k", err)
	}
	if got != "v" || !ok {
		t.Fatalf("Get(%q) = (%q, %t), want (%q, true)", "k", got, ok, "v")
	}
}

func TestIncrementIsAtomic(t *testing.T) {
	c := memcache.New[int](time.Minute, time.Hour)
	defer c.Close()

	add := func(current int, exists bool) int {
		if !exists {
			return 1
		}
		return current + 1
	}

	var wg sync.WaitGroup
	wg.Add(100)
	for range 100 {
		go func() {
			defer wg.Done()
			c.Increment("card:123", add)
		}()
	}
	wg.Wait()

	got, ok, err := c.Get("card:123")
	if err != nil {
		t.Fatalf("Get(%q): %v", "card:123", err)
	}
	if got != 100 || !ok {
		t.Fatalf("Get(%q) = (%d, %t), want (100, true)", "card:123", got, ok)
	}
}

func TestExpiration(t *testing.T) {
	c := memcache.New[int](10*time.Millisecond, time.Hour)
	defer c.Close()

	c.Set("k", 42)
	time.Sleep(30 * time.Millisecond)

	got, ok, err := c.Get("k")
	if err != nil {
		t.Fatalf("Get(%q): %v", "k", err)
	}
	if ok {
		t.Fatalf("Get(%q) = (%d, true), want ok=false", "k", got)
	}
}

func TestJanitorRevictsExpired(t *testing.T) {
	c := memcache.New[int](10*time.Millisecond, 5*time.Millisecond)
	defer c.Close()

	for i := range 256 {
		key := fmt.Sprintf("key-%d", i)
		if err := c.Set(key, i); err != nil {
			t.Fatalf("Set(%q): %v", key, err)
		}
	}

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && c.Len() != 0 {
		<-ticker.C
	}

	if got := c.Len(); got != 0 {
		t.Fatalf("Len() = %d, want 0 after janitor sweep", got)
	}
}

func TestConcurrentAccessAcrossShards(t *testing.T) {
	c := memcache.New[int](time.Minute, time.Hour)
	defer c.Close()

	var wg sync.WaitGroup
	wg.Add(200)
	for i := range 200 {
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", i%50)
			c.Set(key, i)
			c.Get(key)
		}()
	}
	wg.Wait()
}

func TestCloseError(t *testing.T) {
	c := memcache.New[int](time.Minute, time.Minute)
	if err := c.Close(); err != nil {
		t.Errorf("Close() returned unexpected error: %v", err)
	}
	if err := c.Close(); !errors.Is(err, memcache.ErrClosed) {
		t.Errorf("expected ErrClosed, got %v", err)
	}
}

func TestInvalidKeyError(t *testing.T) {
	c := memcache.New[int](time.Minute, time.Minute)
	defer c.Close()

	if err := c.Set("", 1); !errors.Is(err, memcache.ErrInvalidKey) {
		t.Errorf("expected ErrInvalidKey on set, got %v", err)
	}
	if _, _, err := c.Get(""); !errors.Is(err, memcache.ErrInvalidKey) {
		t.Errorf("expected ErrInvalidKey on get, got %v", err)
	}
	if _, err := c.Increment("", func(c int, e bool) int { return 1 }); !errors.Is(err, memcache.ErrInvalidKey) {
		t.Errorf("expected ErrInvalidKey on increment, got %v", err)
	}
}

func TestClosedCacheError(t *testing.T) {
	c := memcache.New[int](time.Minute, time.Minute)
	c.Close()

	if err := c.Set("k", 1); !errors.Is(err, memcache.ErrClosed) {
		t.Errorf("expected ErrClosed on set, got %v", err)
	}
	if _, _, err := c.Get("k"); !errors.Is(err, memcache.ErrClosed) {
		t.Errorf("expected ErrClosed on get, got %v", err)
	}
	if _, err := c.Increment("k", func(c int, e bool) int { return 1 }); !errors.Is(err, memcache.ErrClosed) {
		t.Errorf("expected ErrClosed on increment, got %v", err)
	}
}
