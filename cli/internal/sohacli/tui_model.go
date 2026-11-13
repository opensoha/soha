package sohacli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
)

type tuiResult struct {
	epoch    uint64
	snapshot tuiSnapshot
	err      error
}
type tuiPoll struct {
	epoch  uint64
	target tuiConfirmation
}
type tuiStreamResult struct {
	epoch     uint64
	update    aiStreamOutput
	runUpdate *aiRunStreamOutput
	err       error
	done      bool
}
type tuiConfirmation struct {
	scope                      tuiScope
	row                        tuiRow
	action, requestID, content string
	scroll                     int
	plan                       *tuiWritePlan
}

type tuiModel struct {
	ctx                                         context.Context
	backend                                     tuiBackend
	profile, origin, subject                    string
	scope                                       tuiScope
	epoch                                       uint64
	cancel                                      context.CancelFunc
	snapshot                                    tuiSnapshot
	selected, scroll, width, height             int
	detail, help, busy, composing               bool
	draft, message, sessionID, requestID, runID string
	confirm                                     *tuiConfirmation
	stream                                      <-chan tuiStreamResult
	cursors                                     map[string]int
	accepted, failed                            bool
	watch                                       *tuiConfirmation
	previousCursors                             []string
	contextEditing                              bool
	loadContext                                 tuiContextLoader
	pendingContext                              *tuiContextTarget
	writeEditing                                bool
	writeReference                              string
	references                                  []tuiRequestReference
}

func newTUIModel(ctx context.Context, backend tuiBackend, profile, origin, subject, cluster, namespace string) *tuiModel {
	return &tuiModel{ctx: ctx, backend: backend, profile: profile, origin: origin, subject: subject,
		scope: tuiScope{cluster: cluster, namespace: namespace, page: 1}, width: 80, height: 24, cursors: map[string]int{}}
}

func (m *tuiModel) Init() tea.Cmd { return m.reload(nil) }

func (m *tuiModel) detach() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

func (m *tuiModel) begin() (context.Context, uint64) {
	m.detach()
	m.epoch++
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Minute)
	m.cancel = cancel
	m.busy = true
	return ctx, m.epoch
}

func (m *tuiModel) reload(row *tuiRow) tea.Cmd {
	ctx, epoch := m.begin()
	scope := m.scope
	m.confirm = nil
	return func() tea.Msg {
		snapshot, err := m.backend.load(ctx, scope, row)
		return tuiResult{epoch: epoch, snapshot: snapshot, err: err}
	}
}

func (m *tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
	case tuiResult:
		return m, m.applyLoadResult(v)
	case tuiPoll:
		if v.epoch == m.epoch && m.watch != nil {
			return m, m.reload(&v.target.row)
		}
	case tuiContextResult:
		m.applyContextResult(v)
	case tuiWriteResult:
		m.applyWriteResult(v)
	case tuiStreamResult:
		if v.epoch != m.epoch {
			return m, nil
		}
		m.applyStream(v)
		if !v.done {
			return m, m.waitStream()
		}
		if v.err == nil && m.accepted && m.runID != "" {
			m.accepted = false
			return m, m.startFollow(m.runID, m.sessionID)
		}
	case tea.PasteMsg:
		if m.composing {
			m.appendDraft(v.String())
		}
	case tea.KeyPressMsg:
		return m.key(v)
	}
	return m, nil
}

func (m *tuiModel) applyLoadResult(result tuiResult) tea.Cmd {
	if result.epoch != m.epoch {
		return nil
	}
	m.busy = false
	if result.err != nil {
		m.message, m.failed = tuiText(result.err.Error()), true
		return nil
	}
	m.snapshot, m.message, m.scroll = result.snapshot, result.snapshot.message, 0
	m.failed = false
	if m.selected >= len(m.snapshot.rows) {
		m.selected = 0
	}
	if m.watch != nil {
		return m.pollAfterCancel()
	}
	return nil
}

