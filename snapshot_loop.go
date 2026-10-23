// The snapshot loop.
package sourceapi

import (
	"context"
	"log/slog"
	"time"
)

// SnapshotPolicy decides what to do after a snapshot failed. It returns the
// delay before the retry and whether the snapshot should be retried.
type SnapshotPolicy func(error) (time.Duration, bool)

// SnapshotLoopConfig configures the publication loop of one source.
//
// The loop does not build the snapshot: Publish does. The provider owns the
// model and the hub client; the loop owns when to try, how to coalesce
// requests and how to back off after a failure.
type SnapshotLoopConfig struct {
	Logger  *slog.Logger
	Publish func(context.Context) error

	// Initial publishes as soon as the loop starts.
	Initial bool
	// FirstDelay is how long to wait before the first publication when
	// Initial is false. Zero with Every > 0 waits Every.
	FirstDelay time.Duration
	// Every republishes periodically. Zero means only on request.
	Every time.Duration

	// RetryMin and RetryMax are the exponential backoff. RetryMin zero
	// means no retry unless RetryPolicy says otherwise.
	RetryMin time.Duration
	RetryMax time.Duration
	// RetryPolicy overrides the backoff per error. It is what a provider
	// uses to retry a cloud failure but leave a hub failure to its hub
	// loop.
	RetryPolicy SnapshotPolicy

	OnError   func(context.Context, error)
	OnSuccess func(context.Context)
}

// SnapshotLoop publishes snapshots on request, on a timer, and after a
// failure.
type SnapshotLoop struct {
	cfg      SnapshotLoopConfig
	requests chan struct{}
}

// NewSnapshotLoop returns a snapshot loop.
func NewSnapshotLoop(cfg SnapshotLoopConfig) *SnapshotLoop {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &SnapshotLoop{cfg: cfg, requests: make(chan struct{}, 1)}
}

// Request asks for a snapshot. Requests are coalesced: a snapshot that is
// already in flight makes the next one happen after it, once.
func (s *SnapshotLoop) Request() {
	select {
	case s.requests <- struct{}{}:
	default:
	}
}

// Run publishes until ctx ends.
func (s *SnapshotLoop) Run(ctx context.Context) {
	var timer *time.Timer
	var timerC <-chan time.Time
	pending := s.cfg.Initial
	backoff := s.cfg.RetryMin

	if !pending {
		switch {
		case s.cfg.FirstDelay > 0:
			timer = time.NewTimer(s.cfg.FirstDelay)
			timerC = timer.C
		case s.cfg.Every > 0:
			timer = time.NewTimer(s.cfg.Every)
			timerC = timer.C
		}
	}

	for {
		if !pending {
			select {
			case <-ctx.Done():
				stopTimer(timer)
				return
			case <-s.requests:
				pending = true
			case <-timerC:
				pending = true
			}
			stopTimer(timer)
			timer, timerC = nil, nil
		}
		if ctx.Err() != nil {
			return
		}

		// A request that arrived while this one was being prepared is the
		// same snapshot: its state will be in this build.
		select {
		case <-s.requests:
		default:
		}

		err := s.cfg.Publish(ctx)
		if err == nil {
			pending = false
			backoff = s.cfg.RetryMin
			if s.cfg.OnSuccess != nil {
				s.cfg.OnSuccess(ctx)
			}
			if s.cfg.Every > 0 {
				timer = time.NewTimer(s.cfg.Every)
				timerC = timer.C
			}
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if s.cfg.OnError != nil {
			s.cfg.OnError(ctx, err)
		}

		delay, retry := backoff, s.cfg.RetryMin > 0
		if s.cfg.RetryPolicy != nil {
			delay, retry = s.cfg.RetryPolicy(err)
		}
		if !retry {
			pending = false
			if s.cfg.Every > 0 {
				timer = time.NewTimer(s.cfg.Every)
				timerC = timer.C
			}
			continue
		}
		if delay <= 0 {
			delay = backoff
		}
		if delay <= 0 {
			delay = time.Second
		}
		if !waitContext(ctx, delay) {
			return
		}
		if s.cfg.RetryMax > 0 {
			backoff = min(backoff*2, s.cfg.RetryMax)
		}
		// pending stays true: the same snapshot is tried again.
	}
}

func stopTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
