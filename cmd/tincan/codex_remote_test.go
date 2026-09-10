package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestCodexTargetValidationAndDetection(t *testing.T) {
	for _, endpoint := range []string{"unix://", "unix:///tmp/codex socket.sock", "ws://127.0.0.1:4500", "ws://[::1]:4500/", "wss://codex.example:443"} {
		if err := validateCodexTarget(codexTarget{Endpoint: endpoint}); err != nil {
			t.Fatal(endpoint, err)
		}
	}
	for _, endpoint := range []string{"unix://relative.sock", "ws://public.example:4500", "https://localhost:4500", "wss://user:token@host:443", "wss://host:443/?token=secret", "ws://localhost", "ws://localhost:4500/path"} {
		if validateCodexTarget(codexTarget{Endpoint: endpoint}) == nil {
			t.Fatal("unsafe or ambiguous endpoint", endpoint)
		}
	}
	cases := []struct {
		args               []string
		want, source, auth string
	}{
		{[]string{"/bin/codex", "--remote=ws://127.0.0.1:7777", "--remote-auth-token-env", "TOKEN"}, "ws://127.0.0.1:7777", "cli_remote", "TOKEN"},
		{[]string{"codex", "app-server", "--listen", "ws://0.0.0.0:7777"}, "ws://127.0.0.1:7777", "app_server_listen", ""},
		{[]string{"codex", "app-server", "--listen=unix:///tmp/own.sock"}, "unix:///tmp/own.sock", "app_server_listen", ""},
		{[]string{"codex", "--remote", "unix://relative.sock"}, "unix:///tmp/relative.sock", "cli_remote", ""},
	}
	for _, c := range cases {
		target, ok := targetFromArgs(c.args, "/tmp")
		if !ok || target.Endpoint != c.want || target.Source != c.source || target.TokenEnv != c.auth {
			t.Fatal(target, ok)
		}
	}
	for _, args := range [][]string{{"shell", "--remote", "ws://127.0.0.1:5"}, {"codex", "--", "--remote", "ws://127.0.0.1:5"}, {"codex", "app-server", "--listen", "stdio://"}} {
		if _, ok := targetFromArgs(args, "/tmp"); ok {
			t.Fatal("guessed endpoint", args)
		}
	}
	target, ok := targetFromArgs([]string{"codex", "app-server", "--listen", "ws://127.0.0.1:8", "--ws-token-file", "bearer.txt"}, "/tmp")
	if !ok || target.TokenFile != "/tmp/bearer.txt" {
		t.Fatal(target)
	}
	t.Setenv("TINCAN_CODEX_REMOTE", "ws://127.0.0.1:7777")
	t.Setenv("TINCAN_CODEX_REMOTE_AUTH_TOKEN_ENV", "TOKEN")
	found, err := discoverCodexTarget(context.Background())
	if err != nil || found.Endpoint != "ws://127.0.0.1:7777" {
		t.Fatal(found, err)
	}
}

type fakeCodex struct {
	server    *httptest.Server
	mu        sync.Mutex
	methods   []string
	queued    []string
	auth      string
	wrongTask bool
	denyTurn  bool
	denyQueue bool
}

