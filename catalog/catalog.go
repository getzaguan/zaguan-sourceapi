// Package catalog is the vocabulary of the Zaguán model: for every domain,
// how its state looks like, which device classes it has, which capabilities
// it supports and the commands a client can send.
//
// It lives in the contract repository so that a provider and a hub speak the
// same words: a source publishes a domain and the hub renders the entity
// from what the catalog says. The hub checks what a source publishes against
// this catalog, so an invented domain is refused instead of showing up as a
// device nobody can control.
package catalog

import (
	"fmt"
	"sort"
	"sync"
)

// Domain names used by the initial catalog. New domains may be registered
// without changing the core.
const (
	DomainSwitch            = "switch"
	DomainLight             = "light"
	DomainSensor            = "sensor"
	DomainBinarySensor      = "binary_sensor"
	DomainEvent             = "event"
	DomainCamera            = "camera"
	DomainAlarmControlPanel = "alarm_control_panel"
	DomainClimate           = "climate"
	DomainNumber            = "number"
	DomainSelect            = "select"
	DomainLock              = "lock"
	DomainCover             = "cover"
	DomainFan               = "fan"
	DomainHumidifier        = "humidifier"
	DomainWaterHeater       = "water_heater"
	DomainValve             = "valve"
	DomainVacuum            = "vacuum"
	DomainSiren             = "siren"
	DomainMediaPlayer       = "media_player"
	DomainRemote            = "remote"
	DomainLawnMower         = "lawn_mower"
	DomainButton            = "button"
	DomainDate              = "date"
	DomainTime              = "time"
	DomainText              = "text"
	DomainTodo              = "todo"
	DomainInputBoolean      = "input_boolean"
	DomainInputNumber       = "input_number"
	DomainInputSelect       = "input_select"
	DomainInputText         = "input_text"
	DomainInputDatetime     = "input_datetime"
	DomainWeather           = "weather"
	DomainAirQuality        = "air_quality"
	DomainPerson            = "person"
	DomainDeviceTracker     = "device_tracker"
	DomainSun               = "sun"
	DomainCalendar          = "calendar"
	DomainCounter           = "counter"
)

// State types: how the state of an entity should be read and rendered.
const (
	StateTypeBool   = "bool"
	StateTypeNumber = "number"
	StateTypeText   = "text"
	StateTypeEnum   = "enum"
)

// Argument types: how the value of a command argument is entered.
const (
	ArgTypeNumber = "number"
	ArgTypeText   = "text"
	ArgTypeSelect = "select"
	ArgTypeBool   = "bool"
)

// EntityType describes a domain the hub and clients understand. The catalog
// is the extension point: a new device kind registers its domain, device
// classes, capabilities and commands here; the API and the PWA fall back to
// the catalog instead of hardcoding every device.
type EntityType struct {
	Domain        string        `json:"domain"`
	Description   string        `json:"description,omitempty"`
	StateType     string        `json:"state_type,omitempty"`
	States        []string      `json:"states,omitempty"`
	DeviceClasses []string      `json:"device_classes,omitempty"`
	Capabilities  []string      `json:"capabilities,omitempty"`
	Commands      []CommandSpec `json:"commands,omitempty"`
}

// CommandSpec describes one command of an entity type.
type CommandSpec struct {
	Name string `json:"name"`
	// Capability is the capability a command needs. A client only offers the
	// command when the entity has that capability.
	Capability string    `json:"capability,omitempty"`
	Args       []ArgSpec `json:"args,omitempty"`
	Sensitive  bool      `json:"sensitive,omitempty"`
}

// ArgSpec describes one argument of a command: enough for a client to render
// the right input and for the hub to validate the value.
type ArgSpec struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Step    *float64 `json:"step,omitempty"`
	Options []string `json:"options,omitempty"`
	Unit    string   `json:"unit,omitempty"`
}

// Catalog is a concurrency-safe entity type registry.
type Catalog struct {
	mu    sync.RWMutex
	types map[string]EntityType
}

