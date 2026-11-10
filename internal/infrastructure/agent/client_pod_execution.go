package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const podExecutionPath = "/api/v1/platform/pod-execution-v1"

func (c *Client) podExecutionCapability(ctx context.Context) error {
	var payload struct {
		Data contractresource.AgentPodExecutionCapabilities `json:"data"`
	}
	if err := c.request(ctx, http.MethodGet, podExecutionPath+"/capabilities", nil, &payload); err != nil {
		return err
	}
	item := payload.Data
	if !item.Ready || item.ProtocolVersion != contractresource.PodExecutionProtocolVersion || item.ArgvVersion != "1" || item.MaxTimeoutSeconds < contractresource.PodExecMaxTimeoutSeconds || item.MaxOutputBytes < contractresource.PodArgvMaxOutputBytes {
		return fmt.Errorf("%w: Agent lacks the bounded Pod argv protocol", apperrors.ErrUnsupportedOperation)
	}
	return nil
}

func (c *Client) InspectPodExecutionTarget(ctx context.Context, namespace, name, uid, container string) (contractresource.PodExecutionTarget, error) {
	if err := c.podExecutionCapability(ctx); err != nil {
		return contractresource.PodExecutionTarget{}, err
	}
	var payload struct {
		Data contractresource.PodExecutionTarget `json:"data"`
	}
	query := url.Values{"namespace": {namespace}, "expectedUid": {uid}, "container": {container}}
	err := c.request(ctx, http.MethodGet, podExecutionPath+"/pods/"+url.PathEscape(name)+"/target?"+query.Encode(), nil, &payload)
	return payload.Data, err
}

func (c *Client) ExecutePodArgv(ctx context.Context, namespace, name string, input contractresource.PodArgvInput) (contractresource.PodArgvResult, error) {
	var payload struct {
		Data contractresource.PodArgvResult `json:"data"`
	}
	// Copy the client for this bounded long request. Never change the shared
	// timeout, and never retry a command after an uncertain transport result.
	boundedClient := *c.httpClient
	boundedClient.Timeout = time.Duration(input.TimeoutSeconds+10) * time.Second
	bounded := *c
	bounded.httpClient = &boundedClient
	query := url.Values{"namespace": {namespace}}
	err := bounded.request(ctx, http.MethodPost, podExecutionPath+"/pods/"+url.PathEscape(name)+"/argv?"+query.Encode(), input, &payload)
	if err == nil && payload.Data.Validate(namespace, name, input) != nil {
		err = fmt.Errorf("%w: Agent returned an invalid Pod execution result", apperrors.ErrClusterUnready)
	}
	return payload.Data, err
}
