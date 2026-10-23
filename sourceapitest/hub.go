// Package sourceapitest provides a fake Source API hub for provider tests.
//
// It speaks the same wire contract as a real hub: it versions snapshots,
// resets the event sequence on a snapshot, refuses a stale snapshot with 409
// and hands out queued commands with a long poll. A provider test can use the
// same hub as any other Go provider instead of copying a test server.
package sourceapitest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/getzaguan/zaguan-sourceapi"
)

// Hub is a fake Source API hub.
type Hub struct {
	tb testing.TB

	mu             sync.Mutex
	meta           sourceapi.Meta
	snapshot       sourceapi.Snapshot
	events         []sourceapi.Event
	results        map[string]sourceapi.CommandResult
	pending        []sourceapi.Command
	snapshots      int
	lastVersion    uint64
	lastSequence   uint64
	failSnapshots  int
	failEvents     int
	failHeartbeats int

	// snapshotsFeed receives every accepted snapshot. It never blocks the
	// hub: a test that does not read it loses the notifications.
	snapshotsFeed chan sourceapi.Snapshot
	// commandSignal wakes a long poll when Queue adds a command.
	commandSignal chan struct{}
	// answersFeed receives every command result. It never blocks the hub.
	answersFeed chan CommandAnswer

	server *httptest.Server
}

// CommandAnswer is a result the provider sent for a command.
type CommandAnswer struct {
	ID     string
	Result sourceapi.CommandResult
}

// NewHub starts a fake hub and closes it when the test ends.
func NewHub(tb testing.TB) *Hub {
	tb.Helper()
	h := &Hub{
		tb:            tb,
		results:       map[string]sourceapi.CommandResult{},
		snapshotsFeed: make(chan sourceapi.Snapshot, 16),
		commandSignal: make(chan struct{}, 1),
		answersFeed:   make(chan CommandAnswer, 16),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/source", h.putMeta)
	mux.HandleFunc("PUT /api/v1/source/snapshot", h.putSnapshot)
	mux.HandleFunc("POST /api/v1/source/events", h.postEvents)
	mux.HandleFunc("POST /api/v1/source/heartbeat", h.postHeartbeat)
	mux.HandleFunc("GET /api/v1/source/commands", h.getCommands)
	mux.HandleFunc("POST /api/v1/source/commands/{id}", h.postResult)
	h.server = httptest.NewServer(mux)
	tb.Cleanup(h.server.Close)
	return h
}

// URL is the address of the fake hub.
func (h *Hub) URL() string { return h.server.URL }

// Client returns a client of the fake hub.
func (h *Hub) Client(tb testing.TB) *sourceapi.Client {
	tb.Helper()
	client, err := sourceapi.New(sourceapi.Config{BaseURL: h.URL(), Token: "test"})
	if err != nil {
		tb.Fatal(err)
	}
	return client
}

// Meta returns the last source metadata the hub received.
func (h *Hub) Meta() sourceapi.Meta {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.meta
}

// LastSnapshot returns the last snapshot the hub accepted.
func (h *Hub) LastSnapshot() sourceapi.Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snapshot
}

// SnapshotCount returns how many snapshots the hub accepted.
func (h *Hub) SnapshotCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snapshots
}

// AllEvents returns every event the hub accepted, in order.
func (h *Hub) AllEvents() []sourceapi.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]sourceapi.Event(nil), h.events...)
}

// SnapshotFeed returns a channel that receives every accepted snapshot.
func (h *Hub) SnapshotFeed() <-chan sourceapi.Snapshot { return h.snapshotsFeed }

// Result returns the answer the provider sent for a command.
func (h *Hub) Result(id string) (sourceapi.CommandResult, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	result, ok := h.results[id]
	return result, ok
}

// ResultFeed returns a channel with the answers the provider sends.
func (h *Hub) ResultFeed() <-chan CommandAnswer { return h.answersFeed }

// Queue puts commands in the hub for the provider's long poll.
func (h *Hub) Queue(commands ...sourceapi.Command) {
	h.mu.Lock()
	h.pending = append(h.pending, commands...)
	h.mu.Unlock()
	select {
	case h.commandSignal <- struct{}{}:
	default:
	}
}

