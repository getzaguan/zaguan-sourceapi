// Package sourceapi is the Go binding of the Zaguán Source API.
//
// A provider (Tuya, Zigbee2MQTT, a camera server, ...) uses it to publish a
// snapshot, state events and heartbeats to a hub, and to receive commands
// through the hub's long poll. The token is created by the hub admin (Sources
// page) and is bound to one source.
//
// The client is shared: every provider imports it, so the contract is
// implemented once. It lives in its own repository
// (github.com/matiasdelellis/zaguan-sourceapi); the contract itself is in
// docs/source-api.md of the Zaguán repository.
//
//	hub, err := sourceapi.New(sourceapi.Config{
//		BaseURL: "http://127.0.0.1:8081",
//		Token:   token,
//	})
//	if err != nil {
//		return err
//	}
//	// Publish what the provider knows.
//	err = hub.Snapshot(ctx, sourceapi.Snapshot{
//		Version: 1,
//		Devices: []sourceapi.Device{{UniqueID: "cam-1", Name: "Front door"}},
//		Entities: []sourceapi.Entity{{
//			UniqueID:       "cam-1/camera",
//			DeviceUniqueID: "cam-1",
//			Domain:         "camera",
//			Name:           "Front door",
//			State:          "idle",
//			Commands:       []string{"stream_start", "stream_stop", "snapshot"},
//		}},
//	})
package sourceapi

import "time"

// Device is one physical device in a source snapshot.
type Device struct {
	UniqueID     string         `json:"unique_id"`
	Name         string         `json:"name,omitempty"`
	Manufacturer string         `json:"manufacturer,omitempty"`
	Model        string         `json:"model,omitempty"`
	SWVersion    string         `json:"sw_version,omitempty"`
	HWVersion    string         `json:"hw_version,omitempty"`
	AreaID       string         `json:"area_id,omitempty"`
	Labels       []string       `json:"labels,omitempty"`
	Identifiers  []string       `json:"identifiers,omitempty"`
	Connections  []string       `json:"connections,omitempty"`
	Disabled     bool           `json:"disabled,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// Entity is one capability in a source snapshot.
type Entity struct {
	UniqueID       string         `json:"unique_id"`
	DeviceUniqueID string         `json:"device_unique_id"`
	Domain         string         `json:"domain"`
	DeviceClass    string         `json:"device_class,omitempty"`
	Name           string         `json:"name,omitempty"`
	State          any            `json:"state,omitempty"`
	Attributes     map[string]any `json:"attributes,omitempty"`
	Capabilities   []string       `json:"capabilities,omitempty"`
	Commands       []string       `json:"commands,omitempty"`
	Unit           string         `json:"unit,omitempty"`
	StateClass     string         `json:"state_class,omitempty"`
	Category       string         `json:"category,omitempty"`
	Assumed        bool           `json:"assumed,omitempty"`
	Available      *bool          `json:"available,omitempty"`
	Hidden         bool           `json:"hidden,omitempty"`
	Disabled       bool           `json:"disabled,omitempty"`
	LastUpdated    time.Time      `json:"last_updated,omitempty"`
}

// Area is a room hint.
type Area struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Snapshot is the full state sent on connect. Version must increase.
type Snapshot struct {
	Version  uint64   `json:"version"`
	Areas    []Area   `json:"areas,omitempty"`
	Devices  []Device `json:"devices,omitempty"`
	Entities []Entity `json:"entities,omitempty"`
}

// Event is one delta. Type is "state", "availability" or "entity_removed".
type Event struct {
	Type           string         `json:"type"`
	EntityUniqueID string         `json:"entity_unique_id"`
	State          any            `json:"state,omitempty"`
	Attributes     map[string]any `json:"attributes,omitempty"`
	Available      *bool          `json:"available,omitempty"`
	Assumed        bool           `json:"assumed,omitempty"`
	At             time.Time      `json:"at,omitempty"`
}

// Events is a batch. Sequence must increase per source.
type Events struct {
	Sequence uint64  `json:"sequence"`
	Events   []Event `json:"events"`
}

// EventsResponse is the hub's batch result.
type EventsResponse struct {
	OK       bool   `json:"ok"`
	Accepted int    `json:"accepted"`
	Skipped  int    `json:"skipped"`
	Ignored  bool   `json:"ignored"`
	Sequence uint64 `json:"sequence"`
}

// HeartbeatResponse keeps the source lease alive.
type HeartbeatResponse struct {
	OK           bool `json:"ok"`
	Online       bool `json:"online"`
	LeaseSeconds int  `json:"lease_seconds"`
}

// Command is one command delivered by the hub long poll.
type Command struct {
	ID             string         `json:"id"`
	EntityID       string         `json:"entity_id"`
	EntityUniqueID string         `json:"entity_unique_id"`
	Command        string         `json:"command"`
	Args           map[string]any `json:"args,omitempty"`
	ExpiresAt      time.Time      `json:"expires_at"`
}

// CommandResult is the source's answer to a command.
type CommandResult struct {
	OK         bool           `json:"ok"`
	Error      string         `json:"error,omitempty"`
	State      any            `json:"state,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
	Assumed    bool           `json:"assumed,omitempty"`
}

// Meta updates the source display metadata.
type Meta struct {
	Name    string `json:"name,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Version string `json:"version,omitempty"`
}