func (m *tuiModel) key(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := key.String()
	if k == "ctrl+c" || !m.composing && k == "q" {
		m.detach()
		return m, tea.Quit
	}
	if m.composing {
		return m.compose(key)
	}
	if m.confirm != nil {
		return m.confirmKey(k)
	}
	if len(k) == 1 && k[0] >= '1' && k[0] <= '6' {
		m.scope.tab, m.scope.page, m.scope.section = int(k[0]-'1'), 1, ""
		m.resetView()
		return m, m.reload(nil)
	}
	return m, m.browseKey(k)
}

func (m *tuiModel) browseKey(k string) tea.Cmd {
	switch k {
	case "?":
		m.help = !m.help
	case "c":
		m.beginContextEdit()
	case "w":
		m.beginWriteEdit()
	case "esc", "b":
		m.scope.logs = false
		m.watch = nil
		m.detail, m.help, m.scroll = false, false, 0
		return m.reload(nil)
	case "r":
		if m.detail && m.row() != nil {
			return m.reload(m.row())
		}
		return m.reload(nil)
	case "tab":
		m.cycleSection()
		return m.reload(nil)
	case "down", "j":
		m.move(1)
	case "up", "k":
		m.move(-1)
	case "pgdown":
		m.move(m.bodyHeight())
	case "pgup":
		m.move(-m.bodyHeight())
	case "n", "p":
		return m.pageKey(k)
	case "enter":
		if row := m.row(); row != nil && !m.busy {
			m.detail = true
			return m.reload(row)
		}
	default:
		return m.detailKey(k)
	}
	return nil
}
func (m *tuiModel) cycleSection() {
	sections := tuiSections(m.scope.tab)
	next := 1
	for i, section := range sections {
		if section == m.scope.section {
			next = i + 1
		}
	}
	m.scope.section, m.scope.page = sections[next%len(sections)], 1
	m.resetView()
}
func (m *tuiModel) pageKey(key string) tea.Cmd {
	if m.detail || m.busy {
		return nil
	}
	if key == "n" && m.snapshot.nextCursor != "" {
		m.previousCursors = append(m.previousCursors, m.scope.cursor)
		m.scope.cursor = m.snapshot.nextCursor
		m.scope.page++
	} else if key == "p" && len(m.previousCursors) > 0 {
		last := len(m.previousCursors) - 1
		m.scope.cursor = m.previousCursors[last]
		m.previousCursors = m.previousCursors[:last]
		m.scope.page--
	} else if m.scope.tab != 1 {
		return nil
	} else if key == "n" && m.snapshot.total > int64(m.scope.page*50) {
		m.scope.page++
	} else if key == "p" && m.scope.page > 1 {
		m.scope.page--
	} else {
		return nil
	}
	m.selected = 0
	return m.reload(nil)
}
func (m *tuiModel) detailKey(key string) tea.Cmd {
	switch key {
	case "i":
		if m.scope.tab == 3 && (m.scope.section == "" || m.scope.section == "sessions") && m.row() != nil && !m.busy {
			m.sessionID, m.draft, m.composing = m.row().id, "", true
		}
	case "f":
		return m.followKey()
	case "l":
		if row := m.row(); row != nil && !m.busy {
			m.detail = true
			m.scope.logs = true
			return m.reload(row)
		}
	case "x", "a", "d":
		m.reviewAction(key)
	}
	return nil
}
func (m *tuiModel) followKey() tea.Cmd {
	if m.busy {
		return nil
	}
	if m.scope.section == "runs" && m.row() != nil {
		return m.startFollow(m.row().id, tuiString(m.row().value, "sessionId"))
	}
	if m.runID != "" {
		return m.startFollow(m.runID, m.sessionID)
	}
	return nil
}
func (m *tuiModel) reviewAction(key string) {
	if key == "x" && m.runID != "" && m.busy {
		m.confirm = &tuiConfirmation{scope: tuiScope{tab: 3, section: "runs"}, row: tuiRow{id: m.runID, status: "read authoritative run status after cancellation", value: map[string]any{"sessionId": m.sessionID}}, action: "cancel"}
		return
	}
	if row := m.row(); row != nil && m.detail && !m.busy {
		action := map[string]string{"x": "cancel", "a": "approve", "d": "reject"}[key]
		if tuiActionAllowed(m.scope, *row, action) {
			scope := m.scope
			scope.logs = false
			m.confirm = &tuiConfirmation{scope: scope, row: *row, action: action}
		}
	}
}

