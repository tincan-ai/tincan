package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestClaudeJoinsUseIsolatedCredentials(t *testing.T) {
	base := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("TINCAN_CONFIG", base)
	t.Setenv("TINCAN_TOKEN", "inherited-agent-token")
	original := []byte(`{"token":"existing-agent-token"}`)
	if err := os.WriteFile(base, original, 0600); err != nil {
		t.Fatal(err)
	}
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/join" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "Bearer inherited-agent-token" {
			t.Error("inherited identity used to join")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["name"] != "Claude" || body["invite"] == "" {
			t.Errorf("join body %v", body)
		}
		count++
		invite, _ := body["invite"].(string)
		json.NewEncoder(w).Encode(map[string]string{"token": "token-" + invite, "agent_id": "ag_" + invite})
	}))
	defer server.Close()
	paths := []string{}
	for _, invite := range []string{"one", "two"} {
		t.Setenv("TINCAN_CONFIG", base)
		t.Setenv("TINCAN_TOKEN", "inherited-agent-token")
		c, err := joinClaude(Config{Server: server.URL, Token: "inherited-agent-token"}, "Claude", invite)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, configPath())
		if c.Token != "token-"+invite || load().Token != c.Token {
			t.Fatal("child did not retain instance identity")
		}
		if os.Getenv("TINCAN_TOKEN") != "" {
			t.Fatal("inherited token overrides instance")
		}
	}
	if count != 2 || paths[0] == paths[1] || paths[0] == base || paths[1] == base {
		t.Fatalf("shared identity paths: %v", paths)
	}
	got, err := os.ReadFile(base)
	if err != nil || string(got) != string(original) {
		t.Fatal("existing credentials overwritten")
	}
	for idx, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var c Config
		if err := json.Unmarshal(data, &c); err != nil {
			t.Fatal(err)
		}
		if c.Token != []string{"token-one", "token-two"}[idx] {
			t.Fatal("instance credentials overwritten")
		}
	}
}

func TestClaudeJoinArgsPreserveHarnessFlags(t *testing.T) {
	invite, name, rest, err := claudeJoinArgs([]string{"--invite", "url", "--agent-name", "Claude", "--", "--bg", "--name", "session"})
	if err != nil || invite != "url" || name != "Claude" || !reflect.DeepEqual(rest, []string{"--bg", "--name", "session"}) {
		t.Fatalf("parse: %s %s %v %v", invite, name, rest, err)
	}
	_, _, rest, err = claudeJoinArgs([]string{"--plugin-dir", "/plugins"})
	if err != nil || !reflect.DeepEqual(rest, []string{"--plugin-dir", "/plugins"}) {
		t.Fatal("existing launcher flags changed")
	}
	if _, _, _, err := claudeJoinArgs([]string{"--invite"}); err == nil {
		t.Fatal("missing invite accepted")
	}
}
