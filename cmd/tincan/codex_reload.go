package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// Reload only the runtime that already owns this task. Never start another
// server or redeem an invitation while repairing tool discovery.
func codexReloadCommand() error {
	thread := os.Getenv("CODEX_THREAD_ID")
	if !codexTaskID.MatchString(thread) {
		return errors.New("run this command from your Codex task so it can find the right conversation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	target, err := discoverCodexTarget(ctx)
	if err != nil {
		return err
	}
	rpc, close, err := openCodexTarget(ctx, target)
	if err != nil {
		return fmt.Errorf("could not reach this Codex session to reload tools: %w", err)
	}
	defer close()
	if err = reloadCodexRPC(rpc, thread); err != nil {
		return err
	}
	fmt.Println("Codex accepted the tool reload. On the next turn, check for tincan_connect before using the invitation.")
	return nil
}

func reloadCodexRPC(rpc *codexRPC, thread string) error {
	if err := rpc.initialize(); err != nil {
		return err
	}
	if err := verifyCodexLoaded(rpc, thread); err != nil {
		return err
	}
	_, err := rpc.call("config/mcpServer/reload", map[string]any{})
	return err
}
