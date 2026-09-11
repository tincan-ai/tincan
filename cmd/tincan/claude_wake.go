package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

// Claude owns this process through asyncRewake. Never detach it: exit 2 is
// delivered to the originating session, not to a shared MCP process. The MCP
// listener remains the sole network/inbox consumer; this waits on snapshots.
const claudeWakeLifetime = 24 * time.Hour

func claudeWakePath(root, session string) string {
	return filepath.Join(root, "claude-wake-"+session+".json")
}

type claudeWakeLease struct {
	ExpiresAt time.Time `json:"expires_at"`
}

func claudeWakeArmed(root string, c *pluginConnection) bool {
	if c.HookHost != "claude" || validateHookBinding(c.HookHost, c.HookSessionID) != nil || c.HookSessionID == "" {
		return false
	}
	path := claudeWakePath(root, c.HookSessionID)
	var lease claudeWakeLease
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &lease) != nil || !time.Now().Before(lease.ExpiresAt) {
		return false
	}
	// A receipt alone survives a crash. A live process must still own its lock.
	lock, err := lockInbox(path + ".lock")
	if err == nil {
		unlockInbox(lock)
		return false
	}
	return isWakeLockBusy(err)
}

func waitClaudeWake(ctx context.Context, root string, in hookInput, interval time.Duration, output io.Writer) int {
	if in.AgentID != "" || in.SessionID == "" || validateHookBinding("claude", in.SessionID) != nil {
		return 0
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return 0
	}
	path := claudeWakePath(root, in.SessionID)
	lock, err := lockInbox(path + ".lock")
	if err != nil {
		return 0 // One waiter per session, even when multiple hooks fire.
	}
	defer unlockInbox(lock)
	defer os.Remove(path)
	expires := time.Now().Add(claudeWakeLifetime)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(expires) {
		expires = deadline
	}
	if writePrivateJSON(path, claudeWakeLease{ExpiresAt: expires}) != nil {
		return 0
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	in.Host, in.Event, in.TurnID, in.StopActive = "claude", "TincanWake", "", false
	for ctx.Err() == nil {
		result, err := runHook(root, in)
		if err == nil {
			if specific, ok := result["hookSpecificOutput"].(map[string]any); ok {
				if pointer, ok := specific["additionalContext"].(string); ok && pointer != "" {
					if _, err := fmt.Fprintln(output, pointer); err == nil {
						return 2
					}
					return 0
				}
			}
		}
		select {
		case <-ctx.Done():
			return 0
		case <-ticker.C:
		}
	}
	return 0
}

func claudeWakeCommand(input io.Reader, output io.Writer) int {
	var in hookInput
	if json.NewDecoder(io.LimitReader(input, 2*1024*1024)).Decode(&in) != nil {
		return 0
	}
	root, err := runtimeStateDirectory(os.Getenv("TINCAN_STATE_DIR"))
	if err != nil {
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, claudeWakeLifetime)
	defer cancel()
	return waitClaudeWake(ctx, root, in, time.Second, output)
}
