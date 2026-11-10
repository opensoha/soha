package dshruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	BaseURL      string
	ServiceToken string
	Timeout      time.Duration
	// AllowInsecureHTTP is only for startup-owned isolated service networks.
	AllowInsecureHTTP bool
}

type Client struct {
	base  string
	token string
	http  *http.Client
}

// HTTPError intentionally excludes response bodies and URLs, which may contain secrets.
type HTTPError struct{ StatusCode int }

func (e *HTTPError) Error() string { return fmt.Sprintf("dsh runtime HTTP status %d", e.StatusCode) }

var ErrProtocol = errors.New("dsh runtime invalid response")
var ErrInput = errors.New("dsh runtime invalid input")

func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || strings.Contains(cfg.BaseURL, "#") {
		return nil, errors.New("dsh runtime invalid base URL")
	}
	ip := net.ParseIP(u.Hostname())
	loopback := u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && (loopback || cfg.AllowInsecureHTTP)) {
		return nil, errors.New("dsh runtime requires HTTPS or loopback HTTP")
	}
	if cfg.Timeout <= 0 || cfg.Timeout > 5*time.Minute || !validToken(cfg.ServiceToken) {
		return nil, errors.New("dsh runtime invalid timeout or service token")
	}
	// A private transport avoids caller mutation and environment proxy credential egress.
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: cfg.Timeout}).DialContext, TLSHandshakeTimeout: cfg.Timeout, ResponseHeaderTimeout: cfg.Timeout, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 4, MaxIdleConnsPerHost: 4, MaxResponseHeaderBytes: 64 << 10}
	return &Client{base: strings.TrimRight(u.String(), "/"), token: cfg.ServiceToken, http: &http.Client{Timeout: cfg.Timeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func validToken(s string) bool {
	if len(s) == 0 || len(s) > 8192 {
		return false
	}
	for _, c := range s {
		if c <= 32 || c >= 127 {
			return false
		}
	}
	return true
}

func (c *Client) Close() { c.http.CloseIdleConnections() }

func (c *Client) call(ctx context.Context, method, path string, body any, out any, codes ...int) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil || len(data) > maxBody {
			return ErrInput
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(data))
	if err != nil {
		return ErrInput
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("dsh runtime request: %w", ctx.Err())
		}
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			return fmt.Errorf("dsh runtime request: %w", context.DeadlineExceeded)
		}
		return errors.New("dsh runtime transport failed")
	}
	defer resp.Body.Close()
	allowed := false
	for _, code := range codes {
		allowed = allowed || resp.StatusCode == code
	}
	if !allowed {
		return &HTTPError{StatusCode: resp.StatusCode}
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(data) > maxBody {
		return ErrProtocol
	}
	if err := decodeResponse(data, out); err != nil {
		return ErrProtocol
	}
	return nil
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	var h Health
	err := c.call(ctx, http.MethodGet, "/v1/health", nil, &h, 200)
	if err == nil && !validHealth(h) {
		err = ErrProtocol
	}
	return h, err
}

func (c *Client) Start(ctx context.Context, in StartRequest) (Receipt, error) {
	var r Receipt
	if !validStart(in) {
		return r, ErrInput
	}
	err := c.call(ctx, http.MethodPost, "/v1/runs", startWire{SchemaVersion: SchemaVersion, Kind: "start", StartRequest: in}, &r, 200, 202)
	if err == nil && (!validReceipt(r) || r.Identity != in.Identity || r.InputHash != in.InputHash) {
		err = ErrProtocol
	}
	return r, err
}

func runPath(id Identity) string {
	return "/v1/runs/" + id.RunID + "/attempts/" + strconv.FormatInt(id.Attempt, 10)
}

func (c *Client) Status(ctx context.Context, expected Submission) (Status, error) {
	var s Status
	if !validSubmission(expected) {
		return s, ErrInput
	}
	err := c.call(ctx, http.MethodGet, runPath(expected.Identity), nil, &s, 200)
	if err == nil && (!validStatus(s) || s.Submission != expected) {
		err = ErrProtocol
	}
	return s, err
}

func (c *Client) Cancel(ctx context.Context, expected Submission, reason string) (Status, error) {
	var s Status
	if !validSubmission(expected) || !bounded(reason, 0, 256) {
		return s, ErrInput
	}
	err := c.call(ctx, http.MethodPost, runPath(expected.Identity)+"/cancel", controlWire{SchemaVersion: SchemaVersion, Kind: "cancel", Identity: expected.Identity, Reason: reason}, &s, 200)
	if err == nil && (!validStatus(s) || s.Submission != expected) {
		err = ErrProtocol
	}
	return s, err
}

func (c *Client) Events(ctx context.Context, expected Identity, after int64) (EventPage, error) {
	var page EventPage
	if !validIdentity(expected) || !safe(after, 0) {
		return page, ErrInput
	}
	err := c.call(ctx, http.MethodGet, runPath(expected)+"/events?after="+strconv.FormatInt(after, 10), nil, &page, 200)
	if err == nil && !validPage(page, expected, after) {
		err = ErrProtocol
	}
	return page, err
}

// Lookup reconciles an uncertain Start outcome without resubmitting it.
func (c *Client) Lookup(ctx context.Context, id Identity, inputHash string) (Status, error) {
	var s Status
	if !validIdentity(id) || !hashPattern.MatchString(inputHash) {
		return s, ErrInput
	}
	err := c.call(ctx, http.MethodGet, runPath(id), nil, &s, 200)
	if err == nil && (!validStatus(s) || s.Identity != id || s.InputHash != inputHash) {
		err = ErrProtocol
	}
	return s, err
}
