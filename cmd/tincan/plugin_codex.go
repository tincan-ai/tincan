package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/tincan-ai/tincan-plugin/internal/core"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"
)

var codexTaskID = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

// Use the existing host's control socket, never launch a competing app server or
// resume a desktop task in another runtime. Payloads travel over the socket.
func codexCallTarget(ctx context.Context, t codexTarget, thread string, payload map[string]any) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	r, close, err := openCodexTarget(ctx, t)
	if err != nil {
		return err
	}
	err = codexExchangeRPC(r, thread, payload)
	close()
	if err != nil && r.diagnostics != nil && r.diagnostics() != "" {
		return fmt.Errorf("%w: %s", err, r.diagnostics())
	}
	return err
}
func codexExchange(in io.Writer, out io.Reader, thread string, payload map[string]any) error {
	r := newCodexRPC(in, out)
	defer r.shutdown()
	return codexExchangeRPC(r, thread, payload)
}
func codexExchangeRPC(r *codexRPC, thread string, payload map[string]any) error {
	if !codexTaskID.MatchString(thread) {
		return errors.New("codex_thread_id must be this task's CODEX_THREAD_ID UUID")
	}
	if err := r.initialize(); err != nil {
		return err
	}
	if err := verifyCodexLoaded(r, thread); err != nil {
		return err
	}
	if payload == nil {
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = r.call("turn/start", map[string]any{"threadId": thread, "input": []any{}, "toolOutput": map[string]any{"name": "tincan_event", "output": string(data)}})
	return err
}

func verifyCodexLoaded(r *codexRPC, thread string) error {
	// A persisted thread/read result does not prove that this runtime owns the task.
	params := map[string]any{}
	seen := map[string]bool{}
	for {
		data, err := r.call("thread/loaded/list", params)
		if err != nil {
			return err
		}
		var loaded struct {
			Data       []string `json:"data"`
			NextCursor string   `json:"nextCursor"`
		}
		if err = json.Unmarshal(data, &loaded); err != nil {
			return err
		}
		if slices.Contains(loaded.Data, thread) {
			break
		}
		if loaded.NextCursor == "" || seen[loaded.NextCursor] {
			return errors.New("Codex task is not loaded in the reachable runtime")
		}
		seen[loaded.NextCursor] = true
		params["cursor"] = loaded.NextCursor
	}
	return nil
}

// Experimental stream subscriptions forward events to an App Server client.
// They are hosted-app-only in 0.153.4 and do not themselves wake the model.
// Probe the public API with our real server identity; never masquerade as an app.
func probeCodexEventsTarget(ctx context.Context, t codexTarget, thread string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r, close, err := openCodexTarget(ctx, t)
	if err != nil {
		return "unavailable"
	}
	defer close()
	if err = r.initialize(); err != nil {
		return "unavailable"
	}
	return probeCodexEventsRPC(r, thread)
}
func probeCodexEventsRPC(r *codexRPC, thread string) string {
	id := core.ID("tincan_probe_")
	_, err := r.call("mcpServer/event/stream/start", map[string]any{"threadId": thread, "server": "tincan", "name": "mentions", "arguments": map[string]any{}, "subscriptionId": id})
	if err != nil {
		if strings.Contains(err.Error(), "only supported for hosted apps") {
			return "unavailable_for_local_plugins"
		}
		return "unavailable: " + err.Error()
	}
	// A successful client subscription still lacks a host-to-model routing contract.
	_, _ = r.call("mcpServer/event/stream/stop", map[string]any{"subscriptionId": id})
	return "client_stream_only_no_verified_model_delivery"
}

func (b *pluginBroker) bindCodex(ctx context.Context, c *pluginConnection, thread string, targets ...*codexTarget) error {
	if b.host != "codex" {
		if thread != "" {
			return errors.New("codex_thread_id is only valid in Codex")
		}
		return nil
	}
	if c.HookHost != "" {
		return errors.New("connection belongs to another harness session")
	}
	if thread == "" {
		thread = c.CodexThreadID
	}
	if !codexTaskID.MatchString(thread) {
		return errors.New("agent must read its CODEX_THREAD_ID and pass codex_thread_id; do not ask the user for an ID")
	}
	if c.CodexThreadID != "" && c.CodexThreadID != thread {
		return errors.New("connection belongs to another Codex task; create a fresh connection")
	}
	var explicit *codexTarget
	if len(targets) > 0 {
		explicit = targets[0]
	}
	if err := b.selectCodexTarget(ctx, c, explicit); err != nil {
		return err
	}
	b.mu.Lock()
	latest, err := b.load(c.Handle)
	if err == nil {
		if latest.HookHost != "" || (latest.CodexThreadID != "" && latest.CodexThreadID != thread) {
			b.mu.Unlock()
			return errors.New("connection belongs to another Codex task")
		}
		latest.CodexTarget = c.CodexTarget
		latest.CodexThreadID = thread
		err = b.save(latest)
		*c = *latest
	}
	b.mu.Unlock()
	if err != nil {
		return err
	}
	b.probeDelivery(ctx, c)
	return nil
}

func (b *pluginBroker) codexDelivery(ctx context.Context, c *pluginConnection, payload map[string]any) error {
	if b.codexSend != nil {
		return b.codexSend(ctx, c.CodexThreadID, payload)
	}
	t, err := b.targetFor(c)
	if err != nil {
		return err
	}
	return codexCallTarget(ctx, t, c.CodexThreadID, payload)
}

func (b *pluginBroker) deliver(ctx context.Context, c *pluginConnection, payload map[string]any) error {
	payload = inboundNotification(payload)
	if b.host == "codex" {
		return b.deliverCodex(ctx, c, payload)
	}
	if b.notify != nil {
		return b.notify(ctx, payload)
	}
	return nil
}