// NewCatalog returns an empty catalog.
func NewCatalog() *Catalog {
	return &Catalog{types: make(map[string]EntityType)}
}

// Register adds an entity type. Registering the same domain twice is an error.
func (c *Catalog) Register(t EntityType) error {
	if t.Domain == "" {
		return fmt.Errorf("model: empty domain")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.types[t.Domain]; ok {
		return fmt.Errorf("model: domain %q already registered", t.Domain)
	}
	c.types[t.Domain] = t
	return nil
}

// Lookup returns the entity type for a domain.
func (c *Catalog) Lookup(domain string) (EntityType, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.types[domain]
	return t, ok
}

// Domains returns the registered domains in sorted order.
func (c *Catalog) Domains() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.types))
	for d := range c.types {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// Types returns the registered types sorted by domain. It is what a client
// needs to render a device it does not know.
func (c *Catalog) Types() []EntityType {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]EntityType, 0, len(c.types))
	for _, t := range c.types {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out
}

// Command returns the spec of one command of a domain.
func (c *Catalog) Command(domain, command string) (CommandSpec, bool) {
	t, ok := c.Lookup(domain)
	if !ok {
		return CommandSpec{}, false
	}
	for _, cmd := range t.Commands {
		if cmd.Name == command {
			return cmd, true
		}
	}
	return CommandSpec{}, false
}

// numArg builds a numeric argument with a range.
func numArg(name string, min, max float64, unit string) ArgSpec {
	lo, hi := min, max
	return ArgSpec{Name: name, Type: ArgTypeNumber, Min: &lo, Max: &hi, Unit: unit}
}

// textArg builds a free text argument.
func textArg(name string) ArgSpec {
	return ArgSpec{Name: name, Type: ArgTypeText}
}

// selectArg builds an argument with a fixed list of options.
func selectArg(name string, options ...string) ArgSpec {
	return ArgSpec{Name: name, Type: ArgTypeSelect, Options: options}
}

// cmd builds a command spec.
func cmd(name string, args ...ArgSpec) CommandSpec {
	return CommandSpec{Name: name, Args: args}
}

// cmdCap builds a command spec that needs a capability.
func cmdCap(name, capability string, args ...ArgSpec) CommandSpec {
	return CommandSpec{Name: name, Capability: capability, Args: args}
}

