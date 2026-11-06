package kubernetes

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"

	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	contractresource "github.com/opensoha/soha-contracts/resource"
)

const resourceCreateTimeout = 10 * time.Second

type resourceCreateCandidate struct {
	document   domainresource.KubernetesResourceDocument
	ref        domainresource.KubernetesResourceRef
	item       *unstructured.Unstructured
	gvr        schema.GroupVersionResource
	namespaced bool
	warnings   []domainresource.KubernetesResourceWarning
}

type resourceCreateValidationError struct {
	code  domainresource.KubernetesResourceCreateErrorCode
	field string
}

func (e resourceCreateValidationError) Error() string { return string(e.code) }

func (c *Client) PreflightResourceCreate(ctx context.Context, request domainresource.KubernetesResourceAgentCreateRequest) domainresource.KubernetesResourceAgentPreflightResult {
	result, _ := c.preflightResourceCreate(ctx, request)
	return result
}

func (c *Client) preflightResourceCreate(ctx context.Context, request domainresource.KubernetesResourceAgentCreateRequest) (domainresource.KubernetesResourceAgentPreflightResult, []resourceCreateCandidate) {
	items, candidates := c.prepareResourceCreate(ctx, request)
	ready := true
	for index, candidate := range candidates {
		if len(items[index].Errors) > 0 {
			ready = false
			continue
		}
		if _, err := c.createResource(ctx, candidate, true); err != nil {
			failure := publicResourceCreateError(resourceCreateErrorCode(err), "cluster dry-run rejected resource", "")
			items[index].DryRun = domainresource.KubernetesResourceDryRunDecision{Status: domainresource.KubernetesResourceDryRunStatusFailed, Error: &failure}
			items[index].Errors = append(items[index].Errors, failure)
			ready = false
			continue
		}
		items[index].DryRun = domainresource.KubernetesResourceDryRunDecision{Status: domainresource.KubernetesResourceDryRunStatusPassed}
	}
	return domainresource.KubernetesResourceAgentPreflightResult{Ready: ready, Items: items}, candidates
}

func (c *Client) CreateResources(ctx context.Context, request domainresource.KubernetesResourceAgentCreateRequest) domainresource.KubernetesResourceAgentCreateResult {
	preflight, candidates := c.preflightResourceCreate(ctx, request)
	result := domainresource.KubernetesResourceAgentCreateResult{
		OperationID: request.OperationID,
		Status:      domainresource.KubernetesResourceCreateBatchStatusFailed,
		Items:       make([]domainresource.KubernetesResourceCreateResultItem, len(preflight.Items)),
	}
	for index, item := range preflight.Items {
		result.Items[index] = preflightCreateResultItem(item)
	}
	if !preflight.Ready {
		return result
	}

	succeeded := 0
	for index, candidate := range candidates {
		created, err := c.createResource(ctx, candidate, false)
		if err != nil {
			failure := publicResourceCreateError(resourceCreateErrorCode(err), "cluster create failed", "")
			result.Items[index].Status = domainresource.KubernetesResourceCreateResultStatusFailed
			result.Items[index].Error = &failure
			break
		}
		result.Items[index].Status = domainresource.KubernetesResourceCreateResultStatusSucceeded
		candidate.ref.Name = created.GetName()
		candidate.ref.UID = string(created.GetUID())
		result.Items[index].ResourceRef = &candidate.ref
		succeeded++
	}
	result.Status = resourceCreateBatchStatus(succeeded, len(result.Items))
	return result
}

