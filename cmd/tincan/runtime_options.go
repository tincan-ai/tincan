package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
)

// defaultServer is set by the release builder; source builds use the local service.
var defaultServer = "http://localhost:8080"

// Runtime configuration is independent of the global CLI token/config: a
// shared host must still create a separate connection for each logical agent.
func runtimeServer() string {
	endpoint := os.Getenv("TINCAN_SERVER")
	if endpoint == "" {
		endpoint = defaultServer
	}
	return strings.TrimRight(endpoint, "/")
}

func runtimeBroker(mode string, args []string) (*pluginBroker, error) {
	endpoint := runtimeServer()
	host := "agent"
	if mode == "sidecar" {
		host = "custom"
	}
	f := flag.NewFlagSet(mode, flag.ContinueOnError)
	f.StringVar(&endpoint, "server", endpoint, "Tincan server origin (TINCAN_SERVER)")
	f.StringVar(&host, "host", host, "Host name, such as grok-bot, muse, or instinct")
	stateDir := f.String("state-dir", os.Getenv("TINCAN_STATE_DIR"), "Private durable connection/inbox directory (TINCAN_STATE_DIR)")
	var native bool
	if mode == "plugin" {
		f.BoolVar(&native, "claude-channel", false, "Advertise native Claude channel delivery; Claude requires host opt-in")
	}
	if err := f.Parse(args); err != nil {
		return nil, err
	}
	if f.NArg() != 0 {
		return nil, errors.New("unexpected positional arguments")
	}
	endpoint = strings.TrimRight(endpoint, "/")
	if err := validateServer(endpoint); err != nil {
		return nil, err
	}
	root, err := runtimeStateDirectory(*stateDir)
	if err != nil {
		return nil, err
	}
	return &pluginBroker{root: root, server: endpoint, host: host, native: native}, nil
}

func runtimeStateDirectory(stateDir string) (string, error) {
	if stateDir == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		stateDir = filepath.Join(dir, "tincan", "connections")
	}
	// Resolve once: later working-directory changes must not select a new vault.
	return filepath.Abs(stateDir)
}
