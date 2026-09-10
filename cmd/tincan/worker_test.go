package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestWorkerCompletionBoundary(t *testing.T) {
	for _, status := range []string{"completed", "failed", "interrupted"} {
		var wire bytes.Buffer
		fmt.Fprintf(&wire, "{\"id\":1,\"result\":{\"turn\":{\"id\":\"turn1\"}}}\n")
		fmt.Fprintf(&wire, "{\"id\":\"tool1\",\"method\":\"item/tool/call\",\"params\":{\"threadId\":\"worker1\",\"tool\":\"inbox_reply\",\"arguments\":{\"seq\":7,\"text\":\"done\"}}}\n")
		fmt.Fprintf(&wire, "{\"method\":\"turn/completed\",\"params\":{\"threadId\":\"worker1\",\"turn\":{\"id\":\"turn1\",\"status\":%q}}}\n", status)
		var sent bytes.Buffer
		w := &codexWorker{rpc: newCodexRPC(&sent, &wire), thread: "worker1", connection: "conn1", tools: map[string]bool{"inbox_reply": true}, callTool: func(context.Context, string, map[string]any) (any, error) {
			t.Fatal("sent acknowledgement before turn completion")
			return nil, nil
		}}
		completion, err := w.turn(context.Background(), &inboxEvent{Seq: 7})
		if status == "completed" {
			if err != nil || completion == nil || completion.Text == nil || *completion.Text != "done" {
				t.Fatal(completion, err)
			}
		} else if err == nil || completion != nil {
			t.Fatal("committed failed turn", completion, err)
		}
	}
}
func TestWorkerRejectsCrossTaskAndApproval(t *testing.T) {
	w := &codexWorker{thread: "worker1", connection: "conn1", seq: 7, tools: map[string]bool{"inbox_ack": true}}
	for _, p := range []string{
		`{"threadId":"other","tool":"inbox_ack","arguments":{"seq":7}}`,
		`{"threadId":"worker1","tool":"inbox_ack","arguments":{"seq":7,"connection":"other"}}`,
		`{"threadId":"worker1","tool":"inbox_ack","arguments":{"seq":8}}`,
		`{"threadId":"worker1","tool":"tincan_connect","arguments":{}}`,
	} {
		v, err := w.handle(context.Background(), "item/tool/call", json.RawMessage(p))
		if err != nil || v.(map[string]any)["success"] != false || w.staged != nil {
			t.Fatal(v, err)
		}
	}
	if _, err := w.handle(context.Background(), "item/commandExecution/requestApproval", nil); err == nil || w.attention == "" {
		t.Fatal("silently approved", err)
	}
}
func TestWorkerDisconnectedKeepsPending(t *testing.T) {
	w := &codexWorker{rpc: newCodexRPC(&bytes.Buffer{}, strings.NewReader("")), thread: "worker1"}
	if c, err := w.turn(context.Background(), &inboxEvent{Seq: 7}); err == nil || c != nil {
		t.Fatal(c, err)
	}
}
