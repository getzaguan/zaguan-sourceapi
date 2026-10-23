// The hub lease.
package sourceapi

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Defaults of the lease.
const (
	DefaultHeartbeatInterval = 30 * time.Second
	DefaultCommandWait       = 25 * time.Second
	DefaultCommandRetry      = 2 * time.Second
	DefaultUnauthorizedRetry = 5 * time.Minute
)

// LeaseConfig configures the hub lease of one source connection.
//
// The provider keeps its own snapshot policy: the lease only renews the
// lease, picks up commands and answers them. A provider that changes its
// hub builds a new Lease for the new connection.
type LeaseConfig struct {
	Client *Client
	Logger *slog.Logger

	HeartbeatInterval time.Duration
	CommandWait       time.Duration
	// CommandWorkers is how many commands of one batch run at the same
	// time. Zero or one runs them in order.
	CommandWorkers int
	// CommandRetry is how long to wait after a failed long poll.
	CommandRetry time.Duration
	// UnauthorizedRetry is how long to wait when the hub rejected the
	// token: somebody has to fix it, but it must come back by itself.
	UnauthorizedRetry time.Duration

	// HandleCommand runs one command and returns its answer. The lease
	// sends the answer to the hub.
	HandleCommand func(context.Context, Command) CommandResult
	// OnResultError is called when the answer could not reach the hub. It
	// is where a provider publishes the state of the command as an event
	// when the result failed.
	OnResultError func(context.Context, Command, CommandResult, error)

	// OnHeartbeat is called with every successful heartbeat. The provider
	// updates its status and compares Devices with what it published.
	OnHeartbeat func(context.Context, HeartbeatResponse)
	// OnHeartbeatError is called when the heartbeat failed.
	OnHeartbeatError func(context.Context, error)
	// OnCommandError is called when the command long poll failed. The
	// lease retries by itself.
	OnCommandError func(context.Context, error)

	// OnRecovered is called once when a heartbeat or the command long poll
	// works again after a failure. The hub may have marked the source
	// offline, so the provider restores its availability here.
	OnRecovered func(context.Context)
}

// Lease keeps the lease of a source alive and answers the commands of the
// hub.
type Lease struct {
	cfg  LeaseConfig
	down atomic.Bool
}

// NewLease returns a lease with the defaults filled in.
func NewLease(cfg LeaseConfig) *Lease {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = DefaultHeartbeatInterval
	}
	if cfg.CommandWait <= 0 {
		cfg.CommandWait = DefaultCommandWait
	}
	if cfg.CommandRetry <= 0 {
		cfg.CommandRetry = DefaultCommandRetry
	}
	if cfg.UnauthorizedRetry <= 0 {
		cfg.UnauthorizedRetry = DefaultUnauthorizedRetry
	}
	return &Lease{cfg: cfg}
}

// Run renews the lease and answers commands until ctx ends. It returns
// immediately when there is no hub.
func (l *Lease) Run(ctx context.Context) {
	if l.cfg.Client == nil {
		return
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		l.heartbeats(ctx)
	}()
	go func() {
		defer wg.Done()
		l.commands(ctx)
	}()
	wg.Wait()
}

func (l *Lease) heartbeats(ctx context.Context) {
	ticker := time.NewTicker(l.cfg.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		resp, err := l.cfg.Client.Heartbeat(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			l.down.Store(true)
			if l.cfg.OnHeartbeatError != nil {
				l.cfg.OnHeartbeatError(ctx, err)
			}
			continue
		}
		if l.cfg.OnHeartbeat != nil {
			l.cfg.OnHeartbeat(ctx, resp)
		}
		l.recovered(ctx)
	}
}

// recovered tells the provider that the connection works again after a
// failure. Only the first success of the recovery is reported.
func (l *Lease) recovered(ctx context.Context) {
	if l.down.Swap(false) && l.cfg.OnRecovered != nil {
		l.cfg.OnRecovered(ctx)
	}
}

func (l *Lease) commands(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		commands, err := l.cfg.Client.Commands(ctx, l.cfg.CommandWait)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			l.down.Store(true)
			if l.cfg.OnCommandError != nil {
				l.cfg.OnCommandError(ctx, err)
			}
			wait := l.cfg.CommandRetry
			if errors.Is(err, ErrUnauthorized) {
				wait = l.cfg.UnauthorizedRetry
			}
			if !waitContext(ctx, wait) {
				return
			}
			continue
		}
		l.recovered(ctx)
		l.runBatch(ctx, commands)
	}
}

func (l *Lease) runBatch(ctx context.Context, commands []Command) {
	if len(commands) == 0 {
		return
	}
	workers := l.cfg.CommandWorkers
	if workers <= 1 {
		for _, cmd := range commands {
			if ctx.Err() != nil {
				return
			}
			l.runCommand(ctx, cmd)
		}
		return
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, cmd := range commands {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(cmd Command) {
			defer wg.Done()
			defer func() { <-sem }()
			l.runCommand(ctx, cmd)
		}(cmd)
	}
	wg.Wait()
}

func (l *Lease) runCommand(ctx context.Context, cmd Command) {
	result := CommandResult{Error: "the source has no command handler"}
	if l.cfg.HandleCommand != nil {
		result = l.cfg.HandleCommand(ctx, cmd)
	}
	err := l.cfg.Client.Result(ctx, cmd.ID, result)
	if err == nil || ctx.Err() != nil {
		return
	}
	if l.cfg.OnResultError != nil {
		l.cfg.OnResultError(ctx, cmd, result, err)
		return
	}
	l.cfg.Logger.Warn("cannot answer a command", "command", cmd.ID, "err", err)
}

func waitContext(ctx context.Context, wait time.Duration) bool {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
