package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// A target belongs to a connection, not to a globally selected agent. Auth
// references are saved privately; bearer values never enter tool results.
type codexTarget struct {
	Endpoint  string `json:"endpoint"`
	TokenEnv  string `json:"token_env,omitempty"`
	TokenFile string `json:"token_file,omitempty"`
	Source    string `json:"source"`
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func defaultCodexTarget() codexTarget {
	return codexTarget{Endpoint: "unix://", Source: "default_socket"}
}
func validateCodexTarget(t codexTarget) error {
	if t.TokenEnv != "" && !envName.MatchString(t.TokenEnv) {
		return errors.New("Codex auth reference must be an environment variable name, never a token")
	}
	if t.TokenEnv != "" && t.TokenFile != "" {
		return errors.New("multiple Codex auth sources")
	}
	if strings.HasPrefix(t.Endpoint, "unix://") {
		path := strings.TrimPrefix(t.Endpoint, "unix://")
		if strings.ContainsAny(path, "\x00\r\n?#") || (path != "" && !filepath.IsAbs(path)) {
			return errors.New("Codex socket path must be absolute")
		}
		if t.TokenEnv != "" || t.TokenFile != "" {
			return errors.New("Unix socket targets do not use bearer auth")
		}
		return nil
	}
	u, err := url.Parse(t.Endpoint)
	if err != nil || u.User != nil || u.Hostname() == "" || u.Port() == "" || (u.Scheme != "ws" && u.Scheme != "wss") || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return errors.New("Codex endpoint must be unix://, unix:///absolute/path, or ws(s)://host:port without credentials, query, or fragment")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme == "ws" && u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("non-local Codex WebSockets require wss://")
	}
	if t.TokenFile != "" && !filepath.IsAbs(t.TokenFile) {
		return errors.New("Codex token-file reference must be absolute")
	}
	return nil
}
func (t codexTarget) token() (string, error) {
	var token string
	if t.TokenEnv != "" {
		token = os.Getenv(t.TokenEnv)
	}
	if t.TokenFile != "" {
		f, err := os.Open(t.TokenFile)
		if err != nil {
			return "", errors.New("Codex endpoint credential file is unavailable")
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, 16*1024+1))
		if err != nil || len(b) > 16*1024 {
			return "", errors.New("Codex endpoint credential file is invalid")
		}
		token = strings.TrimSpace(string(b))
	}
	if (t.TokenEnv != "" || t.TokenFile != "") && token == "" {
		return "", errors.New("Codex endpoint credential is unavailable in this runtime")
	}
	if len(token) > 16*1024 || strings.ContainsAny(token, "\r\n\x00") {
		return "", errors.New("invalid Codex endpoint credential")
	}
	return token, nil
}
func argvFlag(args []string, name string) string {
	for i, arg := range args {
		if arg == "--" {
			break
		}
		if strings.HasPrefix(arg, name+"=") {
			return strings.TrimPrefix(arg, name+"=")
		}
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
func targetFromArgs(args []string, cwd string) (codexTarget, bool) {
	if len(args) == 0 {
		return codexTarget{}, false
	}
	exe := strings.TrimSuffix(strings.ToLower(filepath.Base(args[0])), ".exe")
	if exe != "codex" && !strings.HasPrefix(exe, "codex-") {
		return codexTarget{}, false
	}
	remote := argvFlag(args[1:], "--remote")
	if remote != "" {
		if strings.HasPrefix(remote, "unix://") && remote != "unix://" && !filepath.IsAbs(strings.TrimPrefix(remote, "unix://")) {
			remote = "unix://" + filepath.Join(cwd, strings.TrimPrefix(remote, "unix://"))
		}
		return codexTarget{Endpoint: remote, TokenEnv: argvFlag(args[1:], "--remote-auth-token-env"), Source: "cli_remote"}, true
	}
	appServer := false
	for _, arg := range args[1:] {
		if arg == "app-server" {
			appServer = true
			break
		}
	}
	if !appServer {
		return codexTarget{}, false
	}
	listen := argvFlag(args[1:], "--listen")
	if listen == "" || listen == "stdio://" || listen == "off" {
		return codexTarget{}, false
	}
	// A wildcard bind address denotes this ancestor's local listener, not a
	// destination to scan. Only the exact declared port is used.
	if u, err := url.Parse(listen); err == nil && (u.Scheme == "ws" || u.Scheme == "wss") {
		if u.Hostname() == "0.0.0.0" {
			u.Host = net.JoinHostPort("127.0.0.1", u.Port())
			listen = u.String()
		}
		if u.Hostname() == "::" {
			u.Host = net.JoinHostPort("::1", u.Port())
			listen = u.String()
		}
	}
	tokenFile := argvFlag(args[1:], "--ws-token-file")
	if tokenFile != "" && !filepath.IsAbs(tokenFile) {
		tokenFile = filepath.Join(cwd, tokenFile)
	}
	return codexTarget{Endpoint: listen, TokenFile: tokenFile, Source: "app_server_listen"}, true
}
func discoverCodexTarget(ctx context.Context) (codexTarget, error) {
	if endpoint := os.Getenv("TINCAN_CODEX_REMOTE"); endpoint != "" {
		t := codexTarget{Endpoint: endpoint, TokenEnv: os.Getenv("TINCAN_CODEX_REMOTE_AUTH_TOKEN_ENV"), Source: "tincan_environment"}
		return t, validateCodexTarget(t)
	}
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	pid := int32(os.Getppid())
	for depth := 0; depth < 16 && pid > 1 && ctx.Err() == nil; depth++ {
		p, err := process.NewProcessWithContext(ctx, pid)
		if err != nil {
			break
		}
		args, err := p.CmdlineSliceWithContext(ctx)
		if err == nil {
			cwd, _ := p.CwdWithContext(ctx)
			if t, ok := targetFromArgs(args, cwd); ok {
				return t, validateCodexTarget(t)
			}
		}
		parent, err := p.PpidWithContext(ctx)
		if err != nil || parent == pid {
			break
		}
		pid = parent
	}
	return defaultCodexTarget(), nil
}
func (b *pluginBroker) targetFor(c *pluginConnection) (codexTarget, error) {
	if c.CodexTarget != nil {
		return *c.CodexTarget, validateCodexTarget(*c.CodexTarget)
	}
	return defaultCodexTarget(), nil
}
func (b *pluginBroker) selectCodexTarget(ctx context.Context, c *pluginConnection, explicit *codexTarget) error {
	if explicit != nil {
		c.CodexTarget = explicit
	} else if c.CodexTarget == nil || c.CodexTarget.Source != "connect_argument" {
		t, err := discoverCodexTarget(ctx)
		if err != nil {
			t = codexTarget{Source: "discovery_unavailable"}
		}
		if c.CodexTarget == nil || t.Source != "default_socket" || c.CodexTarget.Source == "default_socket" {
			c.CodexTarget = &t
		}
	}
	if explicit != nil {
		return validateCodexTarget(*c.CodexTarget)
	}
	return nil
}

// The standard MCP entry is shared by portable clients. Select Codex delivery
// only when Codex is actually an ancestor, not from a shared task-ID variable.
func launchedByCodex() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	pid := int32(os.Getppid())
	for depth := 0; depth < 16 && pid > 1 && ctx.Err() == nil; depth++ {
		p, err := process.NewProcessWithContext(ctx, pid)
		if err != nil {
			return false
		}
		exe, err := p.ExeWithContext(ctx)
		if err == nil && isCodexExecutable(exe) {
			return true
		}
		parent, err := p.PpidWithContext(ctx)
		if err != nil || parent == pid {
			return false
		}
		pid = parent
	}
	return false
}
func isCodexExecutable(exe string) bool {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(exe)), ".exe")
	return name == "codex" || name == "codex-app-server"
}