func (c *Client) prepareResourceCreate(ctx context.Context, request domainresource.KubernetesResourceAgentCreateRequest) ([]domainresource.KubernetesResourceAgentPreflightItem, []resourceCreateCandidate) {
	items := make([]domainresource.KubernetesResourceAgentPreflightItem, len(request.Documents))
	candidates := make([]resourceCreateCandidate, len(request.Documents))
	mapper, err := c.resourceCreateMapper(ctx)
	if err != nil {
		for index, input := range request.Documents {
			candidate := resourceCreateCandidate{document: input.Document, ref: input.ResourceRef}
			candidates[index] = candidate
			items[index] = failedPreflightItem(candidate, publicResourceCreateError(
				domainresource.KubernetesResourceCreateErrorCodeResourceCapabilityUnsupported,
				"cluster resource discovery failed", "resourceRef",
			))
		}
		return items, candidates
	}

	seen := make(map[string]struct{}, len(request.Documents))
	for index, input := range request.Documents {
		candidate, prepareErr := c.prepareResourceCreateCandidate(mapper, input)
		candidates[index] = candidate
		items[index] = pendingPreflightItem(candidate)
		if prepareErr != nil {
			items[index] = failedPreflightItem(candidate, validationPublicError(prepareErr))
			continue
		}
		key := strings.Join([]string{candidate.ref.APIVersion, candidate.ref.Kind, candidate.ref.Namespace, candidate.ref.Name}, "\x00")
		if _, exists := seen[key]; exists {
			items[index] = failedPreflightItem(candidate, publicResourceCreateError(
				domainresource.KubernetesResourceCreateErrorCodeResourceDryRunFailed,
				"resource is duplicated in request", "documents",
			))
			continue
		}
		seen[key] = struct{}{}
	}
	return items, candidates
}

type resourceCreateMapper interface {
	RESTMapping(schema.GroupKind, ...string) (*meta.RESTMapping, error)
}

func (c *Client) resourceCreateMapper(ctx context.Context) (resourceCreateMapper, error) {
	queryCtx, cancel := context.WithTimeout(ctx, resourceDiscoveryTimeout)
	defer cancel()
	discoveryClient, err := c.discoveryForContext(queryCtx)
	if err != nil {
		return nil, err
	}
	resources, err := restmapper.GetAPIGroupResourcesWithContext(queryCtx, discoveryClient)
	if queryCtx.Err() != nil {
		return nil, queryCtx.Err()
	}
	if err != nil {
		return nil, err
	}
	return restmapper.NewDiscoveryRESTMapper(resources), nil
}

func (c *Client) prepareResourceCreateCandidate(mapper resourceCreateMapper, input domainresource.KubernetesResourceAgentCreateDocument) (resourceCreateCandidate, error) {
	candidate := resourceCreateCandidate{document: input.Document, ref: input.ResourceRef}
	if input.ResourceRef.ClusterID != strings.TrimSpace(c.cfg.ID) {
		return candidate, validationError(domainresource.KubernetesResourceCreateErrorCodeResourceCapabilityUnsupported, "resourceRef.clusterId")
	}
	if contentHash(input.Content) != strings.TrimSpace(input.Document.ContentHash) {
		return candidate, validationError(domainresource.KubernetesResourceCreateErrorCodeResourceKindMismatch, "document.contentHash")
	}
	item, err := decodeSingleResourceDocument(input.Content)
	if err != nil {
		if strings.Contains(err.Error(), "multiple yaml documents") {
			return candidate, validationError(domainresource.KubernetesResourceCreateErrorCodeMultiDocumentNotAllowed, "content")
		}
		return candidate, validationError(domainresource.KubernetesResourceCreateErrorCodeResourceDryRunFailed, "content")
	}
	gvk := item.GroupVersionKind()
	if err := validateResourceCreateIdentity(input, item, gvk); err != nil {
		return candidate, err
	}
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return candidate, validationError(domainresource.KubernetesResourceCreateErrorCodeResourceCapabilityUnsupported, "resourceRef.apiVersion")
	}
	if definition, known := contractresource.LookupKubernetesResourceKind(gvk.Kind); known && definition.Group == gvk.Group {
		if mapping.Resource.Resource != definition.Resource || (mapping.Scope.Name() == meta.RESTScopeNameNamespace) != definition.Namespaced {
			return candidate, validationError(domainresource.KubernetesResourceCreateErrorCodeResourceKindMismatch, "resourceRef.kind")
		}
	}
	candidate.gvr = mapping.Resource
	candidate.namespaced = mapping.Scope.Name() == meta.RESTScopeNameNamespace
	candidate.item = item
	if err := validateResourceCreateScope(&candidate); err != nil {
		return candidate, err
	}
	return candidate, nil
}

