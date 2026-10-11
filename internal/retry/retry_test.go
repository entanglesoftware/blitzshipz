package retry

import (
	"context"
	"testing"
	"time"
)

func TestBackoff_WithinBounds(t *testing.T) {
	// Full jitter means the delay is in [0, cap] for each attempt, and the cap
	// grows exponentially up to maxDelay.
	for attempt := 1; attempt <= 10; attempt++ {
		want := min(baseDelay<<(attempt-1), maxDelay)
		for range 100 {
			got := Backoff(attempt)
			if got < 0 || got > want {
				t.Fatalf("attempt %d: Backoff = %v, want within [0, %v]", attempt, got, want)
			}
		}
	}
}

func TestBackoff_ClampsLowAttempts(t *testing.T) {
	// attempt < 1 must not panic (no negative shift) and stays within the base.
	for _, a := range []int{0, -5} {
		got := Backoff(a)
		if got < 0 || got > baseDelay {
			t.Errorf("Backoff(%d) = %v, want within [0, %v]", a, got, baseDelay)
		}
	}
}

func TestSleep_CompletesAfterDelay(t *testing.T) {
	start := time.Now()
	if err := Sleep(context.Background(), 20*time.Millisecond); err != nil {
		t.Fatalf("Sleep returned %v", err)
	}
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Errorf("Sleep returned too early after %v", elapsed)
	}
}

func TestSleep_HonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Sleep(ctx, time.Hour); err != context.Canceled {
		t.Fatalf("Sleep = %v, want context.Canceled", err)
	}
}

func TestSleep_ZeroDelay(t *testing.T) {
	if err := Sleep(context.Background(), 0); err != nil {
		t.Errorf("Sleep(0) = %v, want nil", err)
	}
}
