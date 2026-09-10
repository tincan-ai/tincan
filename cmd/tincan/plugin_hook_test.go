package main

import (
	"encoding/json"
	"github.com/tincan-ai/tincan/internal/core"
	"os"
	"strings"
	"testing"
)

func hookFixture(t *testing.T) (string, *pluginConnection, string) {
	t.Helper()
	root := t.TempDir()
	b := &pluginBroker{root: root}
	c := &pluginConnection{Handle: "conn_00000000000000000000000000000001", CodexThreadID: testCodexThread, AgentID: "agent_self", Config: Config{Token: "secret", Server: "http://localhost:8080"}}
	if err := b.save(c); err != nil {
		t.Fatal(err)
	}
	path := inboxPath(root, c)
	e := inboxEvent{Seq: 7, Kind: "message", Payload: core.Message{ID: "msg_1", AgentID: "peer", Text: "UNTRUSTED PEER BODY", Mentions: []string{c.AgentID}}}
	if err := writePrivateJSON(path, inboxState{Pending: &e}); err != nil {
		t.Fatal(err)
	}
	return root, c, path
}
func TestHookIsolationDedupRecovery(t *testing.T) {
	root, c, path := hookFixture(t)
	before, _ := os.ReadFile(path)
	// The background consumer can retain its lock while a hook reads the snapshot.
	lock, err := lockInbox(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer unlockInbox(lock)
	in := hookInput{SessionID: c.CodexThreadID, Event: "PostToolUse", TurnID: "turn1"}
	for _, wrong := range []hookInput{{SessionID: "../../other", Event: in.Event}, {SessionID: "00000000-0000-0000-0000-000000000002", Event: in.Event}, {SessionID: c.CodexThreadID, Event: in.Event, AgentID: "subagent"}} {
		got, err := runHook(root, wrong)
		if err != nil || len(got) != 0 {
			t.Fatal(got, err)
		}
	}
	got, err := runHook(root, in)
	if err != nil || got["hookSpecificOutput"] == nil {
		t.Fatal(got, err)
	}
	data, _ := json.Marshal(got)
	if strings.Contains(string(data), "UNTRUSTED") || strings.Contains(string(data), "secret") || !strings.Contains(string(data), c.Handle) {
		t.Fatal(string(data))
	}
	got, err = runHook(root, in)
	if err != nil || len(got) != 0 {
		t.Fatal("duplicate context", got, err)
	}
	in.Event = "Stop"
	got, err = runHook(root, in)
	if err != nil || len(got) != 0 {
		t.Fatal("stop loop", got, err)
	}
	in.Event = "UserPromptSubmit"
	in.TurnID = "turn2"
	got, err = runHook(root, in)
	if err != nil || got["hookSpecificOutput"] == nil {
		t.Fatal("lost unfinished work", got, err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("presentation changed acknowledgement state")
	}
}
func TestHookStopAndCorruptQuiet(t *testing.T) {
	root, c, path := hookFixture(t)
	in := hookInput{SessionID: c.CodexThreadID, Event: "Stop", TurnID: "turn1", StopActive: true}
	got, err := runHook(root, in)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	in.StopActive = false
	got, err = runHook(root, in)
	if err != nil || got["decision"] != "block" {
		t.Fatal(got, err)
	}
	got, err = runHook(root, in)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	if err = os.WriteFile(path, []byte("bad json"), 0600); err != nil {
		t.Fatal(err)
	}
	in.TurnID = "turn2"
	got, err = runHook(root, in)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	var output strings.Builder
	if err = hookCommand(strings.NewReader("invalid"), &output); err != nil || strings.TrimSpace(output.String()) != "{}" {
		t.Fatal(output.String(), err)
	}
}
