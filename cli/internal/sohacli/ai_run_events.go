package sohacli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func runAIRunEvents(ctx context.Context, args []string, rt Runtime) error {
	leading, rest := extractLeadingPositionals(args, 1)
	fs := newRuntimeFlagSet("ai runs events", rest, rt)
	profile := fs.String("profile", "", "profile name")
	session := fs.String("session-id", "", "restrict follow status reads to the original owned session")
	output := fs.String("output", "", "json/yaml for one page; ndjson for --follow")
	sequence := fs.Int("after-sequence", 0, "last applied durable run-event sequence")
	follow := fs.Bool("follow", false, "follow durable events and read the authoritative run status")
	wait := fs.Duration("wait-timeout", 10*time.Minute, "maximum follow duration; expiry does not cancel the run")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	ids := append(leading, fs.Args()...)
	id, err := aiWorkbenchID(ids, true)
	if err != nil {
		return err
	}
	if *sequence < 0 || *wait < time.Second || *wait > 24*time.Hour {
		return usageError{message: "after-sequence must be nonnegative; wait-timeout must be between 1s and 24h"}
	}
	if !*follow {
		if err := checkWorkbenchFlags(fs, "after-sequence", "follow"); err != nil {
			return err
		}
		format, client, err := computeOutputClient(ctx, rt, *profile, firstNonEmptyString(*output, "json"))
		if err != nil {
			return err
		}
		var page sohaapi.AgentRunEventListEnvelope
		if err := client.doJSON(ctx, http.MethodGet, aiRunEventsPath(id, *sequence), client.Token, nil, nil, &page); err != nil {
			return err
		}
		return writeStructuredOutput(rt.Out, format, sanitizeCLIJSONValue(page))
	}
	if *output != "" && *output != "ndjson" {
		return usageError{message: "following run events requires --output ndjson"}
	}
	if *session != "" {
		if _, err := aiWorkbenchID([]string{*session}, true); err != nil {
			return err
		}
	}
	client, err := loadComputeClient(ctx, rt, *profile)
	if err != nil {
		return err
	}
	if rt.StreamingContext != nil {
		var stop context.CancelFunc
		ctx, stop = rt.StreamingContext(ctx)
		defer stop()
	}
	ctx, stop := context.WithTimeout(ctx, *wait)
	defer stop()
	err = client.followAIRunEvents(ctx, ids[0], id, *session, *sequence, rt.Out)
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("run-events wait expired; the run was not canceled; resume viewing with the last emitted nextSequence")
	}
	return err
}

func aiRunEventsPath(id string, sequence int) string {
	return copilotPath + "/agent-runs/" + id + "/events?" + url.Values{"afterSequence": {strconv.Itoa(sequence)}}.Encode()
}

type aiRunStreamReceipt struct {
	RunID        string `json:"runId"`
	Status       string `json:"status"`
	NextSequence int    `json:"nextSequence"`
}
type aiRunStreamOutput struct {
	Source        string                     `json:"source"`
	RunID         string                     `json:"runId,omitempty"`
	AfterSequence *int                       `json:"afterSequence,omitempty"`
	Data          *sohaapi.AgentRunEventPage `json:"data,omitempty"`
	Receipt       *aiRunStreamReceipt        `json:"receipt,omitempty"`
}