// FailSnapshots makes the hub refuse the next n snapshots.
func (h *Hub) FailSnapshots(n int) {
	h.mu.Lock()
	h.failSnapshots = n
	h.mu.Unlock()
}

// FailEvents makes the hub refuse the next n event batches.
func (h *Hub) FailEvents(n int) {
	h.mu.Lock()
	h.failEvents = n
	h.mu.Unlock()
}

// FailHeartbeats makes the hub refuse the next n heartbeats.
func (h *Hub) FailHeartbeats(n int) {
	h.mu.Lock()
	h.failHeartbeats = n
	h.mu.Unlock()
}

func (h *Hub) putMeta(w http.ResponseWriter, r *http.Request) {
	var meta sourceapi.Meta
	if err := json.NewDecoder(r.Body).Decode(&meta); err != nil {
		h.tb.Errorf("the metadata is not valid JSON: %v", err)
		http.Error(w, "bad metadata", http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	h.meta = meta
	h.mu.Unlock()
	writeJSON(w, map[string]any{"ok": true})
}

func (h *Hub) putSnapshot(w http.ResponseWriter, r *http.Request) {
	var snapshot sourceapi.Snapshot
	if err := json.NewDecoder(r.Body).Decode(&snapshot); err != nil {
		h.tb.Errorf("the snapshot is not valid JSON: %v", err)
		http.Error(w, "bad snapshot", http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	if h.failSnapshots > 0 {
		h.failSnapshots--
		h.mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	stale := snapshot.Version != 0 && snapshot.Version <= h.lastVersion
	if !stale {
		h.lastVersion = snapshot.Version
		h.snapshot = snapshot
		h.snapshots++
		// A snapshot resets the sequence of the events, like the hub does.
		h.lastSequence = 0
	}
	h.mu.Unlock()
	if stale {
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]any{
			"error": map[string]string{
				"type":    "stale_snapshot",
				"message": "snapshot version is not newer than the stored one",
			},
		})
		return
	}
	select {
	case h.snapshotsFeed <- snapshot:
	default:
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (h *Hub) postEvents(w http.ResponseWriter, r *http.Request) {
	var batch sourceapi.Events
	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		h.tb.Errorf("the events are not valid JSON: %v", err)
		http.Error(w, "bad events", http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	if h.failEvents > 0 {
		h.failEvents--
		h.mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	ignored := batch.Sequence != 0 && batch.Sequence <= h.lastSequence
	if !ignored {
		h.lastSequence = batch.Sequence
		h.events = append(h.events, batch.Events...)
	}
	h.mu.Unlock()
	writeJSON(w, map[string]any{
		"ok":       true,
		"accepted": len(batch.Events),
		"ignored":  ignored,
		"sequence": batch.Sequence,
	})
}

func (h *Hub) postHeartbeat(w http.ResponseWriter, _ *http.Request) {
	h.mu.Lock()
	if h.failHeartbeats > 0 {
		h.failHeartbeats--
		h.mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	devices := len(h.snapshot.Devices)
	h.mu.Unlock()
	writeJSON(w, map[string]any{
		"ok": true, "online": true, "lease_seconds": 90, "devices": devices,
	})
}

func (h *Hub) getCommands(w http.ResponseWriter, r *http.Request) {
	wait := 25 * time.Second
	if raw := r.URL.Query().Get("wait"); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			wait = parsed
		}
	}
	if wait > 60*time.Second {
		wait = 60 * time.Second
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	for {
		h.mu.Lock()
		pending := h.pending
		h.pending = nil
		h.mu.Unlock()
		if len(pending) > 0 {
			writeJSON(w, pending)
			return
		}
		select {
		case <-h.commandSignal:
		case <-deadline.C:
			w.WriteHeader(http.StatusNoContent)
			return
		case <-r.Context().Done():
			return
		}
	}
}

func (h *Hub) postResult(w http.ResponseWriter, r *http.Request) {
	var result sourceapi.CommandResult
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		h.tb.Errorf("the result is not valid JSON: %v", err)
		http.Error(w, "bad result", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	h.mu.Lock()
	h.results[id] = result
	h.mu.Unlock()
	select {
	case h.answersFeed <- CommandAnswer{ID: id, Result: result}:
	default:
	}
	writeJSON(w, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
