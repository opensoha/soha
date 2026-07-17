package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func (c *Client) DryRunCreateManifest(ctx context.Context, operationID, clusterID string, manifest domainresource.ResolvedCreateManifest) error {
	document, err := agentCreateDocument(clusterID, manifest)
	if err != nil {
		return err
	}
	var payload struct {
		Data sohaapi.KubernetesResourceAgentPreflightResult `json:"data"`
	}
	request := sohaapi.KubernetesResourceAgentCreateRequest{OperationID: operationID, Documents: []sohaapi.KubernetesResourceAgentCreateDocument{document}}
	if err := c.request(ctx, http.MethodPost, "/api/v1/platform/resources/preflight", request, &payload); err != nil {
		if strings.Contains(err.Error(), "status 404") || strings.Contains(err.Error(), "status 405") {
			return fmt.Errorf("%w: agent resource create capability is not published", apperrors.ErrUnsupportedOperation)
		}
		return err
	}
	if payload.Data.Ready && len(payload.Data.Items) == 1 && payload.Data.Items[0].DryRun.Status == sohaapi.KubernetesResourceDryRunStatusPassed {
		return nil
	}
	if len(payload.Data.Items) > 0 && len(payload.Data.Items[0].Errors) > 0 {
		return fmt.Errorf("%w: agent resource dry-run failed: %s", apperrors.ErrInvalidArgument, payload.Data.Items[0].Errors[0].Message)
	}
	return fmt.Errorf("%w: agent resource dry-run did not pass", apperrors.ErrInvalidArgument)
}

func (c *Client) CreateResolvedManifest(ctx context.Context, operationID, clusterID string, manifest domainresource.ResolvedCreateManifest) (domainresource.ResourceYAMLView, error) {
	document, err := agentCreateDocument(clusterID, manifest)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var payload struct {
		Data sohaapi.KubernetesResourceAgentCreateResult `json:"data"`
	}
	request := sohaapi.KubernetesResourceAgentCreateRequest{OperationID: operationID, Documents: []sohaapi.KubernetesResourceAgentCreateDocument{document}}
	if err := c.request(ctx, http.MethodPost, "/api/v1/platform/resources", request, &payload); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if len(payload.Data.Items) != 1 || payload.Data.Items[0].Status != sohaapi.KubernetesResourceCreateResultStatusSucceeded || payload.Data.Items[0].ResourceRef == nil {
		if len(payload.Data.Items) == 1 && payload.Data.Items[0].Error != nil {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("agent resource create failed: %s", payload.Data.Items[0].Error.Message)
		}
		return domainresource.ResourceYAMLView{}, fmt.Errorf("agent resource create returned no successful result")
	}
	ref := payload.Data.Items[0].ResourceRef
	return domainresource.ResourceYAMLView{Kind: ref.Kind, Name: ref.Name, Namespace: ref.Namespace}, nil
}

func agentCreateDocument(clusterID string, manifest domainresource.ResolvedCreateManifest) (sohaapi.KubernetesResourceAgentCreateDocument, error) {
	var object map[string]any
	if err := yaml.Unmarshal([]byte(manifest.Content), &object); err != nil {
		return sohaapi.KubernetesResourceAgentCreateDocument{}, fmt.Errorf("%w: invalid resolved manifest: %v", apperrors.ErrInvalidArgument, err)
	}
	item := &unstructured.Unstructured{Object: object}
	if manifest.Ref.Namespaced {
		item.SetNamespace(manifest.Ref.Namespace)
	} else {
		item.SetNamespace("")
	}
	content, err := yaml.Marshal(item.Object)
	if err != nil {
		return sohaapi.KubernetesResourceAgentCreateDocument{}, fmt.Errorf("marshal agent manifest: %w", err)
	}
	hash := sha256.Sum256(content)
	scopeMode := sohaapi.KubernetesResourceScopeModeCluster
	if manifest.Ref.Namespaced {
		scopeMode = sohaapi.KubernetesResourceScopeModeNamespace
	}
	document := sohaapi.KubernetesResourceDocument{
		Index: manifest.Index, APIVersion: manifest.Ref.APIVersion, Kind: manifest.Ref.Kind,
		Name: manifest.Ref.Name, Namespace: manifest.Ref.Namespace, ScopeMode: scopeMode,
		ContentHash: hex.EncodeToString(hash[:]),
	}
	ref := sohaapi.KubernetesResourceRef{
		APIVersion: manifest.Ref.APIVersion, ClusterID: clusterID, Kind: manifest.Ref.Kind,
		Name: manifest.Ref.Name, Namespace: manifest.Ref.Namespace, ScopeMode: scopeMode,
	}
	return sohaapi.KubernetesResourceAgentCreateDocument{Content: string(content), Document: document, ResourceRef: ref}, nil
}