func validateResourceCreateIdentity(input domainresource.KubernetesResourceAgentCreateDocument, item *unstructured.Unstructured, gvk schema.GroupVersionKind) error {
	if gvk.Empty() || strings.TrimSpace(resourceCreateIdentityName(item)) == "" {
		return validationError(domainresource.KubernetesResourceCreateErrorCodeResourceKindMismatch, "content.metadata")
	}
	apiVersion := gvk.GroupVersion().String()
	if apiVersion != strings.TrimSpace(input.Document.APIVersion) || apiVersion != strings.TrimSpace(input.ResourceRef.APIVersion) ||
		gvk.Kind != strings.TrimSpace(input.Document.Kind) || gvk.Kind != strings.TrimSpace(input.ResourceRef.Kind) {
		return validationError(domainresource.KubernetesResourceCreateErrorCodeResourceKindMismatch, "content.kind")
	}
	if resourceCreateIdentityName(item) != strings.TrimSpace(input.Document.Name) || resourceCreateIdentityName(item) != strings.TrimSpace(input.ResourceRef.Name) {
		return validationError(domainresource.KubernetesResourceCreateErrorCodeResourceKindMismatch, "content.metadata.name")
	}
	if input.Document.ScopeMode != input.ResourceRef.ScopeMode {
		return validationError(domainresource.KubernetesResourceCreateErrorCodeNamespaceMismatch, "document.scopeMode")
	}
	return nil
}

// Kubernetes gives an explicit name precedence over generateName. Envelope names
// must bind that same identity, never a generateName prefix when name is present.
func resourceCreateIdentityName(item *unstructured.Unstructured) string {
	if item.GetName() != "" {
		return item.GetName()
	}
	return item.GetGenerateName()
}

func validateResourceCreateScope(candidate *resourceCreateCandidate) error {
	targetNamespace := strings.TrimSpace(candidate.ref.Namespace)
	manifestNamespace := strings.TrimSpace(candidate.item.GetNamespace())
	if candidate.namespaced {
		if candidate.ref.ScopeMode != domainresource.KubernetesResourceScopeModeNamespace {
			return validationError(domainresource.KubernetesResourceCreateErrorCodeNamespaceMismatch, "resourceRef.scopeMode")
		}
		if targetNamespace == "" {
			return validationError(domainresource.KubernetesResourceCreateErrorCodeNamespaceRequired, "resourceRef.namespace")
		}
		if documentNamespace := strings.TrimSpace(candidate.document.Namespace); documentNamespace != "" && documentNamespace != targetNamespace {
			return validationError(domainresource.KubernetesResourceCreateErrorCodeNamespaceMismatch, "document.namespace")
		}
		if manifestNamespace != targetNamespace {
			return validationError(domainresource.KubernetesResourceCreateErrorCodeNamespaceMismatch, "content.metadata.namespace")
		}
		return nil
	}
	if candidate.ref.ScopeMode != domainresource.KubernetesResourceScopeModeCluster || targetNamespace != "" {
		return validationError(domainresource.KubernetesResourceCreateErrorCodeNamespaceMismatch, "resourceRef.scopeMode")
	}
	if manifestNamespace != "" {
		candidate.item.SetNamespace("")
		candidate.warnings = append(candidate.warnings, domainresource.KubernetesResourceWarning{
			Code:  domainresource.KubernetesResourceCreateErrorCodeClusterScopedNamespaceIgnored,
			Field: "metadata.namespace", Message: "metadata.namespace was removed for cluster-scoped resource",
		})
	}
	return nil
}

func decodeSingleResourceDocument(content string) (*unstructured.Unstructured, error) {
	reader := yaml.NewYAMLReader(bufio.NewReader(bytes.NewReader([]byte(content))))
	var object map[string]any
	documents := 0
	for {
		raw, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read yaml: %w", err)
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		documents++
		if documents > 1 {
			return nil, errors.New("multiple yaml documents are not allowed in one item")
		}
		jsonDocument, err := yaml.ToJSON(raw)
		if err != nil {
			return nil, fmt.Errorf("convert yaml: %w", err)
		}
		if err := json.Unmarshal(jsonDocument, &object); err != nil {
			return nil, fmt.Errorf("decode yaml: %w", err)
		}
	}
	if documents == 0 || len(object) == 0 {
		return nil, errors.New("resource document is empty")
	}
	return &unstructured.Unstructured{Object: object}, nil
}

