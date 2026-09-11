package main

import (
	"context"
	"errors"
	"github.com/tincan-ai/tincan-plugin/internal/core"
	"sync"
	"time"
)

// Presence is independent of SSE backpressure and model execution.
func runPresence(ctx context.Context, c Config, ready func(context.Context) bool, interval time.Duration) {
	session := core.ID("ps_")
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = callContext(stop, c, "DELETE", "/agents/me/presence/"+session, nil)
	}()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		probe, cancel := context.WithTimeout(ctx, 10*time.Second)
		available := ready != nil && ready(probe)
		cancel()
		if ctx.Err() != nil {
			return
		}
		beat, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, _ = callContext(beat, c, "POST", "/agents/me/presence", map[string]any{"session_id": session, "available": available})
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func startPresence(ctx context.Context, c Config, ready func(context.Context) bool) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); runPresence(ctx, c, ready, core.PresenceInterval) }()
	var once sync.Once
	return func() { once.Do(func() { cancel(); <-done }) }
}

type hostPresence struct {
	Available bool
	SeenAt    time.Time
}

// Harness readiness expires even if an orphaned sidecar keeps streaming.
func (b *pluginBroker) hostStatus(handle string, available bool) error {
	if _, err := b.load(handle); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		return errors.New("plugin is shutting down")
	}
	if b.hostPresence == nil {
		b.hostPresence = map[string]hostPresence{}
	}
	b.hostPresence[handle] = hostPresence{Available: available, SeenAt: time.Now()}
	return nil
}

func (b *pluginBroker) runtimeAvailable(ctx context.Context, saved *pluginConnection) bool {
	b.mu.Lock()
	h := b.hostPresence[saved.Handle]
	stopped := b.stopped
	b.mu.Unlock()
	if stopped {
		return false
	}
	if b.host == "codex" {
		c, err := b.load(saved.Handle)
		if err != nil {
			return false
		}
		d := b.deliverySnapshot(c.Handle)
		if d.Method == "codex_app_server" && d.IdleWake {
			// A nil payload checks the loaded task without starting a turn.
			return b.codexDelivery(ctx, c, nil) == nil
		}
		if b.codexQueueProbe != nil {
			return b.codexQueueProbe(ctx, c) == nil
		}
		t, err := b.targetFor(c)
		return err == nil && codexQueueProbe(ctx, t, c.CodexThreadID) == nil
	}
	if b.host == "codex-worker" {
		return b.notify != nil
	}
	if saved.HookHost == "claude" {
		// Reload because an MCP process can predate this session's binding.
		if c, err := b.load(saved.Handle); err == nil && claudeWakeArmed(b.root, c) {
			return true
		}
	}
	return h.Available && time.Since(h.SeenAt) < core.PresenceTTL
}