func newFakeCodex(t *testing.T) *fakeCodex {
	t.Helper()
	f := &fakeCodex{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.auth != "" && r.Header.Get("Authorization") != "Bearer "+f.auth {
			http.Error(w, "unauthorized", 401)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var msg codexMessage
			if err = json.Unmarshal(data, &msg); err != nil {
				t.Error(err)
				return
			}
			f.mu.Lock()
			f.methods = append(f.methods, msg.Method)
			f.mu.Unlock()
			if len(msg.ID) == 0 {
				continue
			}
			var params map[string]any
			_ = json.Unmarshal(msg.Params, &params)
			result := map[string]any{}
			var serverErr string
			switch msg.Method {
			case "initialize":
				result = map[string]any{"userAgent": "fake-codex/1", "platformFamily": "unix", "platformOs": "macos", "codexHome": os.TempDir()}
			case "thread/loaded/list":
				ids := []string{testCodexThread}
				if f.wrongTask {
					ids = []string{"other"}
				}
				result["data"] = ids
			case "turn/start":
				if f.denyTurn {
					serverErr = "toolOutput not available"
				} else {
					result["turn"] = map[string]any{"id": "turn1"}
				}
			case "thread/queue/list":
				if f.denyQueue {
					serverErr = "unsupported queue"
				} else {
					result["data"] = []any{}
				}
			case "thread/queue/add":
				if params["threadId"] != testCodexThread {
					t.Error("queue routed to wrong task")
					return
				}
				text := params["input"].([]any)[0].(map[string]any)["text"].(string)
				f.mu.Lock()
				f.queued = append(f.queued, text)
				f.mu.Unlock()
				result["queuedSubmission"] = map[string]any{"id": "queue1", "clientUserMessageId": params["clientUserMessageId"], "input": params["input"]}
			case "mcpServer/event/stream/start":
				serverErr = "MCP event subscriptions are only supported for hosted apps"
			default:
				serverErr = "unsupported method " + msg.Method
			}
			response := map[string]any{"id": msg.ID, "result": result}
			if serverErr != "" {
				delete(response, "result")
				response["error"] = map[string]any{"code": -32601, "message": serverErr}
			}
			encoded, _ := json.Marshal(response)
			if err = conn.Write(ctx, websocket.MessageText, encoded); err != nil {
				return
			}
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}
func (f *fakeCodex) target() codexTarget {
	return codexTarget{Endpoint: "ws" + strings.TrimPrefix(f.server.URL, "http"), Source: "connect_argument"}
}
func TestCodexWebSocketAuthAndOwnership(t *testing.T) {
	f := newFakeCodex(t)
	f.auth = "test-bearer-value"
	target := f.target()
	target.TokenEnv = "TINCAN_TEST_REMOTE_TOKEN"
	t.Setenv(target.TokenEnv, f.auth)
	if err := codexCallTarget(context.Background(), target, testCodexThread, map[string]any{"kind": "mention"}); err != nil {
		t.Fatal(err)
	}
	if got := probeCodexEventsTarget(context.Background(), target, testCodexThread); got != "unavailable_for_local_plugins" {
		t.Fatal(got)
	}
	other := newFakeCodex(t)
	other.wrongTask = true
	if err := codexCallTarget(context.Background(), other.target(), testCodexThread, map[string]any{"kind": "mention"}); err == nil {
		t.Fatal("delivered into wrong runtime")
	}
	other.mu.Lock()
	defer other.mu.Unlock()
	for _, method := range other.methods {
		if method == "turn/start" {
			t.Fatal("woke wrong runtime")
		}
	}
}
func TestCodexWebSocketNeverForwardsAuthOnRedirect(t *testing.T) {
	destination := newFakeCodex(t)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.server.URL, http.StatusFound)
	}))
	defer redirect.Close()
	t.Setenv("TINCAN_TEST_REDIRECT_TOKEN", "secret")
	_, close, err := openCodexTarget(context.Background(), codexTarget{Endpoint: "ws" + strings.TrimPrefix(redirect.URL, "http"), TokenEnv: "TINCAN_TEST_REDIRECT_TOKEN"})
	if err == nil {
		close()
		t.Fatal("followed websocket redirect")
	}
	destination.mu.Lock()
	defer destination.mu.Unlock()
	if len(destination.methods) != 0 {
		t.Fatal("sent data to redirected endpoint")
	}
}
func TestCodexQueueFallbackAndDurableDedup(t *testing.T) {
	root, c, path := hookFixture(t)
	calls := 0
	b := &pluginBroker{root: root, host: "codex", codexSend: func(context.Context, string, map[string]any) error { return errors.New("push rejected") }, codexQueueProbe: func(context.Context, *pluginConnection) error { return nil }, codexQueue: func(context.Context, *pluginConnection, int64) error { calls++; return nil }}
	b.setDelivery(c.Handle, deliveryState{Method: "codex_app_server"})
	before, _ := os.ReadFile(path)
	payload := map[string]any{"kind": "mention", "event_seq": int64(7)}
	if err := b.deliver(context.Background(), c, payload); err != nil {
		t.Fatal(err)
	}
	if got := b.deliverySnapshot(c.Handle); got.Method != "codex_queue" || !got.IdleWake {
		t.Fatal(got)
	}
	// Simulate plugin restart. A CLI invocation always chooses a fresh UUID, so
	// the saved receipt must prevent submitting the same nudge a second time.
	b.deliveries = nil
	if err := b.deliver(context.Background(), c, payload); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("duplicate queue invocation", calls)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("queue acceptance acknowledged unfinished work")
	}
}
func TestCodexQueueUncertainFailureRetainsInbox(t *testing.T) {
	root, c, path := hookFixture(t)
	calls := 0
	b := &pluginBroker{root: root, codexQueueProbe: func(context.Context, *pluginConnection) error { return nil }, codexQueue: func(context.Context, *pluginConnection, int64) error {
		calls++
		return errors.New("connection closed after submission")
	}}
	if b.queueCodex(context.Background(), c, 7) == nil {
		t.Fatal("failed submission succeeded")
	}
	if b.queueCodex(context.Background(), c, 7) == nil || calls != 1 {
		t.Fatal("retried uncertain submission", calls)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"pending"`) {
		t.Fatal("pending lost")
	}
}
func TestCodexTargetCredentialsNeverPersistValues(t *testing.T) {
	root, c, _ := hookFixture(t)
	t.Setenv("TINCAN_TEST_TOKEN", "private-value")
	b := &pluginBroker{root: root}
	target := codexTarget{Endpoint: "wss://server.example:443", TokenEnv: "TINCAN_TEST_TOKEN", Source: "connect_argument"}
	if err := b.selectCodexTarget(context.Background(), c, &target); err != nil {
		t.Fatal(err)
	}
	if err := b.save(c); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, c.Handle+".json"))
	if strings.Contains(string(data), "private-value") {
		t.Fatal("persisted bearer value")
	}
	if token, err := target.token(); err != nil || token != "private-value" {
		t.Fatal("credential reference failed", err)
	}
}

// Opt in to testing the installed CLI against a simulated server; no real
// Codex task is created, resumed, messaged or run by this conformance test.
func TestInstalledCodexQueueProtocol(t *testing.T) {
	if os.Getenv("TINCAN_TEST_CODEX_QUEUE") == "" {
		t.Skip("set TINCAN_TEST_CODEX_QUEUE=1 to exercise the installed CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("Codex CLI unavailable")
	}
	f := newFakeCodex(t)
	f.auth = "local-test-bearer"
	target := f.target()
	target.TokenEnv = "TINCAN_TEST_QUEUE_TOKEN"
	t.Setenv(target.TokenEnv, f.auth)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := codexQueueProbe(ctx, target, testCodexThread); err != nil {
		t.Fatal(err)
	}
	if err := codexQueueCommand(ctx, target, testCodexThread, 7); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queued) != 1 || !strings.Contains(f.queued[0], "event 7") || strings.Contains(f.queued[0], f.auth) {
		t.Fatal("invalid queued transport notification")
	}
	for _, method := range f.methods {
		if method == "thread/start" || method == "thread/resume" || method == "turn/start" {
			t.Fatal("queue must not create or resume a task", method)
		}
	}
}

func TestCodexQueueRejectsWrongOwnerBeforeCLI(t *testing.T) {
	f := newFakeCodex(t)
	f.wrongTask = true
	root, c, _ := hookFixture(t)
	target := f.target()
	c.CodexTarget = &target
	called := false
	b := &pluginBroker{root: root, codexQueue: func(context.Context, *pluginConnection, int64) error { called = true; return nil }}
	if err := b.queueCodex(context.Background(), c, 7); err == nil || called {
		t.Fatal("queue escaped task ownership check", err)
	}
	if _, err := os.Stat(queuePath(root, c.Handle)); !os.IsNotExist(err) {
		t.Fatal("recorded an unverified submission", err)
	}
}
func TestCodexWebSocketRejectsUntrustedTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("TLS validation was bypassed") }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, close, err := openCodexTarget(ctx, codexTarget{Endpoint: "wss" + strings.TrimPrefix(server.URL, "https")})
	if err == nil {
		close()
		t.Fatal("accepted untrusted TLS")
	}
}

func TestInstalledCodexCustomUnixSocket(t *testing.T) {
	if os.Getenv("TINCAN_TEST_CODEX_QUEUE") == "" || runtime.GOOS == "windows" {
		t.Skip("requires installed Codex and Unix sockets")
	}
	f := newFakeCodex(t)
	dir, err := os.MkdirTemp("/tmp", "tincan-sock-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "control.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(f.server.Config.Handler)
	server.Listener.Close()
	server.Listener = listener
	server.Start()
	defer server.Close()
	target := codexTarget{Endpoint: "unix://" + path, Source: "connect_argument"}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err = codexCallTarget(ctx, target, testCodexThread, map[string]any{"kind": "mention"}); err != nil {
		t.Fatal(err)
	}
	if err = codexQueueProbe(ctx, target, testCodexThread); err != nil {
		t.Fatal(err)
	}
	if err = codexQueueCommand(ctx, target, testCodexThread, 7); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queued) != 1 {
		t.Fatal("custom socket queue did not receive nudge")
	}
}
func TestInstalledCodexQueueFallback(t *testing.T) {
	if os.Getenv("TINCAN_TEST_CODEX_QUEUE") == "" {
		t.Skip("requires installed Codex")
	}
	f := newFakeCodex(t)
	f.denyTurn = true
	root, c, path := hookFixture(t)
	target := f.target()
	c.CodexTarget = &target
	b := &pluginBroker{root: root, host: "codex"}
	if err := b.save(c); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	b.probeDelivery(context.Background(), c)
	if err := b.deliver(context.Background(), c, map[string]any{"kind": "mention", "event_seq": int64(7)}); err != nil {
		t.Fatal(err)
	}
	if s := b.deliverySnapshot(c.Handle); s.Method != "codex_queue" || s.QueueError != "" {
		t.Fatal(s)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("fallback acknowledged unfinished work")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queued) != 1 {
		t.Fatal("queue fallback not delivered")
	}
}
