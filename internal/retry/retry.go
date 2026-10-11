// Package retry provides the backoff and context-aware sleep primitives used
// by the blitzshipz client's transport layer. It is internal and not part of the
// public API.
package retry

import (
	"context"
	"math/rand/v2"
	"time"
)

const (
	// baseDelay is the backoff for the first retry.
	baseDelay = 200 * time.Millisecond
	// maxDelay caps the exponential growth of the backoff.
	maxDelay = 8 * time.Second
)

// Backoff returns the delay before the given (1-based) retry attempt, using
// exponential growth with full jitter, capped at maxDelay.
func Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := min(baseDelay<<(attempt-1), maxDelay)
	// Full jitter: a random duration in [0, d].
	return time.Duration(rand.Int64N(int64(d) + 1))
}

// Sleep pauses for d or until ctx is done, whichever comes first. It returns
// ctx.Err() if the context is cancelled before the delay elapses.
func Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
