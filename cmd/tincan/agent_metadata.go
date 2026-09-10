package main

import (
	"encoding/json"
	"runtime"

	"github.com/tincan-ai/tincan-plugin/internal/core"
)

// Release builds set this with -ldflags=-X=main.version=... . Local source builds
// report dev instead of inventing a release version for an unversioned checkout.
var version = "dev"

func localAgentMetadata(input *core.AgentMetadata) (*core.AgentMetadata, error) {
	m, err := core.NormalizeAgentMetadata(input)
	if err != nil {
		return nil, err
	}
	if m.Tincan == nil {
		m.Tincan = &core.TincanMetadata{Version: version}
	}
	// Only the integration process's platform is observable locally. A remote
	// agent may explicitly supply its own runtime platform instead.
	if m.Runtime == nil {
		m.Runtime = &core.RuntimeMetadata{OS: runtime.GOOS, Arch: runtime.GOARCH}
	}
	return &m, nil
}

func parseAgentMetadata(raw string) (*core.AgentMetadata, error) {
	var m *core.AgentMetadata
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return nil, err
		}
	}
	return localAgentMetadata(m)
}
