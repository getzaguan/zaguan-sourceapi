// The source runtime.
package sourceapi

import (
	"context"
	"log/slog"
	"sync"
)

// SourceRuntimeConfig is the lease and the snapshot schedule of one source
// connection.
type SourceRuntimeConfig struct {
	Lease     LeaseConfig
	Snapshots SnapshotLoopConfig
}

// SourceRuntime runs the shared parts of a source: the hub lease and the
// snapshot schedule. The provider still builds the snapshots, runs the
// commands and owns its availability loop.
type SourceRuntime struct {
	lease     *Lease
	snapshots *SnapshotLoop
}

// NewSourceRuntime returns a source runtime.
func NewSourceRuntime(cfg SourceRuntimeConfig) *SourceRuntime {
	if cfg.Lease.Logger == nil {
		cfg.Lease.Logger = cfg.Snapshots.Logger
	}
	if cfg.Snapshots.Logger == nil {
		cfg.Snapshots.Logger = slog.Default()
	}
	return &SourceRuntime{
		lease:     NewLease(cfg.Lease),
		snapshots: NewSnapshotLoop(cfg.Snapshots),
	}
}

// Run runs the lease and the snapshot loop until ctx ends.
func (r *SourceRuntime) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		r.lease.Run(ctx)
	}()
	go func() {
		defer wg.Done()
		r.snapshots.Run(ctx)
	}()
	wg.Wait()
}

// RequestSnapshot asks the snapshot loop for a snapshot.
func (r *SourceRuntime) RequestSnapshot() {
	r.snapshots.Request()
}
