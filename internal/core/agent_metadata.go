package core

import (
	"bytes"

	"encoding/json"

	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// AgentMetadata is an allowlisted, self-reported analytics snapshot. It is kept
// separate from Agent so peer discovery, announcements and exports cannot expose it.
type AgentMetadata struct {
	SchemaVersion int              `json:"metadata_schema_version,omitempty" jsonschema:"Schema version; use 1 or omit"`
	Harness       *HarnessMetadata `json:"harness,omitempty" jsonschema:"Agent harness name and exact version if exposed"`
	Model         *ModelMetadata   `json:"model,omitempty" jsonschema:"Model identity reported by this task's runtime; never infer a snapshot from an alias"`
	Tincan        *TincanMetadata  `json:"tincan,omitempty" jsonschema:"Tincan integration version, separate from the harness"`
	Runtime       *RuntimeMetadata `json:"runtime,omitempty" jsonschema:"Agent runtime operating system and architecture; no hostnames or paths"`
	ExecutionMode string           `json:"execution_mode,omitempty" jsonschema:"interactive, unattended, or ci; omit if unknown"`
	Capabilities  []string         `json:"capabilities,omitempty" jsonschema:"Known supported features such as attachments or background_listening; at most 32 short labels"`
}

type HarnessMetadata struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type ModelMetadata struct {
	Provider        string `json:"provider,omitempty"`
	ID              string `json:"id,omitempty"`
	Version         string `json:"version,omitempty" jsonschema:"Exact model release only when exposed; omit if unknown"`
	IDKind          string `json:"id_kind,omitempty" jsonschema:"alias or snapshot when known; omit if unknown"`
	ReasoningEffort string `json:"reasoning_effort,omitempty" jsonschema:"Current configured effort if applicable and exposed"`
}

type TincanMetadata struct {
	Version string `json:"version,omitempty"`
}

type RuntimeMetadata struct {
	OS   string `json:"os,omitempty"`
	Arch string `json:"arch,omitempty"`
}

// Reject arbitrary context and accidental private fields at every nesting level.
func (m *AgentMetadata) UnmarshalJSON(data []byte) error {
	type plain AgentMetadata
	var value plain
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if len(data) > 8192 || !utf8.Valid(data) || decoder.Decode(&value) != nil {
		return Fail(400, "invalid_agent_metadata", "Use only the documented agent metadata fields, within 8 KB.")
	}
	*m = AgentMetadata(value)
	return nil
}

// NormalizeAgentMetadata copies its input and canonicalizes empty values so
// unknowns and reordered capabilities do not produce misleading change events.
func NormalizeAgentMetadata(input *AgentMetadata) (AgentMetadata, error) {
	m := AgentMetadata{SchemaVersion: 1}
	if input == nil {
		return m, nil
	}
	data, err := json.Marshal(input)
	if err != nil || len(data) > 8192 {
		return m, Fail(400, "invalid_agent_metadata", "Agent metadata must fit within 8 KB.")
	}

	m = *input
	if input.Harness != nil {
		value := *input.Harness
		m.Harness = &value
	}
	if input.Model != nil {
		value := *input.Model
		m.Model = &value
	}
	if input.Tincan != nil {
		value := *input.Tincan
		m.Tincan = &value
	}
	if input.Runtime != nil {
		value := *input.Runtime
		m.Runtime = &value
	}
	if m.SchemaVersion == 0 {
		m.SchemaVersion = 1
	}
	if m.SchemaVersion != 1 {
		return m, Fail(400, "invalid_agent_metadata", "Supported metadata_schema_version is 1.")
	}
	fields := []*string{&m.ExecutionMode}
	if m.Harness != nil {
		fields = append(fields, &m.Harness.Name, &m.Harness.Version)
	}
	if m.Model != nil {
		fields = append(fields, &m.Model.Provider, &m.Model.ID, &m.Model.Version, &m.Model.IDKind, &m.Model.ReasoningEffort)
	}
	if m.Tincan != nil {
		fields = append(fields, &m.Tincan.Version)
	}
	if m.Runtime != nil {
		fields = append(fields, &m.Runtime.OS, &m.Runtime.Arch)
	}
	for _, field := range fields {
		*field = strings.TrimSpace(*field)
		if !utf8.ValidString(*field) || len(*field) > 128 || strings.IndexFunc(*field, unicode.IsControl) >= 0 {
			return m, Fail(400, "invalid_agent_metadata", "Metadata values must be short text labels of at most 128 bytes, without control characters.")
		}
	}
	if m.ExecutionMode != "" && m.ExecutionMode != "interactive" && m.ExecutionMode != "unattended" && m.ExecutionMode != "ci" {
		return m, Fail(400, "invalid_agent_metadata", "execution_mode must be interactive, unattended, or ci.")
	}
	if m.Model != nil && m.Model.IDKind != "" && m.Model.IDKind != "alias" && m.Model.IDKind != "snapshot" {
		return m, Fail(400, "invalid_agent_metadata", "model.id_kind must be alias or snapshot when known.")
	}
	if len(m.Capabilities) > 32 {
		return m, Fail(400, "invalid_agent_metadata", "Report at most 32 capabilities.")
	}
	caps := make([]string, 0, len(m.Capabilities))
	seen := map[string]bool{}
	for _, capability := range m.Capabilities {
		capability = strings.TrimSpace(capability)
		if len(capability) > 64 || strings.Trim(capability, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-") != "" {
			return m, Fail(400, "invalid_agent_metadata", "Capabilities must be labels of at most 64 letters, digits, dots, underscores or hyphens.")
		}
		if capability != "" && !seen[capability] {
			caps = append(caps, capability)
			seen[capability] = true
		}
	}
	sort.Strings(caps)
	m.Capabilities = caps
	if m.Harness != nil && *m.Harness == (HarnessMetadata{}) {
		m.Harness = nil
	}
	if m.Model != nil && *m.Model == (ModelMetadata{}) {
		m.Model = nil
	}
	if m.Tincan != nil && *m.Tincan == (TincanMetadata{}) {
		m.Tincan = nil
	}
	if m.Runtime != nil && *m.Runtime == (RuntimeMetadata{}) {
		m.Runtime = nil
	}
	return m, nil
}
