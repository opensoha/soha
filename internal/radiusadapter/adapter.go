package radiusadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/redaction"
)

const maxControlResponseBytes = 1 << 20

type Adapter struct {
	config  Config
	client  *http.Client
	schemas *networkprotocol.Schemas
	journal *Journal
	execute Executor
	logger  *slog.Logger
	now     func() time.Time
}

func New(config Config, client *http.Client, schemas *networkprotocol.Schemas, journal *Journal, execute Executor, logger *slog.Logger) (*Adapter, error) {
	if err := config.validateIdentity(); err != nil {
		return nil, err
	}
	if client == nil || schemas == nil || journal == nil || execute == nil || logger == nil {
		return nil, fmt.Errorf("RADIUS adapter dependencies are required")
	}
	return &Adapter{config: config, client: client, schemas: schemas, journal: journal, execute: execute, logger: logger, now: time.Now}, nil
}

func (a *Adapter) Run(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			if err := a.processOnce(ctx); err != nil && ctx.Err() == nil {
				a.logger.Error("RADIUS adapter cycle failed", "event", "radius_adapter.cycle.failed", "error", redaction.LogText(err.Error(), 2048))
			}
			timer.Reset(a.config.PollInterval)
		}
	}
}

func (a *Adapter) processOnce(ctx context.Context) error {
	if replayed, err := a.replayPending(ctx); err != nil || replayed {
		return err
	}
	message, found, err := a.nextCommand(ctx)
	if err != nil || !found {
		return err
	}
	command, err := a.validateCommand(message)
	if err != nil {
		return err
	}
	now := a.now().UTC()
	deadline := now.Add(a.config.CommandTimeout)
	if message.ExpiresAt.Before(deadline) {
		deadline = message.ExpiresAt
	}
	if !deadline.After(now) {
		return fmt.Errorf("RADIUS session command expired before execution")
	}
	commandCtx, cancel := context.WithDeadline(ctx, deadline)
	result := a.execute(commandCtx, command)
	cancel()
	completedAt := a.now().UTC()
	resultPayload := networkprotocol.NASSessionCommandResult{
		CommandID: command.CommandID, SessionID: command.SessionID, Status: result.Status, ReasonCode: result.ReasonCode, CompletedAt: completedAt,
	}
	payload, err := json.Marshal(resultPayload)
	if err != nil {
		return fmt.Errorf("encode RADIUS command result: %w", err)
	}
	resultMessage := networkprotocol.RuntimeMessage{
		SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageID: uuid.NewString(), MessageType: networkprotocol.MessageNASSessionCommandResult,
		ProducerID: a.config.RuntimeID, RuntimeID: a.config.RuntimeID, RuntimeKind: "nas", OccurredAt: completedAt, ExpiresAt: message.ExpiresAt, Payload: payload,
	}
	raw, err := json.Marshal(resultMessage)
	if err != nil {
		return fmt.Errorf("encode RADIUS result message: %w", err)
	}
	if err := a.schemas.ValidateRuntime(raw); err != nil {
		return fmt.Errorf("RADIUS executor returned an invalid result: %w", err)
	}
	if err := a.journal.Put(command.CommandID, resultMessage); err != nil {
		return err
	}
	if err := a.postResult(ctx, raw); err != nil {
		return err
	}
	if err := a.journal.Delete(command.CommandID); err != nil {
		return err
	}
	a.logger.Info("RADIUS session command completed", "event", "radius_adapter.command.completed", "command_id", command.CommandID, "status", result.Status)
	return nil
}

func (a *Adapter) replayPending(ctx context.Context) (bool, error) {
	pending := a.journal.Pending()
	if len(pending) == 0 {
		return false, nil
	}
	for _, message := range pending {
		result, err := networkprotocol.DecodePayload[networkprotocol.NASSessionCommandResult](message.Payload)
		if err != nil {
			return true, fmt.Errorf("decode journaled RADIUS result: %w", err)
		}
		if !message.ExpiresAt.After(a.now().UTC()) {
			if err := a.journal.Delete(result.CommandID); err != nil {
				return true, err
			}
			continue
		}
		raw, err := json.Marshal(message)
		if err != nil || a.schemas.ValidateRuntime(raw) != nil {
			return true, fmt.Errorf("journaled RADIUS result does not match the runtime contract")
		}
		if err := a.postResult(ctx, raw); err != nil {
			return true, err
		}
		if err := a.journal.Delete(result.CommandID); err != nil {
			return true, err
		}
	}
	return true, nil
}

func (a *Adapter) nextCommand(ctx context.Context) (networkprotocol.RuntimeMessage, bool, error) {
	endpoint := strings.TrimRight(a.config.ControlURL, "/") + "/api/network-control/v1/runtimes/" + url.PathEscape(a.config.RuntimeID) + "/nas-session-commands/next"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, false, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := a.client.Do(request)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, false, fmt.Errorf("poll network control: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNoContent {
		return networkprotocol.RuntimeMessage{}, false, nil
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return networkprotocol.RuntimeMessage{}, false, fmt.Errorf("poll network control returned HTTP %d", response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return networkprotocol.RuntimeMessage{}, false, fmt.Errorf("network control returned a non-JSON command")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxControlResponseBytes+1))
	if err != nil {
		return networkprotocol.RuntimeMessage{}, false, fmt.Errorf("read network control command: %w", err)
	}
	if len(raw) > maxControlResponseBytes {
		return networkprotocol.RuntimeMessage{}, false, fmt.Errorf("network control command exceeds %d bytes", maxControlResponseBytes)
	}
	if err := a.schemas.ValidateRuntime(raw); err != nil {
		return networkprotocol.RuntimeMessage{}, false, fmt.Errorf("network control command violates runtime contract: %w", err)
	}
	message, err := networkprotocol.DecodeRuntimeMessage(raw)
	return message, true, err
}

func (a *Adapter) validateCommand(message networkprotocol.RuntimeMessage) (networkprotocol.NASSessionCommand, error) {
	if message.MessageType != networkprotocol.MessageNASSessionCommand || message.ProducerID != "network-control" || message.RuntimeID != a.config.RuntimeID || message.RuntimeKind != "nas" {
		return networkprotocol.NASSessionCommand{}, fmt.Errorf("network control command does not match runtime identity")
	}
	if err := networkprotocol.ValidateRuntimeMessageWindow(message, a.now().UTC(), a.config.MaxClockSkew); err != nil {
		return networkprotocol.NASSessionCommand{}, err
	}
	command, err := networkprotocol.DecodePayload[networkprotocol.NASSessionCommand](message.Payload)
	if err != nil {
		return networkprotocol.NASSessionCommand{}, fmt.Errorf("decode RADIUS session command: %w", err)
	}
	if command.NASID != a.config.NASID {
		return networkprotocol.NASSessionCommand{}, fmt.Errorf("network control command does not match NAS identity")
	}
	if command.EffectiveAt.After(a.now().UTC().Add(a.config.MaxClockSkew)) {
		return networkprotocol.NASSessionCommand{}, fmt.Errorf("RADIUS session command is not yet effective")
	}
	return command, nil
}

func (a *Adapter) postResult(ctx context.Context, raw []byte) error {
	endpoint := strings.TrimRight(a.config.ControlURL, "/") + "/api/network-control/v1/runtimes/" + url.PathEscape(a.config.RuntimeID) + "/nas-session-commands/result"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(raw)))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := a.client.Do(request)
	if err != nil {
		return fmt.Errorf("deliver RADIUS command result: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("deliver RADIUS command result returned HTTP %d", response.StatusCode)
	}
	return nil
}
