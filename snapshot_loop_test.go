package sourceapi_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getzaguan/zaguan-sourceapi"
)

// TestSnapshotLoopCoalescesRequests checks that two requests that arrive
// while a snapshot is in flight produce one more snapshot, not two.
func TestSnapshotLoopCoalescesRequests(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	loop := sourceapi.NewSnapshotLoop(sourceapi.SnapshotLoopConfig{
		Initial: true,
		Publish: func(context.Context) error {
			n := calls.Add(1)
			started <- struct{}{}
			if n == 1 {
				<-release
			}
			return nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.Run(ctx)

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the first snapshot did not start")
	}
	loop.Request()
	loop.Request()
	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond)
	if got := calls.Load(); got != 2 {
		t.Fatalf("the loop published %d snapshots, want 2", got)
	}
}

// TestSnapshotLoopRetries checks the backoff after a failed snapshot.
func TestSnapshotLoopRetries(t *testing.T) {
	var calls atomic.Int32
	var failures atomic.Int32
	loop := sourceapi.NewSnapshotLoop(sourceapi.SnapshotLoopConfig{
		Initial:  true,
		RetryMin: 2 * time.Millisecond,
		RetryMax: 5 * time.Millisecond,
		Publish: func(context.Context) error {
			if calls.Add(1) < 3 {
				return errors.New("not yet")
			}
			return nil
		},
		OnError: func(context.Context, error) {
			failures.Add(1)
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls.Load() < 3 {
		t.Fatalf("the loop only tried %d times", calls.Load())
	}
	if failures.Load() != 2 {
		t.Fatalf("the loop reported %d failures, want 2", failures.Load())
	}
}
