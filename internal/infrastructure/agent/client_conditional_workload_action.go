package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	contractresource "github.com/opensoha/soha-contracts/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (c *Client) ExecuteWorkloadActionConditional(ctx context.Context, namespace, kind, name, action string, input contractresource.ConditionalWorkloadActionInput) (contractresource.ConditionalWorkloadActionReceipt, error) {
	if err := input.Validate(kind, action); err != nil {
		return contractresource.ConditionalWorkloadActionReceipt{}, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	var payload struct {
		Data contractresource.ConditionalWorkloadActionReceipt `json:"data"`
	}
	path := conditionalResourcePath + "/" + url.PathEscape(kind) + "/" + url.PathEscape(name) + "/actions/" + url.PathEscape(action) + "?" + url.Values{"namespace": {namespace}}.Encode()
	if err := c.request(ctx, http.MethodPost, path, input, &payload); err != nil {
		return contractresource.ConditionalWorkloadActionReceipt{}, err
	}
	item := payload.Data
	if item.Kind != kind || item.Name != name || item.Namespace != namespace || item.Action != action || item.Preconditions != input.Preconditions || item.UID != input.Preconditions.ExpectedUID || item.ResourceVersion == "" || item.DryRun != input.DryRun || item.Applied == input.DryRun {
		return contractresource.ConditionalWorkloadActionReceipt{}, fmt.Errorf("%w: Agent returned an invalid conditional workload receipt", apperrors.ErrClusterUnready)
	}
	return item, nil
}
