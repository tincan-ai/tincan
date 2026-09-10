package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/coder/websocket"
)

type codexWebSocketWriter struct {
	ctx  context.Context
	conn *websocket.Conn
}

func (w codexWebSocketWriter) Write(p []byte) (int, error) {
	err := w.conn.Write(w.ctx, websocket.MessageText, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}
func openCodexTarget(ctx context.Context, t codexTarget) (*codexRPC, func(), error) {
	if err := validateCodexTarget(t); err != nil {
		return nil, nil, err
	}
	endpoint := t.Endpoint
	var transport *http.Transport
	if strings.HasPrefix(endpoint, "unix://") {
		socket := strings.TrimPrefix(endpoint, "unix://")
		if socket == "" {
			// Canonical path from Codex app-server-transport's public implementation;
			// use the configured Codex home, never search for candidate sockets.
			codexDir := os.Getenv("CODEX_HOME")
			if codexDir == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return nil, nil, err
				}
				codexDir = filepath.Join(home, ".codex")
			}
			socket = filepath.Join(codexDir, "app-server-control", "app-server-control.sock")
		}
		transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}}
		endpoint = "ws://localhost"
	}
	token, err := t.token()
	if err != nil {
		return nil, nil, err
	}
	headers := http.Header{}
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	// Never send credentials across redirects, and retain normal TLS verification.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if transport != nil {
		client.Transport = transport
	}
	conn, resp, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: headers, HTTPClient: client})
	if err != nil {
		if transport != nil {
			transport.CloseIdleConnections()
		}
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		return nil, nil, err
	}
	conn.SetReadLimit(8 * 1024 * 1024)
	r := newCodexRPCReceiver(codexWebSocketWriter{ctx, conn}, func() ([]byte, error) { _, data, err := conn.Read(ctx); return data, err })
	return r, func() {
		r.shutdown()
		conn.CloseNow()
		if transport != nil {
			transport.CloseIdleConnections()
		}
	}, nil
}
