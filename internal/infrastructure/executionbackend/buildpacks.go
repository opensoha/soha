package executionbackend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

// BuildpacksRunner reads the administrator-configured execution endpoint. Source
// repositories and application inputs cannot supply URLs or runtime credentials.
type BuildpacksRunner struct {
	endpoint string
	token    string
	client   *http.Client
}

func NewBuildpacksRunner(endpoint, token string) (*BuildpacksRunner, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("invalid Buildpacks runner endpoint")
		}
	}
	return &BuildpacksRunner{endpoint: endpoint, token: token, client: &http.Client{Timeout: 8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (r *BuildpacksRunner) BuildpacksCapability(ctx context.Context, applicationID string) (sohaapi.BuildpacksCapability, error) {
	unavailable := sohaapi.BuildpacksCapability{Reason: "buildpacks_runner_unavailable"}
	if r.endpoint == "" {
		unavailable.Reason = "buildpacks_runner_not_configured"
		return unavailable, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.endpoint+"/api/v1/runtime/buildpacks/capability?applicationId="+url.QueryEscape(applicationID), nil)
	if err != nil {
		return unavailable, nil
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	response, err := r.client.Do(req)
	if err != nil {
		return unavailable, ctx.Err()
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return unavailable, nil
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(content) > 65536 {
		return unavailable, nil
	}
	var envelope sohaapi.BuildpacksCapabilityEnvelope
	if err := json.Unmarshal(content, &envelope); err != nil {
		return unavailable, nil
	}
	return envelope.Data, nil
}
