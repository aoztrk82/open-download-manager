package engine

import (
	"context"

	"golang.org/x/time/rate"
)

// limiters are the speed limits one download obeys: its own and the one all
// downloads share. Either may be missing.
type limiters []*rate.Limiter

// NewSharedLimiter makes the limiter for all downloads together; SetShared
// changes its rate later. bytesPerSec 0 = unlimited.
func NewSharedLimiter(bytesPerSec int64) *rate.Limiter {
	l := rate.NewLimiter(rate.Inf, 0)
	SetShared(l, bytesPerSec)
	return l
}

// SetShared changes a shared limiter's rate; running downloads follow at once.
func SetShared(l *rate.Limiter, bytesPerSec int64) {
	if bytesPerSec <= 0 {
		l.SetLimit(rate.Inf)
		return
	}
	l.SetBurst(burstFor(bytesPerSec))
	l.SetLimit(rate.Limit(bytesPerSec))
}

func burstFor(bytesPerSec int64) int { return int(max(bytesPerSec, 32<<10)) }

func (o Options) limiters() limiters {
	var l limiters
	if o.RateLimit > 0 {
		l = append(l, rate.NewLimiter(rate.Limit(o.RateLimit), burstFor(o.RateLimit)))
	}
	if o.Shared != nil {
		l = append(l, o.Shared)
	}
	return l
}

// wait blocks until n bytes may pass every limiter. Reads can be larger than
// a limiter's burst (WaitN refuses those), so big reads wait in steps.
func (ls limiters) wait(ctx context.Context, n int) error {
	for _, l := range ls {
		if l.Limit() == rate.Inf {
			continue
		}
		for left := n; left > 0; {
			k := min(left, max(l.Burst(), 1))
			if err := l.WaitN(ctx, k); err != nil {
				if ctx.Err() == nil && k > l.Burst() {
					continue // the limit was lowered meanwhile: retry with the new burst
				}
				return err
			}
			left -= k
		}
	}
	return nil
}
