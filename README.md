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
go get github.com/matiasdelellis/zaguan-sourceapi
```

## Use

```go
import "github.com/matiasdelellis/zaguan-sourceapi"

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

The token identifies the source: it is bound to one source of one hub, and
a source only ever publishes its own devices and receives its own commands.

## Domains

The domain of an entity says what it can do (`light`, `sensor`, `camera`,
`lock`, ...). The hub has the catalog of them
(`GET /api/v1/entity-types`); a provider publishes the domain, the device
classes it uses, its capabilities and the commands it accepts.

## Dependencies

None: the standard library only.

## License

MIT, see [LICENSE](LICENSE).
