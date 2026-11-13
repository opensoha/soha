package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type tuiContextInput struct {
	Profile   string `json:"profile"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
}
type tuiContextTarget struct {
	input           tuiContextInput
	backend         tuiBackend
	origin, subject string
}
type tuiContextLoader func(context.Context, tuiContextInput) (tuiContextTarget, error)
type tuiContextResult struct {
	epoch  uint64
	target tuiContextTarget
	err    error
}

func tuiRuntimeContextLoader(rt Runtime) tuiContextLoader {
	return func(ctx context.Context, input tuiContextInput) (tuiContextTarget, error) {
		_, selected, config, err := loadRuntimeProfile(ctx, rt, input.Profile)
		if err != nil {
			return tuiContextTarget{}, err
		}
		client := gatewayClient(rt, config)
		identity, err := client.AuthContext(ctx)
		if err != nil {
			return tuiContextTarget{}, err
		}
		if err := validateAuthContext(identity); err != nil {
			return tuiContextTarget{}, err
		}
		input.Profile = selected
		return tuiContextTarget{input: input, backend: tuiAPI{client: client}, origin: client.ServerURL, subject: identity.SubjectID}, nil
	}
}

func (m *tuiModel) beginContextEdit() {
	if m.loadContext == nil {
		m.message = "Context selection is unavailable"
		return
	}
	raw, _ := json.Marshal(tuiContextInput{Profile: m.profile, Cluster: m.scope.cluster, Namespace: m.scope.namespace})
	m.draft, m.composing, m.contextEditing = string(raw), true, true
	m.pendingContext = nil
}

func parseTUIContext(value string) (tuiContextInput, error) {
	var input tuiContextInput
	decoder := json.NewDecoder(bytes.NewBufferString(value))
	decoder.DisallowUnknownFields()
	if len(value) > 4096 || decoder.Decode(&input) != nil {
		return input, usageError{message: "context requires profile, cluster and namespace JSON"}
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return input, usageError{message: "context accepts one JSON object"}
	}
	if input.Profile == "" || len(input.Profile) > 128 || strings.ContainsAny(input.Profile, "\r\n\t") || input.Cluster != "" && !kubernetesSegment(input.Cluster) || !kubernetesSegment(input.Namespace) {
		return input, usageError{message: "context requires an existing profile and valid cluster/namespace names"}
	}
	return input, nil
}

func (m *tuiModel) stageContext() tea.Cmd {
	input, err := parseTUIContext(m.draft)
	if err != nil {
		m.message = err.Error()
		return nil
	}
	if m.loadContext == nil {
		m.message = "Context selection is unavailable"
		return nil
	}
	ctx, epoch := m.begin()
	loader := m.loadContext
	m.composing, m.contextEditing = false, false
	m.message = "Checking selected profile identity; local streams detached, tasks continue."
	return func() tea.Msg {
		target, err := loader(ctx, input)
		return tuiContextResult{epoch: epoch, target: target, err: err}
	}
}

func (m *tuiModel) applyContextResult(result tuiContextResult) {
	if result.epoch != m.epoch {
		return
	}
	m.busy = false
	if result.err != nil {
		m.failed, m.message = true, tuiText(result.err.Error())
		return
	}
	if result.target.backend == nil || result.target.origin == "" || result.target.subject == "" {
		m.failed, m.message = true, "Selected profile has no authenticated context"
		return
	}
	m.pendingContext = &result.target
	m.confirm = &tuiConfirmation{action: "context", row: tuiRow{id: result.target.input.Profile}}
}

func (m *tuiModel) contextLines() []string {
	if m.pendingContext == nil {
		return []string{"Selected context is unavailable"}
	}
	target := m.pendingContext
	return []string{"Switch context", "Profile: " + tuiText(target.input.Profile), "Server: " + tuiText(target.origin), "Subject: " + tuiText(target.subject), "Cluster: " + tuiText(target.input.Cluster), "Namespace: " + tuiText(target.input.Namespace), "Local views and cursors reset. Server tasks continue.", "y switches to this identity/scope · n/Esc keeps current context"}
}

func (m *tuiModel) contextVisible() bool {
	lines := m.contextLines()
	if m.pendingContext == nil || len(lines) > m.bodyHeight() {
		return false
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > m.width {
			return false
		}
	}
	return true
}

func (m *tuiModel) activateContext() tea.Cmd {
	if m.pendingContext == nil {
		m.message = "Selected context is unavailable"
		return nil
	}
	target := *m.pendingContext
	m.pendingContext = nil
	m.backend, m.profile, m.origin, m.subject = target.backend, target.input.Profile, target.origin, target.subject
	m.scope = tuiScope{tab: m.scope.tab, cluster: target.input.Cluster, namespace: target.input.Namespace, page: 1}
	m.sessionID, m.requestID, m.runID, m.draft = "", "", "", ""
	m.writeReference = ""
	m.stream, m.accepted = nil, false
	m.cursors = map[string]int{}
	m.resetView()
	m.failed = false
	return m.reload(nil)
}

func (m *tuiModel) contextEditorLines() []string {
	return []string{"Context JSON (configured profiles only):", tuiText(m.draft), "profile / cluster / namespace · Enter checks identity then reviews · Esc returns", fmt.Sprintf("Current: %s · %s", tuiText(m.profile), tuiText(m.subject))}
}
