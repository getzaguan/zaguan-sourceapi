// The link publisher.
//
// A source publishes snapshots and batches of events to a hub. The order and
// the monotonic numbers are part of the contract: a snapshot must be newer
// than the last one and an event batch must not cross a snapshot. This type
// keeps those rules in one place so every provider does not implement them
// again.
package sourceapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Publisher owns the wire ordering of one source against one hub.
//
// It does not own the provider's loop or its model: the provider builds the
// Snapshot and the events, and keeps its own retry/heartbeat policy. The
// Publisher only guarantees that a snapshot and an event batch do not cross
// each other and that the numbers grow.
type Publisher struct {
	mu       sync.Mutex
	client   *Client
	version  uint64
	sequence uint64
	hubMu    sync.Mutex
}

// NewPublisher returns a publisher for a client. The client may be nil while
// the provider has no hub configured; SetClient installs it later.
func NewPublisher(client *Client) *Publisher {
	return &Publisher{
		client:  client,
		version: uint64(time.Now().UnixMilli()),
	}
}

// Client returns the current client, or nil.
func (p *Publisher) Client() *Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.client
}

// SetClient changes the hub. The provider calls it when its settings change.
func (p *Publisher) SetClient(client *Client) {
	p.hubMu.Lock()
	defer p.hubMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.client = client
	// A new hub has its own event sequence; a snapshot will reset it on the
	// wire. Start the local one from zero until that happens.
	p.sequence = 0
}

// Snapshot sends a full snapshot. The Publisher assigns its version and
// retries once when the hub has a newer one (a clock that went back or a hub
// restored from a backup). It returns the version it sent.
func (p *Publisher) Snapshot(ctx context.Context, snap Snapshot) (uint64, error) {
	p.hubMu.Lock()
	defer p.hubMu.Unlock()

	client := p.Client()
	if client == nil {
		return 0, errors.New("sourceapi: there is no hub to publish to")
	}
	version := p.nextVersion()
	snap.Version = version
	err := client.Snapshot(ctx, snap)
	if err == nil {
		p.resetSequence()
		return version, nil
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict {
		return version, err
	}
	// The hub has a newer version: take a new stamp and try once more.
	version = p.nextVersion()
	snap.Version = version
	if err := client.Snapshot(ctx, snap); err != nil {
		return version, fmt.Errorf("sourceapi: snapshot version %d: %w", version, err)
	}
	p.resetSequence()
	return version, nil
}

// Events sends one batch of events. The Publisher assigns the sequence. It
// returns the hub answer so the caller can decide what to do with ignored or
// skipped batches.
func (p *Publisher) Events(ctx context.Context, events []Event) (EventsResponse, error) {
	p.hubMu.Lock()
	defer p.hubMu.Unlock()

	client := p.Client()
	if client == nil {
		return EventsResponse{}, errors.New("sourceapi: there is no hub to publish to")
	}
	p.mu.Lock()
	p.sequence++
	sequence := p.sequence
	p.mu.Unlock()
	return client.Events(ctx, Events{Sequence: sequence, Events: events})
}

// nextVersion returns a version that grows with the clock and with the last
// version this process sent.
func (p *Publisher) nextVersion() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := uint64(time.Now().UnixMilli())
	if now <= p.version {
		now = p.version + 1
	}
	p.version = now
	return now
}

func (p *Publisher) resetSequence() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sequence = 0
}
