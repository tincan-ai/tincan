package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/tincan-ai/tincan-plugin/internal/core"
	"os"
	"os/exec"
	"path/filepath"
)

// claude only configures the local channel. Claude owns session scheduling,
// permissions, background mode, and the development-channel consent dialog.
func claudeCommand(args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Usage: tincan claude [--invite URL --agent-name NAME --] [Claude Code flags]\nWith --invite, joins as a new globally unique agent with an isolated credential file. Without it, resumes the configured agent.\nSet TINCAN_CONFIG, TINCAN_SERVER and TINCAN_ALLOW_SENDERS first.\nLaunches Claude with Tincan's native channel. Pass --bg for Claude's background mode. The installed plugin uses its own connection flow. Existing Claude sessions are not modified.")
		return nil
	}
	invite, name, claudeArgs, err := claudeJoinArgs(args)
	if err != nil {
		return err
	}
	args = claudeArgs
	c := load()
	if invite != "" {
		c, err = joinClaude(c, name, invite)
		if err != nil {
			return err
		}
	}
	if c.Token == "" {
		return errors.New("connect first using a separate TINCAN_CONFIG for this agent")
	}
	// Check identity and sender configuration, then release the lock before the
	// subprocess opens its own inbox. No provider API or model is invoked here.
	i, err := openInbox(c, os.Getenv("TINCAN_ALLOW_SENDERS"))
	if err != nil {
		return err
	}
	i.close()
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	// Keep the config alongside the credential: background Claude sessions may
	// restart their MCP process after this launcher has exited. No secrets here.
	config := map[string]any{"mcpServers": map[string]any{"tincan-live": map[string]any{"command": binary, "args": []string{"mcp"}, "env": map[string]string{"TINCAN_WAKE": "claude", "TINCAN_CONFIG": mustAbs(configPath()), "TINCAN_SERVER": c.Server, "TINCAN_ALLOW_SENDERS": os.Getenv("TINCAN_ALLOW_SENDERS")}}}}
	data, _ := json.MarshalIndent(config, "", "  ")
	path := i.path + ".claude-mcp.json"
	if err = os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	command := exec.Command("claude", append([]string{"--mcp-config", path, "--dangerously-load-development-channels", "server:tincan-live"}, args...)...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	// The standalone bridge owns this configured identity and its native inbox.
	// Do not enable a second consumer through an inherited wake setting.
	command.Env = append(os.Environ(), "TINCAN_WAKE=")
	fmt.Fprintln(os.Stderr, "Tincan: starting Claude with native channel delivery. Claude will request development-channel consent.")
	return command.Run()
}
func mustAbs(path string) string {
	p, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return p
}

// Only consume Tincan's leading options. Everything after -- or the first
// Claude option is passed through unchanged to the harness.
func claudeJoinArgs(args []string) (invite, name string, rest []string, err error) {
	name = "Claude"
	for len(args) > 0 {
		switch args[0] {
		case "--":
			return invite, name, args[1:], nil
		case "--invite", "--agent-name":
			if len(args) < 2 || args[1] == "" {
				return "", "", nil, fmt.Errorf("%s requires a value", args[0])
			}
			if args[0] == "--invite" {
				invite = args[1]
			} else {
				name = args[1]
			}
			args = args[2:]
		default:
			return invite, name, args, nil
		}
	}
	return invite, name, nil, nil
}

func joinClaude(c Config, name, invite string) (Config, error) {
	// Scope both the live bridge and any ordinary plugin bridge in the child to
	// this join. Never overwrite another running instance's credential file.
	path := filepath.Join(filepath.Dir(configPath()), "instances", core.ID("instance_")+".json")
	if err := os.Setenv("TINCAN_CONFIG", path); err != nil {
		return c, err
	}
	if err := os.Unsetenv("TINCAN_TOKEN"); err != nil {
		return c, err
	}
	c.Token = ""
	if err := bootstrap(&c, name, "", invite, ""); err != nil {
		return c, err
	}
	return c, nil
}
