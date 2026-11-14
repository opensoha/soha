package sohacli

import (
	"encoding/json"
	"io"

	contractresource "github.com/opensoha/soha/contracts/resource"
)

const maxTUIReferences = 128

// Recovery metadata never includes message content, request bodies or credentials.
// It survives context changes and is printed after leaving the alternate screen.
type tuiRequestReference struct {
	Kind          string                                          `json:"kind"`
	Profile       string                                          `json:"profile"`
	Server        string                                          `json:"server"`
	Subject       string                                          `json:"subject"`
	Cluster       string                                          `json:"cluster,omitempty"`
	Namespace     string                                          `json:"namespace,omitempty"`
	Target        string                                          `json:"target,omitempty"`
	RequestID     string                                          `json:"requestId,omitempty"`
	SessionID     string                                          `json:"sessionId,omitempty"`
	RunID         string                                          `json:"runId,omitempty"`
	Preconditions *contractresource.ResourceMutationPreconditions `json:"preconditions,omitempty"`
}

func (m *tuiModel) rememberReference(reference tuiRequestReference) bool {
	if len(m.references) >= maxTUIReferences {
		m.failed, m.message = true, "Reference limit reached. Detach to save the original references before starting another TUI session."
		return false
	}
	reference.Profile, reference.Server, reference.Subject = m.profile, m.origin, m.subject
	m.references = append(m.references, reference)
	return true
}

func (m *tuiModel) rememberRunID(runID string) {
	for index := len(m.references) - 1; index >= 0; index-- {
		reference := &m.references[index]
		if reference.Kind == "ai-turn" && reference.RequestID == m.requestID && reference.Server == m.origin && reference.Subject == m.subject {
			reference.RunID = runID
			return
		}
	}
}

func (m *tuiModel) writeRecoveryReferences(out io.Writer) error {
	for _, reference := range m.references {
		if err := json.NewEncoder(out).Encode(struct {
			Source    string              `json:"source"`
			Reference tuiRequestReference `json:"reference"`
		}{Source: "tui-original-request", Reference: reference}); err != nil {
			return err
		}
	}
	return nil
}
