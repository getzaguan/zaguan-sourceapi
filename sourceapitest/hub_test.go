package sourceapitest_test

import (
	"context"
	"testing"
	"time"

	"github.com/getzaguan/zaguan-sourceapi"
	"github.com/getzaguan/zaguan-sourceapi/sourceapitest"
)

// TestHubSpeaksTheSourceContract checks the parts every provider tests
// against: snapshots get a version, events a sequence, and the long poll
// hands out queued commands.
func TestHubSpeaksTheSourceContract(t *testing.T) {
	hub := sourceapitest.NewHub(t)
	client := hub.Client(t)
	publisher := sourceapi.NewPublisher(client)

	version, err := publisher.Snapshot(context.Background(), sourceapi.Snapshot{
		Devices: []sourceapi.Device{{UniqueID: "d1", Name: "Lamp"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if version == 0 {
		t.Fatal("the snapshot has no version")
	}
	if uint64(hub.LastSnapshot().Version) != version {
		t.Fatalf("the hub took version %d, want %d", hub.LastSnapshot().Version, version)
	}
	if _, err := publisher.Events(context.Background(), []sourceapi.Event{{
		Type: "state", EntityUniqueID: "d1:switch", State: true,
	}}); err != nil {
		t.Fatal(err)
	}
	if events := hub.AllEvents(); len(events) != 1 || events[0].State != true {
		t.Fatalf("the hub events = %+v", events)
	}

	hub.Queue(sourceapi.Command{ID: "cmd-1", EntityUniqueID: "d1:switch", Command: "turn_on"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	commands, err := client.Commands(ctx, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0].ID != "cmd-1" {
		t.Fatalf("the commands = %+v", commands)
	}
	if err := client.Result(ctx, "cmd-1", sourceapi.CommandResult{OK: true, State: true}); err != nil {
		t.Fatal(err)
	}
	result, ok := hub.Result("cmd-1")
	if !ok || !result.OK || result.State != true {
		t.Fatalf("the result = %+v, %v", result, ok)
	}
}
