// Package hindsight implements the qualified Hindsight 1.2.0 raw memory API.
package hindsight

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	app "github.com/opensoha/soha/internal/application/memory"
)

type Config struct {
	BaseURL string
	Token   string
	Timeout time.Duration
}
type Client struct {
	base  string
	token string
	http  *http.Client
}

func NewClient(cfg Config, client *http.Client) (*Client, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("invalid Hindsight endpoint or missing service token")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.Timeout <= 0 || cfg.Timeout > 30*time.Second {
		return nil, fmt.Errorf("Hindsight timeout must be within 30 seconds")
	}
	c := http.Client{}
	if client != nil {
		c = *client
	}
	c.Timeout = cfg.Timeout
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: strings.TrimRight(cfg.BaseURL, "/"), token: cfg.Token, http: &c}, nil
}
func bankPath(bank, tail string) string { return "/v1/default/banks/" + url.PathEscape(bank) + tail }
func (c *Client) request(ctx context.Context, method, path string, payload, out any, retainOperationID string) error {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return fmt.Errorf("build Hindsight request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if retainOperationID != "" {
		req.Header.Set("X-Soha-Retain-Operation-ID", retainOperationID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Hindsight transport failed")
	}
	defer resp.Body.Close()
	if retainOperationID != "" && (resp.StatusCode == 404 || resp.StatusCode >= 200 && resp.StatusCode < 300) {
		proof := resp.Header.Values("X-Soha-Retain-Operation-Purged")
		if len(proof) != 1 || proof[0] != retainOperationID {
			return fmt.Errorf("missing Hindsight retain operation purge receipt")
		}
	}
	if resp.StatusCode == 404 {
		return app.ErrBackendNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Hindsight HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil || len(data) > 2*1024*1024 {
		return fmt.Errorf("invalid Hindsight response size")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("malformed Hindsight response")
	}
	return nil
}
func (c *Client) Retain(ctx context.Context, bank string, j app.SyncJob) error {
	// Hindsight 1.2.0 creates the bank atomically with the parent and children in submit_async_retain.
	body := map[string]any{"async": true, "operation_id": j.ID, "items": []any{map[string]any{"content": j.Record.Fact, "document_id": j.DocumentID, "metadata": map[string]string{"source_revision": strconv.FormatInt(j.Revision, 10)}}}}
	var res struct {
		Success     bool   `json:"success"`
		Async       bool   `json:"async"`
		OperationID string `json:"operation_id"`
		BankID      string `json:"bank_id"`
	}
	if err := c.request(ctx, http.MethodPost, bankPath(bank, "/memories"), body, &res, ""); err != nil {
		return err
	}
	if !res.Success || !res.Async || res.OperationID != j.ID || res.BankID != bank {
		return fmt.Errorf("invalid Hindsight retain receipt")
	}
	return nil
}

type retainOperation struct {
	OperationID  string                   `json:"operation_id"`
	BankID       *string                  `json:"bank_id"`
	Status       string                   `json:"status"`
	Type         string                   `json:"operation_type"`
	ErrorMessage string                   `json:"error_message"`
	Metadata     *retainOperationMetadata `json:"result_metadata"`
	Children     []retainChildOperation   `json:"child_operations"`
}
type retainOperationMetadata struct {
	IsParent         bool   `json:"is_parent"`
	ItemsCount       int    `json:"items_count"`
	ExtractionErrors *int64 `json:"extraction_errors_count"`
}
type retainChildOperation struct {
	ID           string `json:"operation_id"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message"`
}

func (c *Client) Operation(ctx context.Context, bank, id string) (string, error) {
	var res retainOperation
	// Hindsight 1.2.0 binds both IDs in this scoped endpoint's SQL. Its normal
	// response omits bank_id; reject an explicit conflicting binding if supplied.
	if err := c.request(ctx, http.MethodGet, bankPath(bank, "/operations/"+url.PathEscape(id)), nil, &res, ""); err != nil {
		return "", err
	}
	if res.OperationID != id || (res.BankID != nil && *res.BankID != bank) {
		return "", fmt.Errorf("mismatched Hindsight operation")
	}
	if res.Status == "not_found" {
		return "", app.ErrBackendNotFound
	}
	if res.Type != "retain" && res.Type != "batch_retain" {
		return "", fmt.Errorf("mismatched Hindsight operation type")
	}
	if res.Type == "batch_retain" && (res.Metadata == nil || !res.Metadata.IsParent || res.Metadata.ItemsCount != 1) {
		return "", fmt.Errorf("invalid single-document Hindsight retain parent")
	}
	switch res.Status {
	case "pending", "processing":
		return res.Status, nil
	case "failed", "cancelled":
		return terminalRetainFailure(res)
	case "completed":
		return completedRetainStatus(res)
	default:
		return "", fmt.Errorf("invalid Hindsight operation state")
	}
}
func terminalRetainFailure(res retainOperation) (string, error) {
	// Cancellation only flips an upstream row; even failed children may leave
	// unawaited sub-tasks after their bounded cancellation grace period (1.2.0).
	if res.Type != "batch_retain" || len(res.Children) == 0 {
		return "", fmt.Errorf("%w: retain worker stop not confirmed", app.ErrSyncUnknown)
	}
	if _, err := completedRetainChildren(res.Children, res.OperationID); err != nil {
		return "", err
	}
	return "failed", nil
}
func completedRetainStatus(res retainOperation) (string, error) {
	if res.Type == "batch_retain" && (res.Metadata.ExtractionErrors == nil || len(res.Children) == 0) {
		return "", fmt.Errorf("%w: missing completed retain outcome", app.ErrSyncUnknown)
	}
	// Verify completion before turning extraction errors into failure: failure
	// releases dependent deletes, so a still-running child must remain unknown.
	state, err := completedRetainChildren(res.Children, res.OperationID)
	if err != nil {
		return "", err
	}
	if res.Metadata != nil && res.Metadata.ExtractionErrors != nil {
		if *res.Metadata.ExtractionErrors < 0 {
			return "", fmt.Errorf("invalid Hindsight extraction error count")
		}
		if *res.Metadata.ExtractionErrors > 0 {
			return "failed", nil
		}
	}
	if res.ErrorMessage != "" {
		return "failed", nil
	}
	return state, nil
}
func completedRetainChildren(children []retainChildOperation, parentID string) (string, error) {
	seen := map[string]bool{}
	state := "completed"
	for _, child := range children {
		if child.ID == "" || child.ID == parentID || seen[child.ID] {
			return "", fmt.Errorf("%w: invalid retain child identity", app.ErrSyncUnknown)
		}
		seen[child.ID] = true
		switch child.Status {
		case "completed":
			if child.ErrorMessage != "" {
				state = "failed"
			}
		default:
			return "", fmt.Errorf("%w: retain child completion not confirmed", app.ErrSyncUnknown)
		}
	}
	return state, nil
}
func (c *Client) DeleteDocument(ctx context.Context, bank, document, retainOperationID string) error {
	var res struct {
		Success    bool   `json:"success"`
		DocumentID string `json:"document_id"`
	}
	err := c.request(ctx, http.MethodDelete, bankPath(bank, "/documents/"+url.PathEscape(document)), nil, &res, retainOperationID)
	if errors.Is(err, app.ErrBackendNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !res.Success || res.DocumentID != document {
		return fmt.Errorf("invalid Hindsight deletion receipt")
	}
	return nil
}
func (c *Client) Recall(ctx context.Context, bank, query string) ([]app.Candidate, error) {
	var res struct {
		Results *[]struct {
			Document string            `json:"document_id"`
			Type     string            `json:"type"`
			Metadata map[string]string `json:"metadata"`
		} `json:"results"`
	}
	body := map[string]any{"query": query, "types": []string{"world", "experience"}, "max_tokens": 4096}
	err := c.request(ctx, http.MethodPost, bankPath(bank, "/memories/recall"), body, &res, "")
	if errors.Is(err, app.ErrBackendNotFound) {
		return []app.Candidate{}, nil
	}
	if err != nil {
		return nil, err
	}
	if res.Results == nil {
		return nil, fmt.Errorf("missing Hindsight recall results")
	}
	out := make([]app.Candidate, 0, len(*res.Results))
	for _, r := range *res.Results {
		revision, err := strconv.ParseInt(r.Metadata["source_revision"], 10, 64)
		if err != nil || revision <= 0 || r.Document == "" || (r.Type != "world" && r.Type != "experience") {
			continue
		}
		out = append(out, app.Candidate{DocumentID: r.Document, Revision: revision})
	}
	return out, nil
}
