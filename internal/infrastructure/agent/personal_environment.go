package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
)

// PersonalEnvironment uses this registered connection's transport and token.
// Commands have no retry loop: a lost receipt is an unknown outcome.
func (c *Client) PersonalEnvironment(ctx context.Context, input contractenv.Request) (contractenv.Result, error) {
	var payload struct {
		Data contractenv.Result `json:"data"`
	}
	err := c.requestPersonalEnvironment(ctx, "/api/v1/runtime/personal-environments", input, &payload, func() (time.Time, error) {
		return payload.Data.ObservedAt, payload.Data.ValidateFor(input)
	})
	if err != nil {
		return contractenv.Result{}, err
	}
	return payload.Data, nil
}

func (c *Client) ExportPersonalEnvironmentData(ctx context.Context, input contractenv.Request, target io.Writer) (contractenv.ExportResult, error) {
	if strings.TrimSpace(c.token) == "" || input.Validate() != nil || input.Action != "export_data" {
		return contractenv.ExportResult{}, domaincopilot.ErrPersonalEnvironmentNotDispatched
	}
	started := time.Now().UTC()
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	stream, err := c.openStream(ctx, http.MethodPost, "/api/v1/runtime/personal-environments/export", input)
	if err != nil {
		return contractenv.ExportResult{}, err
	}
	defer func() { _ = stream.Close() }()
	result, err := contractenv.ReadExport(stream, target, input)
	if err != nil {
		return contractenv.ExportResult{}, err
	}
	if result.ObservedAt.Before(started.Add(-time.Minute)) || result.ObservedAt.After(time.Now().UTC().Add(time.Minute)) {
		return contractenv.ExportResult{}, errors.New("personal data export receipt is stale")
	}
	return result, nil
}

// Preflight never acquires personal data. Its receipt confirms the exact quota,
// fresh nonce and disposable-probe cleanup.
func (c *Client) PersonalEnvironmentPreflight(ctx context.Context, input contractenv.PreflightRequest) (contractenv.PreflightResult, error) {
	var payload struct {
		Data contractenv.PreflightResult `json:"data"`
	}
	err := c.requestPersonalEnvironment(ctx, "/api/v1/runtime/personal-environments/preflight", input, &payload, func() (time.Time, error) {
		return payload.Data.ObservedAt, payload.Data.ValidateFor(input)
	})
	if err != nil {
		return contractenv.PreflightResult{}, err
	}
	return payload.Data, nil
}

func (c *Client) requestPersonalEnvironment(ctx context.Context, path string, input interface{ Validate() error }, output any, receipt func() (time.Time, error)) error {
	if strings.TrimSpace(c.token) == "" {
		return fmt.Errorf("%w: authenticated Agent connection is required", domaincopilot.ErrPersonalEnvironmentNotDispatched)
	}
	if err := input.Validate(); err != nil {
		return fmt.Errorf("%w: %w", domaincopilot.ErrPersonalEnvironmentNotDispatched, err)
	}
	started := time.Now().UTC()
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	// Both operations reuse the registered transport without changing its shared
	// timeout. There is no network retry, including after unknown receipts.
	client := *c
	httpClient := *c.httpClient
	httpClient.Timeout = 35 * time.Second
	client.httpClient = &httpClient
	if err := client.request(ctx, http.MethodPost, path, input, output); err != nil {
		return err
	}
	observed, err := receipt()
	if err != nil {
		return err
	}
	if observed.Before(started.Add(-time.Minute)) || observed.After(time.Now().UTC().Add(time.Minute)) {
		return errors.New("environment receipt is stale or clocks are inconsistent")
	}
	return nil
}
