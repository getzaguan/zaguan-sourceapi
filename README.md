# zaguan-sourceapi

The Go client of the [Zaguán](https://github.com/matiasdelellis/zaguan)
Source API.

A **provider** (Tuya, Zigbee2MQTT, a camera server, ...) publishes its
devices, state and availability to a Zaguán **hub**, and receives commands
through a long poll. This package is the binding of that contract, so every
provider speaks it the same way instead of copying the HTTP calls.

The hub is part of the Zaguán project; the contract is documented in
`docs/source-api.md` of that repository.

## Install

```sh
go get github.com/getzaguan/zaguan-sourceapi
```

## Use

```go
import "github.com/getzaguan/zaguan-sourceapi"

hub, err := sourceapi.New(sourceapi.Config{
	BaseURL: "http://127.0.0.1:8081",
	Token:   token, // created in the hub admin, Sources page
})
if err != nil {
	return err
}
```

Publish what the provider knows:

```go
err = hub.Snapshot(ctx, sourceapi.Snapshot{
	Version: 1,
	Devices: []sourceapi.Device{{UniqueID: "cam-1", Name: "Front door"}},
	Entities: []sourceapi.Entity{{
		UniqueID:       "cam-1/camera",
		DeviceUniqueID: "cam-1",
		Domain:         "camera",
		Name:           "Front door",
		State:          "idle",
		Commands:       []string{"stream_start", "stream_stop", "snapshot"},
	}},
})
```

Then keep the lease alive and pick up the commands:

```go
for {
	if err := hub.Heartbeat(ctx); err != nil {
		return err
	}
	commands, err := hub.Commands(ctx, 25*time.Second)
	if err != nil {
		return err
	}
	for _, cmd := range commands {
		// Do the work, then answer.
		err := hub.Result(ctx, cmd.ID, sourceapi.CommandResult{OK: true})
	}
}
```

### The link publisher

A provider that publishes snapshots and events should use `sourceapi.NewPublisher`
instead of calling `Snapshot` and `Events` directly. It owns the snapshot
version, the event sequence and the lock that keeps a snapshot and an event
batch from crossing each other:

```go
link := sourceapi.NewPublisher(hub)
version, err := link.Snapshot(ctx, snap)
resp, err := link.Events(ctx, []sourceapi.Event{{
	Type:           "state",
	EntityUniqueID: "cam-1/camera",
	State:          "idle",
}})
```

The provider still owns its loop, retries, heartbeat and command polling; the
publisher only makes the wire ordering the same in every provider. A
provider that changes its hub calls `link.SetClient(newClient)`.

Provider tests can use `sourceapi/sourceapitest`, a fake hub that versions
snapshots, resets and checks event sequences, holds commands for the long
poll and records what the provider sent. It is the shared fixture of the
Tuya and Zigbee2MQTT provider tests.

### The hub lease

The heartbeat and the command long poll are the same in every provider, so
they live in `sourceapi.NewLease`:

```go
lease := sourceapi.NewLease(sourceapi.LeaseConfig{
	Client:        hub,
	HandleCommand: func(ctx context.Context, cmd sourceapi.Command) sourceapi.CommandResult {
		// Do the work.
		return sourceapi.CommandResult{OK: true}
	},
	OnHeartbeat: func(ctx context.Context, beat sourceapi.HeartbeatResponse) {
		// Compare beat.Devices with what the source published.
	},
})
go lease.Run(ctx)
```

The provider still owns its model and the meaning of a command. The lease
owns the long poll, the retry backoff, the heartbeat and the answer to the
hub. Its `OnRecovered` callback fires once when the connection works again
after a failure: that is where the provider restores the availability of the
entities the hub marked offline.

The snapshot schedule is in `sourceapi.NewSnapshotLoop`:

```go
snapshots := sourceapi.NewSnapshotLoop(sourceapi.SnapshotLoopConfig{
	Publish:  func(ctx context.Context) error { return publish(ctx) },
	Every:    5 * time.Minute, // zero: only on request
	RetryMin: time.Second,
	RetryMax: time.Minute,
})
go snapshots.Run(ctx)
snapshots.Request() // an inventory, a report, or the hub lost its state
```

The loop coalesces requests, republishes on the interval and retries with
backoff. `RetryPolicy` is where Tuya distinguishes a cloud failure (retry
soon) from a hub failure (the hub loop owns it).

`sourceapi.NewSourceRuntime` composes both shared pieces for one connection:

```go
runtime := sourceapi.NewSourceRuntime(sourceapi.SourceRuntimeConfig{
	Lease:     leaseConfig,
	Snapshots: snapshotConfig,
})
go runtime.Run(ctx)
runtime.RequestSnapshot()
```

The token identifies the source: it is bound to one source of one hub, and
a source only ever publishes its own devices and receives its own commands.

## The entity type catalog

The `catalog` package is the vocabulary of the model: for every domain, how
its state looks like, which device classes it has, which capabilities it
supports and the commands a client can send.

```go
import "github.com/getzaguan/zaguan-sourceapi/catalog"

light, ok := catalog.Lookup(catalog.DomainLight)
light.Commands   // turn_on, turn_off, toggle, set_brightness, ...
```

A provider publishes the domain, the device classes it uses, its
capabilities and its commands; the hub renders the entity from the catalog
and **refuses a domain it does not know**, so an invented name does not turn
into a device nobody can control. The hub serves the same catalog at
`GET /api/v1/entity-types`.

The `commands` of an entity are part of the contract: an omitted or `null`
list keeps the legacy catalog fallback, while an explicit empty list means
that the entity is read-only. New providers declare the commands they really
support.

## Dependencies

None: the standard library only.

## License

MIT, see [LICENSE](LICENSE).
