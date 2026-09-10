package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

type boundedLog struct {
	mu   sync.Mutex
	text string
}

func (b *boundedLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.text += string(p)
	if len(b.text) > 4096 {
		b.text = b.text[len(b.text)-4096:]
	}
	return len(p), nil
}
func (b *boundedLog) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(b.text)
}

type codexRPC struct {
	diagnostics func() string
	in          io.Writer
	messages    chan codexRead
	done        chan struct{}
	once        sync.Once
	next        int
	handle      func(string, json.RawMessage) (any, error)
	notice      func(string, json.RawMessage)
}
type codexMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type codexRead struct {
	message codexMessage
	err     error
}

func newCodexRPC(in io.Writer, out io.Reader) *codexRPC {
	s := bufio.NewScanner(out)
	s.Buffer(make([]byte, 4096), 8*1024*1024)
	return newCodexRPCReceiver(in, func() ([]byte, error) {
		if s.Scan() {
			return append([]byte(nil), s.Bytes()...), nil
		}
		if err := s.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	})
}
func newCodexRPCReceiver(in io.Writer, receive func() ([]byte, error)) *codexRPC {
	r := &codexRPC{in: in, messages: make(chan codexRead, 16), done: make(chan struct{})}
	go func() {
		defer close(r.messages)
		for {
			data, err := receive()
			var m codexMessage
			if err == io.EOF {
				return
			}
			if err == nil {
				err = json.Unmarshal(data, &m)
			}
			select {
			case r.messages <- codexRead{m, err}:
			case <-r.done:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return r
}
func (r *codexRPC) shutdown()         { r.once.Do(func() { close(r.done) }) }
func (r *codexRPC) write(v any) error { return json.NewEncoder(r.in).Encode(v) }
func (r *codexRPC) read() (codexMessage, error) {
	v, ok := <-r.messages
	if !ok {
		return codexMessage{}, errors.New("Codex control connection closed")
	}
	return r.dispatch(v)
}
func (r *codexRPC) dispatch(v codexRead) (codexMessage, error) {
	m := v.message
	if v.err != nil {
		return m, v.err
	}
	if m.Method != "" {
		if len(m.ID) > 0 {
			var v any
			err := errors.New("Tincan has no approval or input handler for this request")
			if r.handle != nil {
				v, err = r.handle(m.Method, m.Params)
			}
			reply := map[string]any{"id": m.ID, "result": v}
			if err != nil {
				delete(reply, "result")
				reply["error"] = map[string]any{"code": -32601, "message": err.Error()}
			}
			if err := r.write(reply); err != nil {
				return m, err
			}
		} else if r.notice != nil {
			r.notice(m.Method, m.Params)
		}
	}
	return m, nil
}
func (r *codexRPC) call(method string, params any) (json.RawMessage, error) {
	r.next++
	id := fmt.Sprint(r.next)
	if err := r.write(map[string]any{"id": r.next, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		m, err := r.read()
		if err != nil {
			return nil, err
		}
		if m.Method != "" || string(m.ID) != id {
			continue
		}
		if m.Error != nil {
			return nil, fmt.Errorf("Codex %s: %s", method, m.Error.Message)
		}
		if len(m.Result) == 0 {
			return nil, errors.New("Codex response has no result")
		}
		return m.Result, nil
	}
}
func (r *codexRPC) initialize() error {
	if _, err := r.call("initialize", map[string]any{"clientInfo": map[string]string{"name": "tincan", "version": "0.4.0"}, "capabilities": map[string]bool{"experimentalApi": true}}); err != nil {
		return err
	}
	return r.write(map[string]any{"method": "initialized"})
}
func startCodexRPC(ctx context.Context, bin string, args ...string) (*codexRPC, func(), *boundedLog, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	log := &boundedLog{}
	cmd.Stderr = log
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, log, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, nil, log, err
	}
	if err = cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, nil, log, err
	}
	r := newCodexRPC(in, out)
	r.diagnostics = log.String
	cleanup := func() { r.shutdown(); in.Close(); cmd.Process.Kill(); cmd.Wait() }
	return r, cleanup, log, nil
}
