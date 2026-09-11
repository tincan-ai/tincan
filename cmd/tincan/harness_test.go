package main

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestClaudePluginLaunch(t *testing.T) {
	for _, tc := range []struct{ in, want []string }{
		{nil, []string{"--dangerously-load-development-channels", "plugin:tincan@tincan"}},
		{[]string{"--marketplace", "company", "--", "--continue"}, []string{"--dangerously-load-development-channels", "plugin:tincan@company", "--continue"}},
		{[]string{"--plugin-dir", "/a path/tincan", "--", "--continue"}, []string{"--dangerously-load-development-channels", "plugin:tincan@inline", "--plugin-dir", "/a path/tincan", "--continue"}},
	} {
		got, err := claudePluginArgs(tc.in)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatal(got, err)
		}
	}
	for _, args := range [][]string{{"--marketplace"}, {"--marketplace", "x;evil"}, {"--plugin-dir"}, {"--", "--channels", "other"}} {
		if _, err := claudePluginArgs(args); err == nil {
			t.Fatal(args)
		}
	}
}

func TestHarnessHooksScopeFormatsAndClaims(t *testing.T) {
	for _, host := range []string{"cursor", "copilot", "claude"} {
		t.Run(host, func(t *testing.T) {
			root, c, path := hookFixture(t)
			b := &pluginBroker{root: root}
			c.CodexThreadID = ""
			if err := (&pluginBroker{root: root}).save(c); err != nil {
				t.Fatal(err)
			}
			if err := b.bindHook(c, host, "session-one"); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			t.Setenv("TINCAN_STATE_DIR", root)
			invoke := func(session, event string, extra map[string]any) string {
				raw := map[string]any{"session_id": session, "hook_event_name": event, "status": "completed"}
				for k, v := range extra {
					raw[k] = v
				}
				data, _ := json.Marshal(raw)
				var out bytes.Buffer
				if err := harnessHookCommand([]string{host}, bytes.NewReader(data), &out); err != nil {
					t.Fatal(err)
				}
				return out.String()
			}
			if got := invoke("other", "Stop", nil); strings.Contains(got, c.Handle) {
				t.Fatal(got)
			}
			if got := invoke("session-one", "SessionStart", nil); !strings.Contains(got, "hook_session_id") || !strings.Contains(got, c.Handle) || strings.Contains(got, "UNTRUSTED") || strings.Contains(got, "secret") {
				t.Fatal(got)
			}
			if got := invoke("session-one", "Stop", nil); strings.Contains(got, c.Handle) {
				t.Fatal("duplicate", got)
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(before) {
				t.Fatal("hook mutated durable inbox")
			}
			state := inboxState{}
			data, _ := os.ReadFile(path)
			json.Unmarshal(data, &state)
			state.Claim = &inboxClaim{WorkerID: "worker", Token: "claim"}
			writePrivateJSON(path, state)
			if got := invoke("session-one", "SessionStart", nil); strings.Contains(got, c.Handle) {
				t.Fatal("claimed work redelivered", got)
			}
			if err := b.bindHook(c, host, "other"); err == nil {
				t.Fatal("rebound identity")
			}
			if err := validateHookBinding(host, "../secret"); err == nil {
				t.Fatal("path traversal")
			}
		})
	}
}

func TestCopilotEventArgumentAndCursorAborted(t *testing.T) {
	t.Setenv("TINCAN_STATE_DIR", t.TempDir())
	var out bytes.Buffer
	harnessHookCommand([]string{"copilot", "SessionStart"}, strings.NewReader(`{"sessionId":"copilot-session"}`), &out)
	if !strings.Contains(out.String(), "additionalContext") || !strings.Contains(out.String(), "copilot-session") {
		t.Fatal(out.String())
	}
	out.Reset()
	harnessHookCommand([]string{"cursor"}, strings.NewReader(`{"conversation_id":"cursor-session","hook_event_name":"stop","status":"aborted"}`), &out)
	if strings.TrimSpace(out.String()) != "{}" {
		t.Fatal(out.String())
	}
}

func TestHarnessBindingCannotOverwriteAnotherRuntime(t *testing.T) {
	root, c, _ := hookFixture(t)
	b := &pluginBroker{root: root}
	if err := b.bindHook(c, "cursor", "session-one"); err == nil {
		t.Fatal("overwrote Codex binding")
	}
	c.CodexThreadID = ""
	if err := b.save(c); err != nil {
		t.Fatal(err)
	}
	stale := *c
	if err := b.bindHook(c, "cursor", "session-one"); err != nil {
		t.Fatal(err)
	}
	if err := b.bindHook(&stale, "copilot", "session-two"); err == nil {
		t.Fatal("stale snapshot overwrote binding")
	}
	b.host = "codex"
	if err := b.bindCodex(t.Context(), c, testCodexThread); err == nil {
		t.Fatal("Codex overwrote another harness")
	}
}