func (c APIClient) followAIRunEvents(ctx context.Context, runID, escapedID, sessionID string, sequence int, out io.Writer) error {
	return c.receiveAIRunEvents(ctx, runID, escapedID, sessionID, sequence, func(update aiRunStreamOutput) error { return writeAINdjson(out, update, nil) })
}
func (c APIClient) receiveAIRunEvents(ctx context.Context, runID, escapedID, sessionID string, sequence int, emit func(aiRunStreamOutput) error) error {
	if emit == nil {
		return fmt.Errorf("run-events requires an event consumer")
	}
	publish := func(update aiRunStreamOutput) error {
		raw, err := json.Marshal(redactAIStreamSecrets(sanitizeCLIJSONValue(update), c.aiStreamSecrets()))
		if err != nil {
			return err
		}
		var safe aiRunStreamOutput
		if err := json.Unmarshal(raw, &safe); err != nil {
			return err
		}
		return emit(safe)
	}
	terminalStatus := ""
	for {
		var raw json.RawMessage
		if err := c.doJSON(ctx, http.MethodGet, aiRunEventsPath(escapedID, sequence), c.Token, nil, nil, &raw); err != nil {
			return err
		}
		page, err := decodeAIRunEventPage(raw)
		if err != nil {
			return err
		}
		if err := validateAIRunEventPage(runID, sequence, page); err != nil {
			return err
		}
		if err := publish(aiRunStreamOutput{Source: "run-events", RunID: runID, AfterSequence: &sequence, Data: &page}); err != nil {
			return err
		}
		if page.ResyncRequired {
			return fmt.Errorf("run-events requires resynchronization; inspect the original run and explicitly choose a valid durable cursor; no automatic reset or resubmission")
		}
		sequence = page.NextSequence
		if page.HasMore {
			continue
		}
		if terminalStatus != "" {
			if err := publish(aiRunStreamOutput{Source: "run-events", Receipt: &aiRunStreamReceipt{RunID: runID, Status: terminalStatus, NextSequence: sequence}}); err != nil {
				return err
			}
			if terminalStatus != "completed" {
				return fmt.Errorf("AI run ended with status %s", terminalStatus)
			}
			return nil
		}
		status, err := c.aiRunStatus(ctx, runID, sessionID)
		if err != nil {
			return err
		}
		if aiRunTerminal(status) {
			// The terminal transaction may commit after this page was read.
			// Drain its durable events before publishing the final cursor.
			terminalStatus = status
			continue
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			if ctx.Err() == context.Canceled {
				return ctx.Err()
			}
			return fmt.Errorf("run-events wait expired; the run was not canceled; resume viewing with the last emitted nextSequence")
		case <-timer.C:
		}
	}
}

func decodeAIRunEventPage(raw []byte) (sohaapi.AgentRunEventPage, error) {
	var shape struct {
		Data *struct {
			Items          *[]json.RawMessage `json:"items"`
			NextSequence   *int               `json:"nextSequence"`
			HasMore        *bool              `json:"hasMore"`
			ResyncRequired *bool              `json:"resyncRequired"`
		} `json:"data"`
	}
	var envelope sohaapi.AgentRunEventListEnvelope
	if json.Unmarshal(raw, &shape) != nil || shape.Data == nil || shape.Data.Items == nil || shape.Data.NextSequence == nil || shape.Data.HasMore == nil || shape.Data.ResyncRequired == nil || json.Unmarshal(raw, &envelope) != nil {
		return sohaapi.AgentRunEventPage{}, fmt.Errorf("invalid run-events page; preserve the last applied sequence")
	}
	return envelope.Data, nil
}

func validateAIRunEventPage(runID string, sequence int, page sohaapi.AgentRunEventPage) error {
	if page.ResyncRequired {
		return nil // Preserve the server's recovery instruction, never apply its cursor implicitly.
	}
	if page.NextSequence < sequence || page.HasMore && page.NextSequence == sequence {
		return fmt.Errorf("invalid run-events cursor; preserve the last applied sequence")
	}
	for _, event := range page.Items {
		raw, err := json.Marshal(event)
		var base struct {
			sohaapi.WorkbenchStreamEventBase
			Type string `json:"type"`
		}
		if err != nil || json.Unmarshal(raw, &base) != nil || base.Type == "" || base.ID == "" || base.SessionID == "" || base.CreatedAt.IsZero() || base.RunID != "" && base.RunID != runID || base.Sequence <= sequence || base.Sequence > page.NextSequence {
			return fmt.Errorf("invalid run-events identity or order; preserve the last applied sequence")
		}
		sequence = base.Sequence
	}
	return nil
}

func (c APIClient) aiRunStatus(ctx context.Context, runID, sessionID string) (string, error) {
	var runs sohaapi.WorkbenchAgentRunListEnvelope
	path := copilotPath + "/agent-runs"
	if sessionID != "" {
		path += "?" + url.Values{"sessionId": {sessionID}}.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, c.Token, nil, nil, &runs); err != nil {
		return "", err
	}
	for _, run := range runs.Items {
		if run.ID == runID {
			return string(run.Status), nil
		}
	}
	return "", fmt.Errorf("original run was not returned in run list; retry viewing with its original --session-id and preserve its ID and cursor; no replacement run was submitted")
}

func aiRunTerminal(status string) bool {
	return status == "completed" || status == "failed" || status == "canceled" || status == "callback_timeout"
}

func (c APIClient) aiStreamSecrets() []string {
	secrets := []string{c.Token, c.RefreshToken}
	if c.authState != nil {
		c.authState.mu.Lock()
		secrets = append(secrets, c.authState.AccessToken, c.authState.RefreshToken)
		c.authState.mu.Unlock()
	}
	return secrets
}
