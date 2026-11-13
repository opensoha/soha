package sohacli

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"net/url"
	"strconv"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func (a tuiAPI) follow(ctx context.Context, runID, sessionID string, sequence int, emit func(aiRunStreamOutput) error) error {
	if _, err := catalogID(runID); err != nil {
		return err
	}
	return a.client.receiveAIRunEvents(ctx, runID, url.PathEscape(runID), sessionID, sequence, emit)
}

func (m *tuiModel) startFollow(runID, sessionID string) tea.Cmd {
	ctx, epoch := m.begin()
	m.runID, m.sessionID = runID, sessionID
	sequence := m.cursors[runID]
	updates := make(chan tuiStreamResult, 16)
	m.stream = updates
	m.detail = true
	m.snapshot.detail = "Durable run " + runID + " · cursor " + strconv.Itoa(sequence)
	m.message = "Following original run; q detaches, x explicitly requests cancellation."
	go func() {
		defer close(updates)
		err := m.backend.follow(ctx, runID, sessionID, sequence, func(update aiRunStreamOutput) error {
			select {
			case updates <- tuiStreamResult{epoch: epoch, runUpdate: &update}:
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

func (m *tuiModel) applyRunStream(update aiRunStreamOutput) {
	if page := update.Data; page != nil {
		if page.ResyncRequired {
			m.message = "Server requires resynchronization; no cursor reset or resubmission was attempted."
			return
		}
		for _, event := range page.Items {
			m.applyStream(tuiStreamResult{update: aiStreamOutput{Event: eventPointer(event)}})
		}
		m.cursors[m.runID] = page.NextSequence
	}
	if receipt := update.Receipt; receipt != nil {
		m.cursors[receipt.RunID] = receipt.NextSequence
		m.message = "Run " + receipt.RunID + " · " + receipt.Status + " · cursor " + strconv.Itoa(receipt.NextSequence)
		// Canceled/failed outcomes return a nonzero error after the durable
		// receipt. Keep that authoritative result in the detail when the footer
		// subsequently displays the outcome error.
		detail, _ := m.snapshot.detail.(string)
		m.snapshot.detail = tuiBound(m.message+"\n"+detail, 128<<10)
	}
}

func eventPointer(event sohaapi.WorkbenchStreamEvent) *sohaapi.WorkbenchStreamEvent { return &event }
