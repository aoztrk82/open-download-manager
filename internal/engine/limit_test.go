package engine

import (
	"context"
	"testing"
	"time"
)

func TestSharedLimiter(t *testing.T) {
	ctx := context.Background()
	shared := NewSharedLimiter(0)
	ls := Options{Shared: shared}.limiters()
	start := time.Now()
	if err := ls.wait(ctx, 10<<20); err != nil || time.Since(start) > 100*time.Millisecond {
		t.Fatalf("unlimited: err=%v after %v", err, time.Since(start))
	}

	// 64 KiB/s, burst 64 KiB. A read bigger than the burst must wait in steps,
	// not fail. 96 KiB takes ~1.5 s (plus up to 1 s if the bucket starts empty).
	SetShared(shared, 64<<10)
	start = time.Now()
	if err := ls.wait(ctx, 96<<10); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el < 400*time.Millisecond || el > 3500*time.Millisecond {
		t.Fatalf("96 KiB at 64 KiB/s took %v", el)
	}

	// Own limit + shared: both apply.
	both := Options{RateLimit: 32 << 10, Shared: shared}.limiters()
	if len(both) != 2 {
		t.Fatalf("limiters: %d", len(both))
	}
	SetShared(shared, 0)
	if err := ls.wait(ctx, 1<<30); err != nil {
		t.Fatal(err)
	}
}
