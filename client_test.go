package sourceapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeHub struct {
	mu           sync.Mutex
	snapshot     Snapshot
	events       Events
	meta         Meta
	result       CommandResult
	commandCalls int
	deleted      bool
	lastWait     string
}

func newFakeHub(t *testing.T) (*httptest.Server, *fakeHub) {
	t.Helper()
	f := &fakeHub{}
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/source/snapshot", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		if r.Method != http.MethodPut {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		var snap Snapshot
		if err := json.NewDecoder(r.Body).Decode(&snap); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.snapshot = snap
		f.mu.Unlock()
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	mux.HandleFunc("/api/v1/source/events", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		var events Events
		if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.events = events
		f.mu.Unlock()
		_, _ = io.WriteString(w, `{"ok":true,"accepted":1,"skipped":0,"sequence":7}`)
	})
	mux.HandleFunc("/api/v1/source/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"online":true,"lease_seconds":90}`)
	})
	mux.HandleFunc("/api/v1/source/commands", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		f.mu.Lock()
		f.commandCalls++
		call := f.commandCalls
		f.lastWait = r.URL.Query().Get("wait")
		f.mu.Unlock()
		if call > 1 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = io.WriteString(w, `[{"id":"cmd_1","entity_id":"ent_1","entity_unique_id":"tuya:1/switch","command":"turn_on","expires_at":"2030-01-01T00:00:00Z"}]`)
	})
	mux.HandleFunc("/api/v1/source/commands/", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		var result CommandResult
		if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.result = result
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/v1/source", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		switch r.Method {
		case http.MethodPut:
			var meta Meta
			if err := json.NewDecoder(r.Body).Decode(&meta); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			f.mu.Lock()
			f.meta = meta
			f.mu.Unlock()
			_, _ = io.WriteString(w, `{"ok":true}`)
		case http.MethodDelete:
			f.mu.Lock()
			f.deleted = true
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method", http.StatusMethodNotAllowed)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, f
}

func TestClient(t *testing.T) {
	srv, fake := newFakeHub(t)
	c, err := New(Config{BaseURL: srv.URL, Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	available := true
	snap := Snapshot{
		Version: 1,
		Areas:   []Area{{ID: "living", Name: "Living"}},
		Devices: []Device{{UniqueID: "tuya:1", Name: "Lamp", AreaID: "living"}},
		Entities: []Entity{{
			UniqueID: "tuya:1/switch", DeviceUniqueID: "tuya:1",
			Domain: "switch", Name: "Lamp", State: false, Available: &available,
			Commands: []string{"turn_on", "turn_off"},
		}},
	}
	if err := c.Snapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	stored := fake.snapshot
	fake.mu.Unlock()
	if stored.Version != 1 || len(stored.Entities) != 1 || stored.Entities[0].State != false {
		t.Fatalf("snapshot = %+v", stored)
	}

	events := Events{Sequence: 7, Events: []Event{{
		Type: "state", EntityUniqueID: "tuya:1/switch", State: true,
	}}}
	resp, err := c.Events(ctx, events)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Sequence != 7 {
		t.Fatalf("events response = %+v", resp)
	}
	fake.mu.Lock()
	if fake.events.Sequence != 7 || len(fake.events.Events) != 1 {
		t.Fatalf("events = %+v", fake.events)
	}
	fake.mu.Unlock()

	hb, err := c.Heartbeat(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !hb.OK || !hb.Online || hb.LeaseSeconds != 90 {
		t.Fatalf("heartbeat = %+v", hb)
	}

	cmds, err := c.Commands(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].ID != "cmd_1" || cmds[0].Command != "turn_on" {
		t.Fatalf("commands = %+v", cmds)
	}
	fake.mu.Lock()
	if fake.lastWait == "" {
		t.Fatal("wait was not sent")
	}
	fake.mu.Unlock()
	if err := c.Result(ctx, cmds[0].ID, CommandResult{OK: true, State: true}); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	if !fake.result.OK || fake.result.State != true {
		t.Fatalf("result = %+v", fake.result)
	}
	fake.mu.Unlock()

	if err := c.Meta(ctx, Meta{Name: "Tuya", Kind: "tuya", Version: "1.0"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	if fake.meta.Name != "Tuya" || !fake.deleted {
		t.Fatalf("meta=%+v deleted=%v", fake.meta, fake.deleted)
	}
	fake.mu.Unlock()

	no, err := c.Commands(ctx, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if no != nil {
		t.Fatalf("expected no commands, got %+v", no)
	}
}

func TestUnauthorized(t *testing.T) {
	srv, _ := newFakeHub(t)
	c, err := New(Config{BaseURL: srv.URL, Token: "wrong"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Heartbeat(context.Background())
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("apiErr = %+v", apiErr)
	}
}

func TestStaleSnapshot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"code":"stale_snapshot","message":"snapshot version is not newer"}`)
	}))
	defer srv.Close()
	c, err := New(Config{BaseURL: srv.URL, Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	err = c.Snapshot(context.Background(), Snapshot{Version: 1})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict || apiErr.Code != "stale_snapshot" {
		t.Fatalf("err = %#v", err)
	}
	if errors.Is(err, ErrUnauthorized) {
		t.Fatal("409 must not be unauthorized")
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New(Config{Token: "x"}); err == nil {
		t.Fatal("missing URL accepted")
	}
	if _, err := New(Config{BaseURL: "ftp://example", Token: "x"}); err == nil {
		t.Fatal("bad scheme accepted")
	}
	if _, err := New(Config{BaseURL: "http://example"}); err == nil {
		t.Fatal("missing token accepted")
	}
	if _, err := New(Config{BaseURL: "http://example", Token: "x"}); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if !strings.HasPrefix((&Client{base: "http://example"}).String(), "sourceclient(") {
		t.Fatal("String")
	}
}
