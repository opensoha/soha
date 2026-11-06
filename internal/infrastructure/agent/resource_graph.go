package agent

import (
	"fmt"
	"net/http"
	"strings"

	contractresource "github.com/opensoha/soha-contracts/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	resourceGraphPath          = "/api/v1/platform/resources/graph"
	storageGraphCapabilityPath = resourceGraphPath + "?capability=" + contractresource.StorageResourceGraphFeature
	rbacGraphCapabilityPath    = resourceGraphPath + "?capability=" + contractresource.RBACResourceGraphFeature
)

func resourceGraphCapabilityPath(kind string) string {
	switch kind {
	case "PersistentVolumeClaim", "PersistentVolume", "StorageClass":
		return storageGraphCapabilityPath
	case "ServiceAccount", "Role", "RoleBinding", "ClusterRole", "ClusterRoleBinding":
		return rbacGraphCapabilityPath
	default:
		return ""
	}
}

func resourceGraphNoRedirect(method, path string) bool {
	switch method {
	case http.MethodHead:
		return path == storageGraphCapabilityPath || path == rbacGraphCapabilityPath
	case http.MethodGet:
		endpoint, _, _ := strings.Cut(path, "?")
		return endpoint == resourceGraphPath
	case http.MethodPost:
		return path == customGraphPagePath || path == customGraphObjectPath
	default:
		return false
	}
}

// A missing route on an older Agent is distinct from a JSON object-not-found response.
func resourceGraphResponseError(resp *http.Response) error {
	if resp.StatusCode == http.StatusNotImplemented || ((resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed) && !strings.Contains(resp.Header.Get("Content-Type"), "application/json")) {
		return fmt.Errorf("%w: upgrade the Agent to support this resource graph", apperrors.ErrUnsupportedOperation)
	}
	return nil
}

// Graph compatibility is limited to the exact Graph endpoint.
func resourceGraphRequestError(path string, resp *http.Response) error {
	switch path {
	case storageGraphCapabilityPath:
		return resourceGraphProbeError("storage", resp)
	case rbacGraphCapabilityPath:
		return resourceGraphProbeError("RBAC", resp)
	case customGraphPagePath, customGraphObjectPath:
		if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest {
			return fmt.Errorf("%w: Agent custom graph metadata endpoint redirected", apperrors.ErrUnsupportedOperation)
		}
		return resourceGraphResponseError(resp)
	}
	endpoint, _, _ := strings.Cut(path, "?")
	if endpoint != resourceGraphPath {
		return nil
	}
	if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest {
		return fmt.Errorf("%w: Agent relationship graph endpoint redirected", apperrors.ErrUnsupportedOperation)
	}
	return resourceGraphResponseError(resp)
}

func resourceGraphProbeError(feature string, resp *http.Response) error {
	if resp.StatusCode != http.StatusNoContent {
		switch resp.StatusCode {
		case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
			return fmt.Errorf("%w: Agent %s relationship graph requires an upgrade", apperrors.ErrUnsupportedOperation, feature)
		}
		if resp.StatusCode < 400 {
			return fmt.Errorf("%w: Agent %s relationship graph capability was not confirmed", apperrors.ErrUnsupportedOperation, feature)
		}
	}
	return nil
}