func (m *tuiModel) resetView() {
	m.detail, m.help = false, false
	m.selected, m.scroll = 0, 0
	m.snapshot = tuiSnapshot{}
	m.message = ""
	m.scope.logs = false
	m.scope.cursor = ""
	m.previousCursors = nil
	m.watch = nil
}
func (m *tuiModel) row() *tuiRow {
	if m.selected < 0 || m.selected >= len(m.snapshot.rows) {
		return nil
	}
	row := m.snapshot.rows[m.selected]
	return &row
}
func (m *tuiModel) move(delta int) {
	if m.detail {
		m.scroll = max(0, m.scroll+delta)
		return
	}
	m.selected = max(0, min(len(m.snapshot.rows)-1, m.selected+delta))
}
func (m *tuiModel) bodyHeight() int { return max(1, m.height-10) }

func (m *tuiModel) compose(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.composing = false
		m.contextEditing = false
		m.writeEditing = false
	case "backspace":
		if len(m.draft) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.draft)
			m.draft = m.draft[:len(m.draft)-size]
		}
	case "ctrl+u":
		m.draft = ""
	case "enter":
		if m.contextEditing {
			return m, m.stageContext()
		}
		if m.writeEditing {
			return m, m.stageWrite()
		}
		if strings.TrimSpace(m.draft) != "" {
			m.confirm = &tuiConfirmation{scope: m.scope, row: tuiRow{id: m.sessionID}, action: "send", content: m.draft, requestID: uuid.NewString()}
			m.composing = false
		}
	default:
		if text := key.Key().Text; text != "" && len(m.draft)+len(text) <= 16<<10 {
			m.appendDraft(text)
		}
	}
	return m, nil
}

func (m *tuiModel) confirmKey(key string) (tea.Model, tea.Cmd) {
	if key == "down" || key == "j" || key == "pgdown" {
		m.confirm.scroll = min(len(m.confirmContentLines())-1, m.confirm.scroll+1)
		return m, nil
	}
	if key == "up" || key == "k" || key == "pgup" {
		m.confirm.scroll = max(0, m.confirm.scroll-1)
		return m, nil
	}

	if key == "esc" || key == "n" {
		m.confirm = nil
		m.pendingContext = nil
		return m, nil
	}
	// The complete confirmation must be visible before accepting it.
	if key != "y" || !m.confirmVisible() {
		return m, nil
	}
	frozen := *m.confirm
	m.confirm = nil
	if frozen.action == "context" {
		return m, m.activateContext()
	}
	if frozen.action == "send" {
		return m, m.startChat(frozen)
	}
	if frozen.action == "write" {
		return m, m.submitWrite(frozen.plan)
	}
	if frozen.action == "cancel" {
		m.watch = &frozen
		m.scope = frozen.scope
	}
	ctx, epoch := m.begin()
	return m, func() tea.Msg {
		value, err := m.backend.act(ctx, frozen.scope, frozen.row, frozen.action)
		return tuiResult{epoch: epoch, snapshot: tuiSnapshot{detail: value, rows: []tuiRow{frozen.row}, loaded: time.Now(), message: "Request returned; refresh to read authoritative status."}, err: err}
	}
}

