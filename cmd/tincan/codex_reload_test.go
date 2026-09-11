package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCodexReloadMissingSocketGuidance(t *testing.T) {
	missing := fmt.Errorf("websocket handshake: %w", os.ErrNotExist)
	for _, tc := range []struct {
		name   string
		target codexTarget
		err    error
		check  bool
	}{
		{"absent default socket", defaultCodexTarget(), missing, true},
		{"configured socket", codexTarget{Endpoint: "unix:///custom.sock", Source: "cli_remote"}, missing, false},
		{"permission failure", defaultCodexTarget(), os.ErrPermission, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := codexReloadError(tc.target, tc.err)
			if !errors.Is(err, tc.err) || strings.Contains(err.Error(), "next user turn") != tc.check {
				t.Fatal(err)
			}
		})
	}
}

func TestCodexReloadChecksOwningRuntime(t *testing.T) {
	for _, wrong := range []bool{false, true} {
		t.Run(map[bool]string{false: "own task", true: "different task"}[wrong], func(t *testing.T) {
			f := newFakeCodex(t)
			f.wrongTask = wrong
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
			defer cancel()
			r, close, err := openCodexTarget(ctx, codexTarget{Endpoint: strings.Replace(f.server.URL, "http://", "ws://", 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer close()
			err = reloadCodexRPC(r, testCodexThread)
			if (err != nil) != wrong {
				t.Fatal("unexpected reload result", err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if slices.Contains(f.methods, "config/mcpServer/reload") == wrong {
				t.Fatal("reload ownership check failed", f.methods)
			}
			if slices.Contains(f.methods, "turn/start") {
				t.Fatal("reload must not start a turn")
			}
		})
	}
}
