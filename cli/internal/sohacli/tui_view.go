package sohacli

import (
	"encoding/json"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

func (m *tuiModel) View() tea.View {
	lines := []string{"Soha · " + tuiText(m.profile) + " · " + tuiText(m.origin), "Subject " + tuiText(m.subject) + " · cluster=" + tuiText(m.scope.cluster) + " namespace=" + tuiText(m.scope.namespace)}
	names := []string{"Kubernetes", "Compute", "Delivery", "AI", "Tasks", "Approvals"}
	var tabs []string
	for index, name := range names {
		if m.width < 80 {
			name = string([]rune(name)[:1])
		}
		label := fmt.Sprintf("%d %s", index+1, name)
		if index == m.scope.tab {
			label = "[" + label + "]"
		}
		tabs = append(tabs, label)
	}
	lines = append(lines, strings.Join(tabs, " · "))
	section := m.scope.section
	if section == "" {
		section = tuiSections(m.scope.tab)[0]
	}
	state := "ready"
	if m.busy {
		state = "loading"
	} else if m.failed {
		state = "stale / read failed"
	}
	updated := "no snapshot"
	if !m.snapshot.loaded.IsZero() {
		updated = m.snapshot.loaded.Format("15:04:05 MST") + " (r refresh)"
	}
	lines = append(lines, fmt.Sprintf("%s · %s · page %d · %s", section, state, m.scope.page, updated), "")
	lines = append(lines, m.body()...)
	lines = append(lines, "", tuiText(m.message))
	if m.requestID != "" {
		lines = append(lines, tuiText(m.reference()))
	}
	if m.writeReference != "" {
		lines = append(lines, tuiText(m.writeReference))
	}
	lines = append(lines, "1–6 workbench · Tab view · ↑↓/jk scroll · Enter detail · b back · r refresh · c context · ? help · q detach")
	for i, line := range lines {
		lines[i] = tuiClip(line, max(1, m.width))
	}
	if len(lines) > m.height {
		lines = lines[:max(1, m.height)]
	}
	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true
	return view
}

func (m *tuiModel) body() []string {
	if m.confirm != nil {
		return m.confirmationBody()
	}
	if m.composing {
		if m.contextEditing {
			return m.contextEditorLines()
		}
		if m.writeEditing {
			return []string{"Typed write action for this workbench:", tuiText(m.draft), "Enter requests server preflight · Ctrl+U clears · Esc discards", "K8s: restart/scale/rollback · VM/host: create · project: deploy", "Delivery: confirm-plan with original planId. No arbitrary HTTP or shell."}
		}
		return []string{"Chat session: " + tuiText(m.sessionID), tuiText(m.draft), "Enter reviews · Esc discards · Ctrl+C detaches"}
	}
	if m.help {
		return []string{"Read: Enter detail, b back, r reread; n/p numbered pages when supported.", "Tab: cycle views. c reviews profile/cluster/namespace.", "AI: select a chat, i compose; Enter reviews, y sends one original turn.", "Tasks: detail then x requests cancellation. q only detaches.", "Approvals: detail then a approve or d reject, then review and y.", "w: typed write action, real server preflight, full review then y.", "Long or sensitive edits use typed native commands with server plans.", "No key bypasses server authorization, MFA, policy or approval."}
	}
	if m.detail {
		text, ok := m.snapshot.detail.(string)
		if !ok {
			raw, err := json.MarshalIndent(m.snapshot.detail, "", "  ")
			if err != nil {
				text = "Cannot render result"
			} else {
				text = string(raw)
			}
		}
		if m.requestID != "" {
			text = "Session: " + m.sessionID + "\nOriginal request: " + m.requestID + "\nRun: " + m.runID + "\n\n" + text
		}
		lines := strings.Split(tuiBound(tuiText(text), 128<<10), "\n")
		start := min(m.scroll, max(0, len(lines)-1))
		end := min(len(lines), start+m.bodyHeight())
		return append(lines[start:end], fmt.Sprintf("Lines %d–%d/%d · x cancel task · a/d approval · i chat", start+1, end, len(lines)))
	}
	if len(m.snapshot.rows) == 0 {
		if m.busy {
			return []string{"Loading authorized resources…"}
		}
		return []string{"No resources in the returned authorized snapshot."}
	}
	start := max(0, m.selected-m.bodyHeight()+2)
	end := min(len(m.snapshot.rows), start+m.bodyHeight())
	lines := []string{"NAME · STATE · ID"}
	for i := start; i < end; i++ {
		row := m.snapshot.rows[i]
		prefix := "  "
		if i == m.selected {
			prefix = "> "
		}
		lines = append(lines, prefix+tuiText(row.name)+" · "+tuiText(row.status)+" · "+tuiText(row.id))
	}
	return lines
}

// Never forward terminal controls from server names, logs, messages or errors.
// Newlines are retained for panel layout. Bidi/zero-width format controls are escaped.
func tuiText(value string) string {
	var out strings.Builder
	for _, r := range value {
		switch {
		case r == '\n':
			out.WriteRune(r)
		case r == '\t':
			out.WriteString("    ")
		case unicode.IsControl(r) || unicode.In(r, unicode.Cf):
			fmt.Fprintf(&out, "\\u%04x", r)
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}
func tuiBound(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) && len(value) > 0 {
		value = value[:len(value)-1]
	}
	return value + "\n[display truncated]"
}
func tuiClip(value string, width int) string { return ansi.Truncate(value, width, "…") }

func (m *tuiModel) confirmationBody() []string {
	if m.confirm.action == "context" {
		if !m.contextVisible() {
			return []string{"Enlarge terminal to review the full selected identity and scope.", "Esc/n keeps the current context."}
		}
		return m.contextLines()
	}
	c := m.confirm
	if m.height < 16 || m.width < 40 || c.action != "write" && ansi.StringWidth(tuiText(c.row.id))+8 > m.width {
		return []string{"Enlarge terminal to review this action.", "Esc/n returns without submitting."}
	}
	lines := []string{"Review " + c.action, "Target: " + tuiText(c.row.id), "Status: " + tuiText(c.row.status), "Server rechecks permission, scope and approval."}
	if c.action == "write" {
		lines[1] = "Target and scope: see full review below."
	}
	if c.action == "cancel" {
		lines = append(lines, "Cancellation is a request; wait for terminal status.")
	}
	if c.action == "send" || c.action == "write" {
		content := m.confirmContentLines()
		start := min(c.scroll, len(content)-1)
		end := min(len(content), start+m.confirmContentHeight())
		lines = append(lines, "Original request: "+c.requestID, "Content:")
		lines = append(lines, content[start:end]...)
		lines = append(lines, fmt.Sprintf("Content lines %d–%d/%d · ↑↓ to review", start+1, end, len(content)))
	}
	if !m.confirmVisible() {
		return append(lines, "Review through the last content line to enable y; n/Esc returns.")
	}
	return append(lines, "y submits this target/input · n/Esc returns")
}
