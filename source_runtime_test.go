package sourceapi_test

import (
	"context"
	"testing"
	"time"

	"github.com/getzaguan/zaguan-sourceapi"
	"github.com/getzaguan/zaguan-sourceapi/sourceapitest"
)

// TestSourceRuntimeRunsTheLeaseAndTheSnapshots checks the composition: one
// runtime publishes the first snapshot and keeps the command channel alive.
func TestSourceRuntimeRunsTheLeaseAndTheSnapshots(t *testing.T) {
	hub := sourceapitest.NewHub(t)
	client := hub.Client(t)
	publisher := sourceapi.NewPublisher(client)
	hub.Queue(sourceapi.Command{
		ID: "cmd-1", EntityUniqueID: "lamp:switch", Command: "turn_on",
	})

	runtime := sourceapi.NewSourceRuntime(sourceapi.SourceRuntimeConfig{
		Lease: sourceapi.LeaseConfig{
			Client:            client,
			HeartbeatInterval: 20 * time.Millisecond,
			CommandWait:       time.Second,
			HandleCommand: func(context.Context, sourceapi.Command) sourceapi.CommandResult {
				return sourceapi.CommandResult{OK: true, State: true}
			},
		},
		Snapshots: sourceapi.SnapshotLoopConfig{
			Initial: true,
			Publish: func(ctx context.Context) error {
				_, err := publisher.Snapshot(ctx, sourceapi.Snapshot{
					Devices: []sourceapi.Device{{UniqueID: "lamp"}},
				})
				return err
			},
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runtime.Run(ctx)
		close(done)
	}()

	select {
	case <-hub.SnapshotFeed():
	case <-time.After(2 * time.Second):
		t.Fatal("the runtime did not publish the snapshot")
	}
	select {
	case answer := <-hub.ResultFeed():
		if answer.ID != "cmd-1" || !answer.Result.OK {
			t.Fatalf("the answer = %+v", answer)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the runtime did not answer the command")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the runtime did not stop")
	}
}
