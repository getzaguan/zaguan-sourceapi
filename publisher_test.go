package sourceapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// publisherHub records the versions and sequences the publisher sends.
type publisherHub struct {
	mu        sync.Mutex
	versions  []uint64
	sequences []uint64
	conflicts int
}

func (h *publisherHub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/source/snapshot", func(w http.ResponseWriter, r *http.Request) {
		var body Snapshot
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "body", http.StatusBadRequest)
			return
		}
		h.mu.Lock()
		if h.conflicts > 0 {
			h.conflicts--
			h.mu.Unlock()
			w.WriteHeader(http.StatusConflict)
			return
		}
		h.versions = append(h.versions, body.Version)
		h.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/v1/source/events", func(w http.ResponseWriter, r *http.Request) {
		var body Events
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "body", http.StatusBadRequest)
			return
		}
		h.mu.Lock()
		h.sequences = append(h.sequences, body.Sequence)
		h.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "accepted": len(body.Events)})
	})
	return mux
}

func newPublisherForTest(t *testing.T, hub *publisherHub) *Publisher {
	t.Helper()
	srv := httptest.NewServer(hub.handler())
	t.Cleanup(srv.Close)
	client, err := New(Config{BaseURL: srv.URL, Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	return NewPublisher(client)
}

func TestPublisherNumbersSnapshotsAndEvents(t *testing.T) {
	hub := &publisherHub{}
	p := newPublisherForTest(t, hub)

	v1, err := p.Snapshot(context.Background(), Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Events(context.Background(), []Event{{Type: "state"}}); err != nil {
		t.Fatal(err)
	}
	// A snapshot resets the sequence on the hub; the local one follows.
	v2, err := p.Snapshot(context.Background(), Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Events(context.Background(), []Event{{Type: "state"}}); err != nil {
		t.Fatal(err)
	}
	if v2 <= v1 {
		t.Fatalf("the snapshot versions do not grow: %d then %d", v1, v2)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if len(hub.versions) != 2 || len(hub.sequences) != 2 {
		t.Fatalf("versions=%v sequences=%v", hub.versions, hub.sequences)
	}
	if hub.sequences[0] != 1 || hub.sequences[1] != 1 {
		t.Fatalf("the event sequence after a snapshot = %v, want 1, 1", hub.sequences)
	}
}

func TestPublisherRetriesAConflict(t *testing.T) {
	hub := &publisherHub{conflicts: 1}
	p := newPublisherForTest(t, hub)
	if _, err := p.Snapshot(context.Background(), Snapshot{}); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if len(hub.versions) != 1 {
		t.Fatalf("the accepted snapshots = %v", hub.versions)
	}
}

// TestPublisherSerializesSnapshotAndEvents checks that an event waits for a
// snapshot that is already in flight.
func TestPublisherSerializesSnapshotAndEvents(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut:
			once.Do(func() { close(entered) })
			<-release
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == http.MethodPost:
			select {
			case <-release:
			default:
				t.Error("the event was sent while the snapshot was in flight")
			}
			_, _ = w.Write([]byte(`{"ok":true,"accepted":1}`))
		}
	}))
	defer srv.Close()
	client, err := New(Config{BaseURL: srv.URL, Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	p := NewPublisher(client)

	snapshotDone := make(chan error, 1)
	go func() {
		_, err := p.Snapshot(context.Background(), Snapshot{})
		snapshotDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the snapshot did not start")
	}
	eventDone := make(chan error, 1)
	go func() {
		_, err := p.Events(context.Background(), []Event{{Type: "state"}})
		eventDone <- err
	}()
	select {
	case err := <-eventDone:
		t.Fatalf("the event did not wait for the snapshot: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-snapshotDone; err != nil {
		t.Fatal(err)
	}
	if err := <-eventDone; err != nil {
		t.Fatal(err)
	}
}