func (m *tuiModel) startChat(turn tuiConfirmation) tea.Cmd {
	if !m.rememberReference(tuiRequestReference{Kind: "ai-turn", SessionID: turn.row.id, RequestID: turn.requestID}) {
		return nil
	}
	ctx, epoch := m.begin()
	m.scope.logs = false
	m.sessionID, m.requestID, m.runID = turn.row.id, turn.requestID, ""
	m.message = "Submitting original turn; a disconnected response is unknown. No automatic resend."
	m.snapshot.detail, m.detail = "", true
	updates := make(chan tuiStreamResult, 16)
	m.stream = updates
	go func() {
		defer close(updates)
		err := m.backend.chat(ctx, turn.row.id, turn.requestID, turn.content, func(update aiStreamOutput) error {
			select {
			case updates <- tuiStreamResult{epoch: epoch, update: update}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		select {
		case updates <- tuiStreamResult{epoch: epoch, err: err, done: true}:
		case <-ctx.Done():
		}
	}()
	return m.waitStream()
}

func (m *tuiModel) waitStream() tea.Cmd {
	stream, ctx := m.stream, m.ctx
	return func() tea.Msg {
		select {
		case update, ok := <-stream:
			if ok {
				return update
			}
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}

func (m *tuiModel) applyStream(result tuiStreamResult) {
	if result.done {
		m.busy = false
		if result.err != nil && !errors.Is(result.err, context.Canceled) {
			m.message = tuiText(result.err.Error())
		}
		return
	}
	if result.runUpdate != nil {
		m.applyRunStream(*result.runUpdate)
		return
	}
	if receipt := result.update.Receipt; receipt != nil {
		m.runID = receipt.RunID
		m.rememberRunID(receipt.RunID)
		m.accepted = receipt.Status == "accepted"
		m.message = "Turn " + receipt.RequestID + " · " + receipt.Status
	}
	if event := result.update.Event; event != nil {
		var projection struct{ Type, ContentDelta, Content, RunID string }
		decodeTUIEvent(*event, &projection)
		if projection.RunID != "" {
			m.runID = projection.RunID
			m.rememberRunID(projection.RunID)
		}
		text, _ := m.snapshot.detail.(string)
		if projection.ContentDelta != "" {
			text += projection.ContentDelta
		} else if projection.Content != "" {
			text += "\n" + projection.Content
		} else {
			text += "\n[" + projection.Type + "]"
		}
		m.snapshot.detail = tuiBound(text, 128<<10)
		m.snapshot.loaded = time.Now()
	}
}

func tuiSections(tab int) []string {
	switch tab {
	case 0:
		return []string{"pods", "deployments", "statefulsets", "daemonsets", "jobs", "events"}
	case 1:
		return []string{"vms", "hosts", "projects", "services"}
	case 2:
		return []string{"batches", "tasks", "workflows"}
	case 3:
		return []string{"sessions", "runs", "assistant-tasks", "artifacts"}
	case 4:
		return []string{"compute", "delivery", "runs"}
	default:
		return []string{"pending"}
	}
}

func tuiActionAllowed(scope tuiScope, row tuiRow, action string) bool {
	if action == "approve" || action == "reject" {
		return scope.tab == 5 && row.status == "pending"
	}
	if action != "cancel" {
		return false
	}
	if tuiTerminalStatus(row.status) {
		return false
	}
	if cancelable, exists := row.value["cancelable"].(bool); exists && !cancelable {
		return false
	}
	return scope.tab == 4 || scope.tab == 3 && scope.section == "runs" || scope.tab == 2 && scope.section == "tasks"
}

func (m *tuiModel) reference() string {
	return fmt.Sprintf("session=%s request=%s run=%s", m.sessionID, m.requestID, m.runID)
}

func (m *tuiModel) confirmVisible() bool {
	if m.confirm == nil || m.height < 16 || m.width < 40 {
		return false
	}
	c := m.confirm
	if c.action != "write" && ansi.StringWidth(tuiText(c.row.id))+8 > m.width {
		return false
	}
	if c.action == "context" {
		return m.contextVisible()
	}
	return c.action != "send" && c.action != "write" || c.scroll+m.confirmContentHeight() >= len(m.confirmContentLines())
}

func (m *tuiModel) appendDraft(text string) {
	if utf8.ValidString(text) && len(m.draft)+len(text) <= 16<<10 {
		m.draft += text
	}
}
func (m *tuiModel) confirmContentHeight() int { return max(1, m.height-19) }
func (m *tuiModel) confirmContentLines() []string {
	return strings.Split(ansi.Hardwrap(tuiText(m.confirm.content), max(1, m.width-2), true), "\n")
}
