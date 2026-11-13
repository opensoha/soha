package personalguest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/opensoha/soha-agent/internal/agent/config"
	guest "github.com/opensoha/soha-contracts/agent/guest"
)

var errTransient = errors.New("personal guest transport is temporarily unavailable")

type Client struct {
	cfg   config.PersonalGuestConfig
	http  *http.Client
	store *stateStore
	state state
}

func New(cfg config.PersonalGuestConfig) (*Client, error) {
	store, current, _, err := openState(cfg.StateFile)
	if err != nil {
		return nil, err
	}
	certificate := current.ActiveCertificatePEM
	if current.RenewalResult != nil && time.Now().Before(current.RenewalResult.CommitBefore) {
		certificate = current.RenewalResult.CertificatePEM
	}
	client, err := guestHTTPClientWithCertificate(cfg, certificate)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	if current.Binding != nil && current.Binding.ID != cfg.BindingID {
		_ = store.Close()
		client.CloseIdleConnections()
		return nil, errors.New("personal guest durable identity differs from configuration")
	}
	return &Client{cfg: cfg, http: client, store: store, state: current}, nil
}

func (c *Client) Close() error {
	c.http.CloseIdleConnections()
	return c.store.Close()
}

func (c *Client) Run(ctx context.Context) error {
	if err := c.enroll(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	failures := 0
	for {
		if err := c.heartbeat(ctx); err != nil {
			failures++
			if !errors.Is(err, errTransient) || failures >= 3 {
				return err
			}
		} else {
			failures = 0
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (c *Client) enroll(ctx context.Context) error {
	if c.state.Renewal != nil {
		if err := c.renew(ctx); err != nil {
			return err
		}
	}
	status, raw, err := c.call(ctx, http.MethodGet, "", nil, "")
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		if c.state.Binding == nil && !c.state.EnrollmentAttempted {
			return errors.New("registered personal guest has lost its durable sequence state")
		}
		return c.acceptBinding(raw)
	}
	if status != http.StatusUnauthorized || c.state.Binding != nil {
		return errors.New("personal guest binding authorization denied")
	}
	request := guest.Enrollment{SchemaVersion: guest.EnrollmentVersion, EnrollmentID: c.cfg.EnrollmentID}
	c.state.EnrollmentAttempted = true
	if err := c.store.save(c.state); err != nil {
		return err
	}
	status, raw, err = c.call(ctx, http.MethodPost, "/enroll", request, "Bearer "+c.cfg.Challenge)
	if err == nil && status == http.StatusCreated {
		return c.acceptBinding(raw)
	}
	// A committed enrollment may lose its response. Recover with the same pinned certificate.
	status, raw, err = c.call(ctx, http.MethodGet, "", nil, "")
	if err != nil || status != http.StatusOK {
		return errors.New("personal guest enrollment is unconfirmed")
	}
	return c.acceptBinding(raw)
}

func (c *Client) acceptBinding(raw []byte) error {
	var binding guest.Binding
	if json.Unmarshal(raw, &binding) != nil || binding.ID != c.cfg.BindingID || time.Now().Before(binding.IssuedAt) || !time.Now().Before(binding.ExpiresAt) {
		return errors.New("invalid authenticated personal guest binding")
	}
	if c.state.Binding != nil && *c.state.Binding != binding {
		return errors.New("personal guest binding changed across restart")
	}
	c.state.Binding = &binding
	return c.store.save(c.state)
}

func (c *Client) heartbeat(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if c.state.Renewal != nil || c.renewalDue() {
		if err := c.renew(ctx); err != nil {
			return err
		}
	}
	binding := c.state.Binding
	if binding == nil || c.state.Sequence >= guest.MaxCounter || !time.Now().Before(binding.ExpiresAt) {
		return errors.New("personal guest binding or sequence has expired")
	}
	c.state.Sequence++
	if err := c.store.save(c.state); err != nil {
		return err
	}
	request := guest.Heartbeat{SchemaVersion: guest.HeartbeatVersion, BindingID: binding.ID, BindingVersion: binding.Version,
		ComputeGeneration: binding.ComputeGeneration, Sequence: c.state.Sequence, ObservedAt: time.Now().UTC()}
	status, _, err := c.call(ctx, http.MethodPost, "/heartbeats", request, "")
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return errors.New("personal guest heartbeat authorization or sequence denied")
	}
	return nil
}

func (c *Client) call(ctx context.Context, method, suffix string, value any, authorization string) (int, []byte, error) {
	var body []byte
	if value != nil {
		var err error
		body, err = json.Marshal(value)
		if err != nil {
			return 0, nil, errors.New("invalid personal guest request")
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, c.cfg.Endpoint+guest.BindingsPath+"/"+c.cfg.BindingID+suffix, bytes.NewReader(body))
	if err != nil {
		return 0, nil, errors.New("invalid personal guest endpoint")
	}
	request.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return 0, nil, errTransient
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode >= 500 || response.StatusCode == http.StatusTooManyRequests {
		return response.StatusCode, nil, errTransient
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return response.StatusCode, nil, nil
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(raw) > 4096 || response.Header.Get("Cache-Control") != "no-store" {
		return response.StatusCode, nil, fmt.Errorf("invalid bounded personal guest response")
	}
	return response.StatusCode, raw, nil
}
