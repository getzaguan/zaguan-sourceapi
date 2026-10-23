package sourceapi_test

import (
	"context"
	"testing"
	"time"

	"github.com/getzaguan/zaguan-sourceapi"
	"github.com/getzaguan/zaguan-sourceapi/sourceapitest"
)

// TestLeaseSignalsRecovery checks that the lease tells the provider when a
// failed connection works again: that is when availability is restored.
func TestLeaseSignalsRecovery(t *testing.T) {
	hub := sourceapitest.NewHub(t)
	hub.FailHeartbeats(1)
	client := hub.Client(t)
	recovered := make(chan struct{}, 1)
	failed := make(chan struct{}, 1)
	lease := sourceapi.NewLease(sourceapi.LeaseConfig{
		Client:            client,
		HeartbeatInterval: 20 * time.Millisecond,
		CommandWait:       time.Second,
		OnHeartbeatError: func(context.Context, error) {
			select {
			case failed <- struct{}{}:
			default:
			}
		},
		OnRecovered: func(context.Context) {
			select {
			case recovered <- struct{}{}:
			default:
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		lease.Run(ctx)
		close(done)
	}()
	select {
	case <-failed:
	case <-time.After(2 * time.Second):
		t.Fatal("the lease did not see the failed heartbeat")
	}
	select {
	case <-recovered:
	case <-time.After(2 * time.Second):
		t.Fatal("the lease did not signal the recovery")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the lease did not stop")
	}
}

// TestLeaseKeepsTheLeaseAndAnswersCommands checks the two things every
// provider shares: the heartbeat and the command long poll.
func TestLeaseKeepsTheLeaseAndAnswersCommands(t *testing.T) {
	hub := sourceapitest.NewHub(t)
	client := hub.Client(t)
	hub.Queue(sourceapi.Command{
		ID: "cmd-1", EntityUniqueID: "lamp:switch", Command: "turn_on",
	})

	beats := make(chan sourceapi.HeartbeatResponse, 4)
	lease := sourceapi.NewLease(sourceapi.LeaseConfig{
		Client:            client,
		HeartbeatInterval: 20 * time.Millisecond,
		CommandWait:       time.Second,
		HandleCommand: func(context.Context, sourceapi.Command) sourceapi.CommandResult {
			return sourceapi.CommandResult{OK: true, State: true}
		},
		OnHeartbeat: func(_ context.Context, resp sourceapi.HeartbeatResponse) {
			select {
			case beats <- resp:
			default:
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		lease.Run(ctx)
		close(done)
	}()

	select {
	case <-beats:
	case <-time.After(2 * time.Second):
		t.Fatal("the lease did not send a heartbeat")
	}
	select {
	case answer := <-hub.ResultFeed():
		if answer.ID != "cmd-1" || !answer.Result.OK || answer.Result.State != true {
			t.Fatalf("the answer = %+v", answer)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the lease did not answer the command")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the lease did not stop")
	}
}
