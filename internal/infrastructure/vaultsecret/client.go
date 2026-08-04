package vaultsecret

import (
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

	domainsecret "github.com/opensoha/soha/internal/domain/secret"
)

type Client struct {
	baseURL          *url.URL
	token            string
	namespace        string
	maxResponseBytes int64
	httpClient       *http.Client
}

func New(address, token, namespace string, timeout time.Duration, maxResponseBytes int64) (*Client, error) {
	if strings.TrimSpace(address) != address || strings.TrimSpace(token) != token || strings.TrimSpace(namespace) != namespace {
		return nil, errors.New("invalid Vault client configuration")
	}
	baseURL, err := url.Parse(address)
	if err != nil || baseURL.Host == "" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" ||
		(baseURL.Path != "" && baseURL.Path != "/") || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return nil, errors.New("invalid Vault server address")
	}
	if strings.TrimSpace(token) == "" || timeout <= 0 || maxResponseBytes < 1 {
		return nil, errors.New("invalid Vault client configuration")
	}
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default HTTP transport is unavailable")
	}
	transport := defaultTransport.Clone()
	transport.TLSHandshakeTimeout = 5 * time.Second
	return &Client{
		baseURL: baseURL, token: token, namespace: namespace, maxResponseBytes: maxResponseBytes,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("vault redirects are disabled")
			},
		},
	}, nil
}

func (c *Client) Read(ctx context.Context, reference domainsecret.VaultKV2Reference) (string, error) {
	endpoint := *c.baseURL
	endpoint.Path = "/v1/" + reference.Mount + "/data/" + reference.Path
	query := endpoint.Query()
	query.Set("version", strconv.Itoa(reference.Version))
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", errors.New("build Vault secret request")
	}
	request.Header.Set("X-Vault-Token", c.token)
	if c.namespace != "" {
		request.Header.Set("X-Vault-Namespace", c.namespace)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", errors.New("read Vault secret")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("read Vault secret: status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, c.maxResponseBytes+1))
	if err != nil || int64(len(body)) > c.maxResponseBytes {
		return "", errors.New("read Vault secret response")
	}
	var payload struct {
		Data struct {
			Data     map[string]json.RawMessage `json:"data"`
			Metadata struct {
				Version int `json:"version"`
			} `json:"metadata"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Data.Metadata.Version != reference.Version {
		return "", errors.New("invalid Vault secret response")
	}
	raw, ok := payload.Data.Data[reference.Key]
	if !ok {
		return "", errors.New("vault secret key is unavailable")
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", errors.New("vault secret value must be a string")
	}
	return value, nil
}
