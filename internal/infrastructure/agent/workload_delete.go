package agent

import (
	"context"
	"fmt"
	"net/http"

	contractresource "github.com/opensoha/soha/contracts/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const observedWorkloadDeletePath = "/api/v1/platform/ownership-v2/resources/observed"

func (c *Client) DeleteObservedWorkload(ctx context.Context, namespace, kind, name, expectedUID string) error {
	return c.request(ctx, http.MethodDelete, observedWorkloadDeletePath, contractresource.ObservedWorkloadDeleteRequest{
		Namespace: namespace, Kind: kind, Name: name, ExpectedUID: expectedUID,
	}, nil)
}

func observedWorkloadDeleteResponseError(resp *http.Response) error {
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode < http.StatusBadRequest || resp.StatusCode == http.StatusNotImplemented {
		return fmt.Errorf("%w: Agent did not confirm UID-guarded workload deletion", apperrors.ErrUnsupportedOperation)
	}
	return nil
}