func (c *Client) createResource(ctx context.Context, candidate resourceCreateCandidate, dryRun bool) (*unstructured.Unstructured, error) {
	requestCtx, cancel := context.WithTimeout(ctx, resourceCreateTimeout)
	defer cancel()
	var resource dynamic.ResourceInterface
	if candidate.namespaced {
		resource = c.dynamic.Resource(candidate.gvr).Namespace(candidate.ref.Namespace)
	} else {
		resource = c.dynamic.Resource(candidate.gvr)
	}
	created, err := resource.Create(requestCtx, candidate.item.DeepCopy(), resourceCreateOptions(dryRun))
	if err == nil && (created == nil || created.GetName() == "") {
		return nil, errors.New("cluster create returned no resource identity")
	}
	return created, err
}

func resourceCreateOptions(dryRun bool) metav1.CreateOptions {
	if dryRun {
		return metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}, FieldManager: resourceEditFieldManager}
	}
	return metav1.CreateOptions{FieldManager: resourceEditFieldManager}
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func pendingPreflightItem(candidate resourceCreateCandidate) domainresource.KubernetesResourceAgentPreflightItem {
	return domainresource.KubernetesResourceAgentPreflightItem{
		Document: candidate.document, ResourceRef: candidate.ref, Warnings: candidate.warnings,
		Errors: []domainresource.KubernetesResourceCreateError{},
		DryRun: domainresource.KubernetesResourceDryRunDecision{Status: domainresource.KubernetesResourceDryRunStatusSkipped},
	}
}

func failedPreflightItem(candidate resourceCreateCandidate, failure domainresource.KubernetesResourceCreateError) domainresource.KubernetesResourceAgentPreflightItem {
	item := pendingPreflightItem(candidate)
	item.Errors = []domainresource.KubernetesResourceCreateError{failure}
	return item
}

func preflightCreateResultItem(item domainresource.KubernetesResourceAgentPreflightItem) domainresource.KubernetesResourceCreateResultItem {
	result := domainresource.KubernetesResourceCreateResultItem{
		Document: item.Document, Status: domainresource.KubernetesResourceCreateResultStatusNotStarted, Warnings: item.Warnings,
	}
	if len(item.Errors) > 0 {
		result.Status = domainresource.KubernetesResourceCreateResultStatusFailed
		result.Error = &item.Errors[0]
	}
	return result
}

func validationError(code domainresource.KubernetesResourceCreateErrorCode, field string) error {
	return resourceCreateValidationError{code: code, field: field}
}

func validationPublicError(err error) domainresource.KubernetesResourceCreateError {
	var validation resourceCreateValidationError
	if errors.As(err, &validation) {
		return publicResourceCreateError(validation.code, "resource manifest does not match resolved target", validation.field)
	}
	return publicResourceCreateError(domainresource.KubernetesResourceCreateErrorCodeResourceDryRunFailed, "resource manifest is invalid", "content")
}

func publicResourceCreateError(code domainresource.KubernetesResourceCreateErrorCode, message, field string) domainresource.KubernetesResourceCreateError {
	return domainresource.KubernetesResourceCreateError{Code: code, Message: message, Field: field}
}

func resourceCreateErrorCode(err error) domainresource.KubernetesResourceCreateErrorCode {
	if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
		return domainresource.KubernetesResourceCreateErrorCodeResourceCreateDenied
	}
	return domainresource.KubernetesResourceCreateErrorCodeResourceDryRunFailed
}

func resourceCreateBatchStatus(succeeded, total int) domainresource.KubernetesResourceCreateBatchStatus {
	if succeeded == total {
		return domainresource.KubernetesResourceCreateBatchStatusSucceeded
	}
	if succeeded > 0 {
		return domainresource.KubernetesResourceCreateBatchStatusPartial
	}
	return domainresource.KubernetesResourceCreateBatchStatusFailed
}