// Default returns the initial domains.
//
// The vocabulary follows Home Assistant: a domain says what a device can do,
// its device classes say what kind of sensor it is, and its commands are the
// ones a client can send. A source publishes the domains it supports; a
// client renders whatever the catalog says.
func Default() *Catalog {
	c := NewCatalog()
	must := func(t EntityType) {
		if err := c.Register(t); err != nil {
			panic(err)
		}
	}

	must(EntityType{
		Domain:      DomainSwitch,
		Description: "On/off switch or plug",
		StateType:   StateTypeBool,
		Commands: []CommandSpec{
			cmd("turn_on"),
			cmd("turn_off"),
			cmd("toggle"),
		},
	})

	must(EntityType{
		Domain:       DomainLight,
		Description:  "Dimmable or color light",
		StateType:    StateTypeBool,
		Capabilities: []string{"brightness", "color_temp", "color", "effect"},
		Commands: []CommandSpec{
			cmd("turn_on"),
			cmd("turn_off"),
			cmd("toggle"),
			cmdCap("set_brightness", "brightness", numArg("brightness", 0, 255, "")),
			cmdCap("set_color_temp", "color_temp", numArg("kelvin", 1000, 10000, "K")),
			cmdCap("set_color", "color", textArg("color")),
		},
	})

	must(EntityType{
		Domain:      DomainSensor,
		Description: "Numeric measurement",
		StateType:   StateTypeNumber,
		DeviceClasses: []string{
			"temperature", "humidity", "battery", "illuminance", "pressure",
			"power", "energy", "signal_strength", "voltage", "current", "gas",
			"moisture", "carbon_monoxide", "carbon_dioxide", "nitrogen_dioxide",
			"ozone", "pm1", "pm10", "pm25", "volatile_organic_compounds",
			"wind_speed", "precipitation", "precipitation_intensity", "timestamp",
			"duration", "data_size", "frequency", "apparent_power",
			"reactive_power", "distance", "speed",
		},
	})

	must(EntityType{
		Domain:      DomainBinarySensor,
		Description: "Two-state sensor",
		StateType:   StateTypeBool,
		DeviceClasses: []string{
			"door", "window", "opening", "garage_door", "motion", "occupancy",
			"presence", "smoke", "gas", "moisture", "battery_low", "connectivity",
			"tamper", "vibration", "sound", "problem", "safety", "cold", "heat",
			"power",
		},
	})

	must(EntityType{
		Domain:        DomainEvent,
		Description:   "Stateless event (remote button, doorbell)",
		StateType:     StateTypeText,
		DeviceClasses: []string{"button", "doorbell"},
	})

	must(EntityType{
		Domain:       DomainCamera,
		Description:  "Camera stream and snapshots",
		StateType:    StateTypeEnum,
		States:       []string{"idle", "streaming"},
		Capabilities: []string{"live", "snapshot", "recording", "ptz"},
		Commands: []CommandSpec{
			cmd("snapshot"),
			cmd("stream_start"),
			cmd("stream_stop"),
			cmdCap("ptz_move", "ptz", numArg("pan", -180, 180, "°"), numArg("tilt", -90, 90, "°")),
			cmdCap("ptz_home", "ptz"),
			cmdCap("ptz_recalibrate", "ptz"),
		},
	})

	must(EntityType{
		Domain:       DomainAlarmControlPanel,
		Description:  "Security panel",
		StateType:    StateTypeEnum,
		States:       []string{"disarmed", "arming", "armed_home", "armed_away", "armed_night", "pending", "triggered"},
		Capabilities: []string{"arm_home", "arm_away", "arm_night", "trigger"},
		Commands: []CommandSpec{
			cmd("arm_home"),
			cmd("arm_away"),
			cmd("arm_night"),
			cmd("disarm"),
			cmd("trigger"),
		},
	})

	must(EntityType{
		Domain:       DomainClimate,
		Description:  "Thermostat or heater",
		StateType:    StateTypeEnum,
		States:       []string{"off", "heat", "cool", "auto", "dry", "fan_only"},
		Capabilities: []string{"target_temperature", "current_temperature", "target_humidity", "current_humidity", "preset_mode", "fan_mode"},
		Commands: []CommandSpec{
			cmd("turn_on"),
			cmd("turn_off"),
			cmdCap("set_temperature", "target_temperature", numArg("temperature", -50, 100, "°C")),
			cmdCap("set_humidity", "humidity", numArg("humidity", 0, 100, "%")),
			cmdCap("set_preset_mode", "preset_mode", textArg("preset_mode")),
			cmdCap("set_fan_mode", "fan_mode", textArg("fan_mode")),
		},
	})

	must(EntityType{
		Domain:      DomainNumber,
		Description: "Numeric setting",
		StateType:   StateTypeNumber,
		Commands: []CommandSpec{
			cmd("set_value", numArg("value", -1e9, 1e9, "")),
		},
	})

	must(EntityType{
		Domain:      DomainSelect,
		Description: "Enum setting",
		StateType:   StateTypeEnum,
		Commands: []CommandSpec{
			cmd("select_option", selectArg("option")),
		},
	})

	must(EntityType{
		Domain:      DomainLock,
		Description: "Door or drawer lock",
		StateType:   StateTypeEnum,
		States:      []string{"locked", "unlocked", "locking", "unlocking", "jammed"},
		Commands: []CommandSpec{
			cmd("lock"),
			cmd("unlock"),
		},
	})

	must(EntityType{
		Domain:       DomainCover,
		Description:  "Blind, shutter or garage door",
		StateType:    StateTypeEnum,
		States:       []string{"open", "closed", "opening", "closing", "stopped"},
		Capabilities: []string{"position"},
		Commands: []CommandSpec{
			cmd("open_cover"),
			cmd("close_cover"),
			cmd("stop_cover"),
			cmdCap("set_cover_position", "position", numArg("position", 0, 100, "%")),
		},
	})

	must(EntityType{
		Domain:       DomainFan,
		Description:  "Fan or ventilation",
		StateType:    StateTypeBool,
		Capabilities: []string{"percentage", "preset_mode", "oscillating"},
		Commands: []CommandSpec{
			cmd("turn_on"),
			cmd("turn_off"),
			cmd("toggle"),
			cmdCap("set_percentage", "percentage", numArg("percentage", 0, 100, "%")),
			cmdCap("set_preset_mode", "preset_mode", textArg("preset_mode")),
			cmdCap("oscillate", "oscillating", ArgSpec{Name: "oscillating", Type: ArgTypeBool}),
		},
	})

	must(EntityType{
		Domain:       DomainHumidifier,
		Description:  "Humidifier or dehumidifier",
		StateType:    StateTypeBool,
		Capabilities: []string{"humidity"},
		Commands: []CommandSpec{
			cmd("turn_on"),
			cmd("turn_off"),
			cmdCap("set_humidity", "humidity", numArg("humidity", 0, 100, "%")),
		},
	})

	must(EntityType{
		Domain:       DomainWaterHeater,
		Description:  "Water heater or boiler",
		StateType:    StateTypeBool,
		Capabilities: []string{"temperature"},
		Commands: []CommandSpec{
			cmd("turn_on"),
			cmd("turn_off"),
			cmdCap("set_temperature", "temperature", numArg("temperature", 0, 100, "°C")),
		},
	})

	must(EntityType{
		Domain:      DomainValve,
		Description: "Water or gas valve",
		StateType:   StateTypeEnum,
		States:      []string{"open", "closed"},
		Commands: []CommandSpec{
			cmd("open_valve"),
			cmd("close_valve"),
		},
	})

	must(EntityType{
		Domain:      DomainVacuum,
		Description: "Robot vacuum",
		StateType:   StateTypeEnum,
		States:      []string{"cleaning", "docked", "returning", "paused", "error"},
		Commands: []CommandSpec{
			cmd("start"),
			cmd("pause"),
			cmd("return_to_base"),
			cmd("locate"),
			cmd("set_fan_speed", selectArg("fan_speed", "min", "medium", "max", "turbo")),
		},
	})

	must(EntityType{
		Domain:      DomainSiren,
		Description: "Siren or alarm",
		StateType:   StateTypeBool,
		Commands: []CommandSpec{
			cmd("turn_on"),
			cmd("turn_off"),
		},
	})

	must(EntityType{
		Domain:       DomainMediaPlayer,
		Description:  "Speaker, television or streamer",
		StateType:    StateTypeEnum,
		States:       []string{"off", "on", "playing", "paused", "idle", "buffering", "standby"},
		Capabilities: []string{"volume", "source"},
		Commands: []CommandSpec{
			cmd("turn_on"),
			cmd("turn_off"),
			cmd("volume_up"),
			cmd("volume_down"),
			cmdCap("set_volume", "volume", numArg("volume", 0, 100, "%")),
			cmd("media_play"),
			cmd("media_pause"),
			cmd("media_next"),
			cmd("media_previous"),
			cmdCap("select_source", "source", textArg("source")),
		},
	})

	must(EntityType{
		Domain:      DomainRemote,
		Description: "Infrared or Bluetooth remote",
		StateType:   StateTypeText,
		Commands: []CommandSpec{
			cmd("send_command", textArg("command")),
		},
	})

	must(EntityType{
		Domain:      DomainLawnMower,
		Description: "Robot lawn mower",
		StateType:   StateTypeEnum,
		States:      []string{"mowing", "docked", "paused", "error"},
		Commands: []CommandSpec{
			cmd("start_mowing"),
			cmd("pause_mowing"),
			cmd("return_to_base"),
		},
	})

	must(EntityType{
		Domain:      DomainButton,
		Description: "Push button",
		StateType:   StateTypeEnum,
		States:      []string{"pressed", "released"},
		Commands: []CommandSpec{
			cmd("press"),
		},
	})

	must(EntityType{
		Domain:      DomainDate,
		Description: "Date setting",
		StateType:   StateTypeText,
		Commands: []CommandSpec{
			cmd("set_value", textArg("value")),
		},
	})

	must(EntityType{
		Domain:      DomainTime,
		Description: "Time setting",
		StateType:   StateTypeText,
		Commands: []CommandSpec{
			cmd("set_value", textArg("value")),
		},
	})

	must(EntityType{
		Domain:      DomainText,
		Description: "Free text setting",
		StateType:   StateTypeText,
		Commands: []CommandSpec{
			cmd("set_value", textArg("value")),
		},
	})

	must(EntityType{
		Domain:      DomainTodo,
		Description: "List of things to do",
		StateType:   StateTypeText,
		Commands: []CommandSpec{
			cmd("add_item", textArg("item")),
			cmd("remove_item", textArg("item")),
			cmd("update_item", textArg("item")),
		},
	})

	must(EntityType{
		Domain:      DomainInputBoolean,
		Description: "Helper: a switch nobody controls physically",
		StateType:   StateTypeBool,
		Commands: []CommandSpec{
			cmd("turn_on"),
			cmd("turn_off"),
			cmd("toggle"),
		},
	})

	must(EntityType{
		Domain:      DomainInputNumber,
		Description: "Helper: a number nobody controls physically",
		StateType:   StateTypeNumber,
		Commands: []CommandSpec{
			cmd("set_value", numArg("value", -1e9, 1e9, "")),
		},
	})

	must(EntityType{
		Domain:      DomainInputSelect,
		Description: "Helper: an option list nobody controls physically",
		StateType:   StateTypeEnum,
		Commands: []CommandSpec{
			cmd("select_option", selectArg("option")),
		},
	})

	must(EntityType{
		Domain:      DomainInputText,
		Description: "Helper: a text nobody controls physically",
		StateType:   StateTypeText,
		Commands: []CommandSpec{
			cmd("set_value", textArg("value")),
		},
	})

	must(EntityType{
		Domain:      DomainInputDatetime,
		Description: "Helper: a date and time nobody controls physically",
		StateType:   StateTypeText,
		Commands: []CommandSpec{
			cmd("set_value", textArg("value")),
		},
	})

	must(EntityType{
		Domain:       DomainWeather,
		Description:  "Weather conditions",
		StateType:    StateTypeText,
		Capabilities: []string{"temperature", "humidity", "pressure", "wind_speed"},
	})

	must(EntityType{
		Domain:      DomainAirQuality,
		Description: "Air quality measurement",
		StateType:   StateTypeNumber,
		DeviceClasses: []string{
			"aqi", "carbon_dioxide", "carbon_monoxide", "nitrogen_dioxide",
			"nitrogen_monoxide", "ozone", "pm1", "pm10", "pm25",
			"sulphur_dioxide", "volatile_organic_compounds",
		},
	})

	must(EntityType{
		Domain:        DomainPerson,
		Description:   "A person and their presence",
		StateType:     StateTypeBool,
		DeviceClasses: []string{"presence"},
	})

	must(EntityType{
		Domain:      DomainDeviceTracker,
		Description: "A tracked device and its presence",
		StateType:   StateTypeBool,
	})

	must(EntityType{
		Domain:      DomainSun,
		Description: "The sun",
		StateType:   StateTypeEnum,
		States:      []string{"above_horizon", "below_horizon"},
	})

	must(EntityType{
		Domain:      DomainCalendar,
		Description: "Calendar with the next events",
		StateType:   StateTypeText,
	})

	must(EntityType{
		Domain:      DomainCounter,
		Description: "Counter helper",
		StateType:   StateTypeNumber,
		Commands: []CommandSpec{
			cmd("increment"),
			cmd("decrement"),
			cmd("reset"),
		},
	})

	return c
}
