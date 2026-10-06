package networkproxyruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

type clashController struct {
	origin string
	secret string
	http   *http.Client
}

func newClashController(origin, secret string) *clashController {
	transport := cloneDefaultTransport()
	transport.Proxy = nil
	return &clashController{origin: strings.TrimRight(origin, "/"), secret: secret,
		http: &http.Client{Transport: transport, Timeout: 5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *clashController) request(ctx context.Context, method, path string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.origin+path, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.secret)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("local controller returned HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if len(raw) >= 2<<20 {
		return nil, fmt.Errorf("local controller response is too large")
	}
	return raw, nil
}

func (c *clashController) version(ctx context.Context) (string, error) {
	raw, err := c.request(ctx, http.MethodGet, "/version", nil)
	if err != nil {
		return "", err
	}
	var result struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || result.Version == "" || len(result.Version) > 64 {
		return "", fmt.Errorf("local controller version is invalid")
	}
	return result.Version, nil
}

type clashConnection struct {
	ID       string `json:"id"`
	Upload   int64  `json:"upload"`
	Download int64  `json:"download"`
	Start    string `json:"start"`
	Metadata struct {
		Host            string `json:"host"`
		DestinationIP   string `json:"destinationIP"`
		DestinationPort string `json:"destinationPort"`
		Network         string `json:"network"`
	} `json:"metadata"`
}

func (c *clashController) snapshot(ctx context.Context) (int64, int64, int, []domain.Connection, error) {
	raw, err := c.request(ctx, http.MethodGet, "/connections", nil)
	if err != nil {
		return 0, 0, 0, nil, err
	}
	var result struct {
		UploadTotal   int64             `json:"uploadTotal"`
		DownloadTotal int64             `json:"downloadTotal"`
		Connections   []clashConnection `json:"connections"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return 0, 0, 0, nil, fmt.Errorf("decode local connections: %w", err)
	}
	if result.UploadTotal < 0 || result.DownloadTotal < 0 || len(result.Connections) > 1000000 {
		return 0, 0, 0, nil, fmt.Errorf("local connection counters are invalid")
	}
	connections := make([]domain.Connection, 0, min(len(result.Connections), 200))
	for _, item := range result.Connections {
		if len(connections) == 200 {
			break
		}
		if item.ID == "" || item.Upload < 0 || item.Download < 0 {
			continue
		}
		destination := item.Metadata.Host
		if destination == "" {
			destination = item.Metadata.DestinationIP
		}
		if destination == "" || len(destination) > 253 {
			continue
		}
		network := strings.ToLower(item.Metadata.Network)
		if network != "tcp" && network != "udp" {
			network = "other"
		}
		connection := domain.Connection{ID: item.ID, Destination: destination, Network: network,
			UploadBytes: item.Upload, DownloadBytes: item.Download}
		if at, err := time.Parse(time.RFC3339Nano, item.Start); err == nil {
			connection.StartedAt = &at
		}
		connections = append(connections, connection)
	}
	return result.UploadTotal, result.DownloadTotal, len(result.Connections), connections, nil
}

func (c *clashController) close(ctx context.Context, id string) (string, error) {
	_, err := c.request(ctx, http.MethodDelete, "/connections/"+url.PathEscape(id), nil)
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			return "not-found", nil
		}
		return "failed", err
	}
	return "closed", nil
}
