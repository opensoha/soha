package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/redaction"
)

type resourceCreateRiskPolicy interface {
	Check(context.Context, domainidentity.Principal, HighRiskResourceRequest) error
}

// ResourceCreation owns manifest decoding, final scope resolution,
// authorization, dry-run and create ordering for every YAML creation entry.
type ResourceCreation struct {
	*resourceAccess
	direct     DirectResourceCreator
	agent      AgentClientFactory[AgentResourceCreator]
	risk       resourceCreateRiskPolicy
	operations CreationOperationStore
	batches    ResourceCreationBatchRepository
}

func (s *ResourceCreation) PreflightCreate(ctx context.Context, principal domainidentity.Principal, clusterID string, request domainresource.ResourceCreateRequest) (domainresource.ResourceCreatePreflight, error) {
	request, err := normalizeResourceCreateRequest(request)
	if err != nil {
		return domainresource.ResourceCreatePreflight{}, err
	}
	if s == nil {
		return domainresource.ResourceCreatePreflight{}, fmt.Errorf("%w: resource creation service is not configured", apperrors.ErrClusterUnready)
	}
	if request.Source == domainresource.ResourceCreateSourceGlobal {
		if s.permissions == nil {
			return domainresource.ResourceCreatePreflight{}, fmt.Errorf("%w: global resource creation permission resolver is unavailable", apperrors.ErrAccessDenied)
		}
		if err := s.permissions.Authorize(ctx, principal, "platform.resource.create"); err != nil {
			return domainresource.ResourceCreatePreflight{}, fmt.Errorf("%w: global resource creation permission is required", apperrors.ErrAccessDenied)
		}
	}
	_, manifests, err := s.resolveCreateManifests(ctx, clusterID, request.Content)
	if err != nil {
		return domainresource.ResourceCreatePreflight{}, err
	}
	if isScopedResourceCreateSource(request.Source) && len(manifests) != 1 {
		return domainresource.ResourceCreatePreflight{}, fmt.Errorf("%w: multi_document_not_allowed: list creation accepts exactly one document", apperrors.ErrInvalidArgument)
	}
	result := domainresource.ResourceCreatePreflight{
		Ready:       true,
		ContentHash: hashResourceCreateRequest(request),
		Documents:   make([]domainresource.ResourceCreateDocument, 0, len(manifests)),
	}
	identities := make(map[string]struct{}, len(manifests))
	for _, manifest := range manifests {
		document := s.preflightDocument(ctx, principal, clusterID, request, manifest)
		if document.Status == "ready" {
			identity := resourceCreateIdentity(document.Resource)
			if _, exists := identities[identity]; exists {
				document = failCreateDocument(document, "resource_kind_mismatch", fmt.Errorf("%w: duplicate resource identity %s", apperrors.ErrInvalidArgument, identity))
			} else {
				identities[identity] = struct{}{}
			}
		}
		if document.Status != "ready" {
			result.Ready = false
		}
		result.Documents = append(result.Documents, document)
	}
	return result, nil
}

func resourceCreateIdentity(ref domainresource.ResourceCreateRef) string {
	return strings.Join([]string{ref.APIVersion, strings.ToLower(ref.Kind), ref.Namespace, ref.Name}, "|")
}

func (s *ResourceCreation) preflightDocument(ctx context.Context, principal domainidentity.Principal, clusterID string, request domainresource.ResourceCreateRequest, manifest domainresource.ResolvedCreateManifest) domainresource.ResourceCreateDocument {
	document := domainresource.ResourceCreateDocument{
		Index: manifest.Index, Resource: manifest.Ref, Status: "invalid",
		OriginalNamespace: manifest.Ref.Namespace, DocumentHash: manifest.ContentHash,
		Authorization: domainresource.ResourceCreateCheck{Reason: "not evaluated"},
		Capability:    domainresource.ResourceCreateCapability{Key: "resource.create", Status: "unsupported", Reason: "not evaluated"},
		DryRun:        domainresource.ResourceDryRunCheck{Message: "not evaluated"},
	}
	warnings, err := resolveManifestTarget(&manifest, request)
	document.Resource = manifest.Ref
	document.Warnings = warnings
	if err != nil {
		return failCreateDocument(document, createErrorCode(err), err)
	}
	connection, decision, err := s.authorize(ctx, principal, clusterID, manifest.Ref.Namespace, manifest.Ref.Kind, domainaccess.ActionCreate)
	if connection.Summary.ID != "" {
		document.Capability = resourceCreateCapability(connection)
	}
	if err != nil {
		document.Authorization = domainresource.ResourceCreateCheck{Reason: publicCreateError(err)}
		return failCreateDocument(document, "resource_create_denied", err)
	}
	document.Authorization = domainresource.ResourceCreateCheck{
		Allowed: decision.Allowed, Reason: decision.Reason, AllowedActions: stringifyActions(decision.AllowedActions),
		ClusterIDs: []string{connection.Summary.ID}, Namespaces: []string{}, ResourceGroups: []string{manifest.Group}, ResourceKinds: []string{manifest.Ref.Kind},
	}
	if decision.ResourceScope != nil {
		document.Authorization.ClusterIDs = append([]string(nil), decision.ResourceScope.Clusters...)
		document.Authorization.Namespaces = append([]string(nil), decision.ResourceScope.Namespaces...)
		if len(decision.ResourceScope.ResourceGroups) > 0 {
			document.Authorization.ResourceGroups = append([]string(nil), decision.ResourceScope.ResourceGroups...)
		}
		if len(decision.ResourceScope.ResourceKinds) > 0 {
			document.Authorization.ResourceKinds = append([]string(nil), decision.ResourceScope.ResourceKinds...)
		}
	}
	if manifest.Ref.Namespace != "" && len(document.Authorization.Namespaces) == 0 {
		document.Authorization.Namespaces = []string{manifest.Ref.Namespace}
	}
	if s.risk != nil {
		err = s.risk.Check(ctx, principal, HighRiskResourceRequest{
			APIVersion: manifest.Ref.APIVersion, Group: manifest.Group, Resource: manifest.Resource, Kind: manifest.Ref.Kind,
			ClusterID: clusterID, Namespace: manifest.Ref.Namespace, Object: manifest.Object,
		})
		if err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, manifest.Ref.Namespace, manifest.Ref.Kind, manifest.Ref.Name, string(domainaccess.ActionCreate), "deny", publicCreateError(err))
			return failCreateDocument(document, createErrorCode(err), err)
		}
	}
	if err = s.dryRunCreateManifest(ctx, connection, clusterID, manifest); err != nil {
		if errors.Is(err, apperrors.ErrUnsupportedOperation) {
			document.Capability.Status = "unsupported"
			document.Capability.Reason = publicCreateError(err)
			return failCreateDocument(document, "resource_capability_unsupported", err)
		}
		document.DryRun = domainresource.ResourceDryRunCheck{Message: publicCreateResourceError(manifest.Ref.Kind, err)}
		document.Status = "invalid"
		document.ErrorCode = "resource_dry_run_failed"
		document.Error = publicCreateResourceError(manifest.Ref.Kind, err)
		return document
	}
	document.DryRun = domainresource.ResourceDryRunCheck{Valid: true}
	document.Status = "ready"
	document.ErrorCode = ""
	document.Error = ""
	return document
}

func (s *ResourceCreation) ExecuteCreate(ctx context.Context, principal domainidentity.Principal, clusterID string, request domainresource.ResourceCreateRequest) (domainresource.ResourceCreateExecution, error) {
	request, err := normalizeResourceCreateRequest(request)
	if err != nil {
		return domainresource.ResourceCreateExecution{}, err
	}
	preflight, err := s.PreflightCreate(ctx, principal, clusterID, request)
	if err != nil {
		return domainresource.ResourceCreateExecution{}, err
	}
	if !preflight.Ready {
		return executionFromRejectedPreflight(preflight), preflightRejectionError(preflight)
	}
	connection, manifests, err := s.resolveCreateManifests(ctx, clusterID, request.Content)
	if err != nil {
		return domainresource.ResourceCreateExecution{}, err
	}
	for index := range manifests {
		if _, err := resolveManifestTarget(&manifests[index], request); err != nil {
			return domainresource.ResourceCreateExecution{}, err
		}
	}
	operationID := uuid.NewString()
	result := domainresource.ResourceCreateExecution{
		OperationID: operationID, ContentHash: preflight.ContentHash, Status: "running",
		Documents: make([]domainresource.ResourceCreateExecutionDocument, len(manifests)),
	}
	for index, manifest := range manifests {
		result.Documents[index] = domainresource.ResourceCreateExecutionDocument{Index: manifest.Index, Resource: manifest.Ref, Status: "not_started", DocumentHash: manifest.ContentHash}
	}
	if request.RequestID != "" && s.batches != nil {
		claim, claimErr := s.batches.Claim(ctx, principal.UserID, clusterID, request.RequestID, result.ContentHash, result.Documents)
		if claimErr != nil {
			return domainresource.ResourceCreateExecution{}, claimErr
		}
		if !claim.Created {
			return executionFromResourceCreateBatch(claim.Batch), nil
		}
		operationID = claim.Batch.ID
		result.OperationID = operationID
	}
	if err := s.recordCreateBatch(ctx, principal, clusterID, request.RequestID, result, "running"); err != nil {
		return domainresource.ResourceCreateExecution{}, fmt.Errorf("record resource creation batch: %w", err)
	}
	result.Status = "succeeded"
	for index, manifest := range manifests {
		created, createErr := s.createResolvedManifest(ctx, connection, operationID, clusterID, manifest)
		if createErr != nil {
			result.Status = "failed"
			if index > 0 {
				result.Status = "partial"
			}
			result.Documents[index].Status = "failed"
			result.Documents[index].ErrorCode = createErrorCode(createErr)
			result.Documents[index].Error = publicCreateResourceError(manifest.Ref.Kind, createErr)
			_ = s.recordAudit(ctx, principal, clusterID, manifest.Ref.Namespace, manifest.Ref.Kind, manifest.Ref.Name, string(domainaccess.ActionCreate), "failure", publicCreateResourceError(manifest.Ref.Kind, createErr))
			if persistErr := s.updateCreateBatchDocument(ctx, result.OperationID, result.Documents[index]); persistErr != nil {
				return result, fmt.Errorf("persist failed resource creation result: %w", persistErr)
			}
			break
		}
		result.Documents[index].Status = "succeeded"
		result.Documents[index].Resource.Name = created.Name
		result.Documents[index].Resource.Namespace = created.Namespace
		_ = s.recordAudit(ctx, principal, clusterID, created.Namespace, created.Kind, created.Name, string(domainaccess.ActionCreate), "success", "created resource from manifest")
		s.recordCreateChild(ctx, principal, clusterID, request.RequestID, operationID, result.Documents[index])
		if persistErr := s.updateCreateBatchDocument(ctx, result.OperationID, result.Documents[index]); persistErr != nil {
			return result, fmt.Errorf("persist resource creation result: %w", persistErr)
		}
	}
	if persistErr := s.completeCreateBatch(ctx, result.OperationID, result.Status); persistErr != nil {
		return result, fmt.Errorf("complete resource creation batch: %w", persistErr)
	}
	if err := s.recordCreateBatch(ctx, principal, clusterID, request.RequestID, result, result.Status); err != nil {
		return result, fmt.Errorf("record resource creation result: %w", err)
	}
	return result, nil
}

func (s *ResourceCreation) resolveCreateManifests(ctx context.Context, clusterID, content string) (domaincluster.Connection, []domainresource.ResolvedCreateManifest, error) {
	connection, err := s.loadConnection(ctx, clusterID)
	if err != nil {
		return domaincluster.Connection{}, nil, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		capability := resourceCreateCapability(connection)
		if capability.Status != "available" {
			return connection, nil, unsupportedAgentOperation(capability.Reason)
		}
		if s.agent == nil {
			return connection, nil, unsupportedAgentOperation("resource create capability is not published by the control plane")
		}
		client, err := s.agent(connection)
		if err != nil {
			return connection, nil, err
		}
		manifests, err := client.ResolveCreateManifests(ctx, content, domainresource.ResourceCreateMaxDocuments)
		return connection, manifests, err
	}
	if s.direct == nil {
		return connection, nil, fmt.Errorf("%w: direct resource creation adapter is not configured", apperrors.ErrClusterUnready)
	}
	manifests, err := s.direct.ResolveCreateManifests(ctx, clusterID, content, domainresource.ResourceCreateMaxDocuments)
	return connection, manifests, err
}

func (s *ResourceCreation) dryRunCreateManifest(ctx context.Context, connection domaincluster.Connection, clusterID string, manifest domainresource.ResolvedCreateManifest) error {
	if connection.Summary.ConnectionMode != domaincluster.ConnectionModeAgent {
		return s.direct.DryRunCreateManifest(ctx, clusterID, manifest, manifest.Ref.Namespace)
	}
	client, err := s.agent(connection)
	if err != nil {
		return err
	}
	return client.DryRunCreateManifest(ctx, "preflight-"+uuid.NewString(), clusterID, manifest)
}

func (s *ResourceCreation) createResolvedManifest(ctx context.Context, connection domaincluster.Connection, operationID, clusterID string, manifest domainresource.ResolvedCreateManifest) (domainresource.ResourceYAMLView, error) {
	if connection.Summary.ConnectionMode != domaincluster.ConnectionModeAgent {
		return s.direct.CreateResolvedManifest(ctx, clusterID, manifest, manifest.Ref.Namespace)
	}
	client, err := s.agent(connection)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return client.CreateResolvedManifest(ctx, operationID, clusterID, manifest)
}

func preflightRejectionError(preflight domainresource.ResourceCreatePreflight) error {
	for _, document := range preflight.Documents {
		if document.ErrorCode == "" {
			continue
		}
		sentinel := apperrors.ErrInvalidArgument
		if document.ErrorCode == "resource_create_denied" || document.ErrorCode == "high_risk_permission_required" || document.ErrorCode == "namespace_mismatch" {
			sentinel = apperrors.ErrAccessDenied
		}
		if document.ErrorCode == "resource_capability_unsupported" {
			sentinel = apperrors.ErrUnsupportedOperation
		}
		return fmt.Errorf("%w: %s: %s", sentinel, document.ErrorCode, document.Error)
	}
	return fmt.Errorf("%w: resource preflight is not ready", apperrors.ErrInvalidArgument)
}

func normalizeResourceCreateRequest(request domainresource.ResourceCreateRequest) (domainresource.ResourceCreateRequest, error) {
	request.DefaultNamespace = strings.TrimSpace(request.DefaultNamespace)
	request.ExpectedAPIVersion = strings.TrimSpace(request.ExpectedAPIVersion)
	request.ExpectedKind = strings.TrimSpace(request.ExpectedKind)
	request.RequestID = strings.TrimSpace(request.RequestID)
	if request.Source != domainresource.ResourceCreateSourceList && request.Source != domainresource.ResourceCreateSourceGlobal && request.Source != domainresource.ResourceCreateSourceForm {
		return request, fmt.Errorf("%w: create source must be list or global", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(request.Content) == "" {
		return request, fmt.Errorf("%w: yaml content is required", apperrors.ErrInvalidArgument)
	}
	if len(request.Content) > domainresource.ResourceCreateMaxBodyBytes {
		return request, fmt.Errorf("%w: yaml content exceeds %d bytes", apperrors.ErrInvalidArgument, domainresource.ResourceCreateMaxBodyBytes)
	}
	if isScopedResourceCreateSource(request.Source) && request.ExpectedKind == "" {
		return request, fmt.Errorf("%w: expectedKind is required for list creation", apperrors.ErrInvalidArgument)
	}
	return request, nil
}

func resolveManifestTarget(manifest *domainresource.ResolvedCreateManifest, request domainresource.ResourceCreateRequest) ([]domainresource.ResourceCreateWarning, error) {
	if request.ExpectedKind != "" && !strings.EqualFold(request.ExpectedKind, manifest.Ref.Kind) {
		return nil, fmt.Errorf("%w: resource_kind_mismatch: manifest kind %s does not match %s", apperrors.ErrInvalidArgument, manifest.Ref.Kind, request.ExpectedKind)
	}
	if request.ExpectedAPIVersion != "" && request.ExpectedAPIVersion != manifest.Ref.APIVersion {
		return nil, fmt.Errorf("%w: resource_kind_mismatch: manifest apiVersion %s does not match %s", apperrors.ErrInvalidArgument, manifest.Ref.APIVersion, request.ExpectedAPIVersion)
	}
	explicitNamespace := strings.TrimSpace(manifest.Ref.Namespace)
	if !manifest.Ref.Namespaced {
		manifest.Ref.Namespace = ""
		if explicitNamespace == "" {
			return nil, nil
		}
		return []domainresource.ResourceCreateWarning{{
			Code: "cluster_scoped_namespace_ignored", Message: "metadata.namespace is ignored for cluster-scoped resources",
		}}, nil
	}
	if isScopedResourceCreateSource(request.Source) {
		if request.DefaultNamespace == "" {
			return nil, fmt.Errorf("%w: namespace_required: list creation requires a namespace", apperrors.ErrInvalidArgument)
		}
		if explicitNamespace != "" && explicitNamespace != request.DefaultNamespace {
			return nil, fmt.Errorf("%w: namespace_mismatch: manifest namespace %s does not match list namespace %s", apperrors.ErrAccessDenied, explicitNamespace, request.DefaultNamespace)
		}
		manifest.Ref.Namespace = request.DefaultNamespace
		return nil, nil
	}
	if explicitNamespace != "" {
		manifest.Ref.Namespace = explicitNamespace
		return nil, nil
	}
	if request.DefaultNamespace == "" {
		return nil, fmt.Errorf("%w: namespace_required: manifest or default namespace is required", apperrors.ErrInvalidArgument)
	}
	manifest.Ref.Namespace = request.DefaultNamespace
	return nil, nil
}

func isScopedResourceCreateSource(source domainresource.ResourceCreateSource) bool {
	return source == domainresource.ResourceCreateSourceList || source == domainresource.ResourceCreateSourceForm
}

func failCreateDocument(document domainresource.ResourceCreateDocument, code string, err error) domainresource.ResourceCreateDocument {
	document.Status = "invalid"
	document.ErrorCode = code
	document.Error = publicCreateError(err)
	return document
}

func executionFromRejectedPreflight(preflight domainresource.ResourceCreatePreflight) domainresource.ResourceCreateExecution {
	result := domainresource.ResourceCreateExecution{ContentHash: preflight.ContentHash, Status: "rejected", Documents: make([]domainresource.ResourceCreateExecutionDocument, len(preflight.Documents))}
	for index, document := range preflight.Documents {
		result.Documents[index] = domainresource.ResourceCreateExecutionDocument{Index: document.Index, Resource: document.Resource, Status: "not_started", ErrorCode: document.ErrorCode, Error: document.Error, DocumentHash: document.DocumentHash}
	}
	return result
}

func executionFromResourceCreateBatch(batch domainresource.ResourceCreateBatch) domainresource.ResourceCreateExecution {
	status := string(batch.Status)
	if batch.Status == domainresource.ResourceCreateBatchFailed {
		for _, document := range batch.Documents {
			if document.Status == "succeeded" {
				status = "partial"
				break
			}
		}
	}
	return domainresource.ResourceCreateExecution{OperationID: batch.ID, ContentHash: batch.ContentHash, Status: status, Documents: batch.Documents}
}

func (s *ResourceCreation) updateCreateBatchDocument(ctx context.Context, batchID string, document domainresource.ResourceCreateExecutionDocument) error {
	if s.batches != nil && batchID != "" {
		return s.batches.UpdateDocument(ctx, batchID, document)
	}
	return nil
}

func (s *ResourceCreation) completeCreateBatch(ctx context.Context, batchID, status string) error {
	if s.batches == nil || batchID == "" {
		return nil
	}
	batchStatus := domainresource.ResourceCreateBatchSucceeded
	if status != "succeeded" {
		batchStatus = domainresource.ResourceCreateBatchFailed
	}
	_, err := s.batches.Complete(ctx, batchID, batchStatus)
	return err
}

func (s *ResourceCreation) recordCreateBatch(ctx context.Context, principal domainidentity.Principal, clusterID, requestID string, result domainresource.ResourceCreateExecution, status string) error {
	if s.operations == nil {
		return nil
	}
	documents := make([]map[string]any, 0, len(result.Documents))
	for _, document := range result.Documents {
		documents = append(documents, map[string]any{
			"index": document.Index, "apiVersion": document.Resource.APIVersion, "kind": document.Resource.Kind,
			"name": document.Resource.Name, "namespace": document.Resource.Namespace, "status": document.Status,
			"errorCode": document.ErrorCode,
		})
	}
	entry := operationentry.New(ctx, principal, "platform.resource_creation.batch", map[string]any{
		"module": "platform", "clusterId": clusterID, "targetId": result.OperationID,
		"targetLabel": result.OperationID,
	}, status, "resource creation batch "+status, map[string]any{
		"operationId": result.OperationID, "contentHash": result.ContentHash, "documents": documents,
	})
	if requestID != "" {
		entry.RequestID = requestID
	}
	return s.operations.Record(ctx, entry)
}

func (s *ResourceCreation) recordCreateChild(ctx context.Context, principal domainidentity.Principal, clusterID, requestID, operationID string, document domainresource.ResourceCreateExecutionDocument) {
	if s.operations == nil {
		return
	}
	entry := operationentry.New(ctx, principal, "platform.resource_creation.resource", map[string]any{
		"module": "platform", "clusterId": clusterID, "namespace": document.Resource.Namespace,
		"resourceKind": document.Resource.Kind, "resourceName": document.Resource.Name,
		"targetId": document.Resource.Name, "targetLabel": document.Resource.Name,
	}, "success", "created resource from manifest", map[string]any{
		"operationId": operationID, "documentIndex": document.Index, "apiVersion": document.Resource.APIVersion,
	})
	if requestID != "" {
		entry.RequestID = requestID
	}
	_ = s.operations.Record(ctx, entry)
}

func hashResourceCreateRequest(request domainresource.ResourceCreateRequest) string {
	payload, _ := json.Marshal(struct {
		Source             domainresource.ResourceCreateSource `json:"source"`
		DefaultNamespace   string                              `json:"defaultNamespace,omitempty"`
		ResourceGroup      string                              `json:"resourceGroup,omitempty"`
		ExpectedAPIVersion string                              `json:"expectedApiVersion,omitempty"`
		ExpectedKind       string                              `json:"expectedKind,omitempty"`
		Content            string                              `json:"content"`
	}{
		Source: request.Source, DefaultNamespace: request.DefaultNamespace, ResourceGroup: request.ResourceGroup,
		ExpectedAPIVersion: request.ExpectedAPIVersion, ExpectedKind: request.ExpectedKind, Content: request.Content,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func createErrorCode(err error) string {
	message := err.Error()
	for _, code := range []string{"resource_kind_mismatch", "namespace_mismatch", "namespace_required", "multi_document_not_allowed", "high_risk_permission_required"} {
		if strings.Contains(message, code) {
			return code
		}
	}
	switch {
	case errors.Is(err, apperrors.ErrAccessDenied):
		return "resource_create_denied"
	case errors.Is(err, apperrors.ErrUnsupportedOperation):
		return "resource_capability_unsupported"
	case errors.Is(err, apperrors.ErrConflict):
		return "resource_already_exists"
	default:
		return "resource_create_failed"
	}
}

func publicCreateError(err error) string {
	if err == nil {
		return ""
	}
	return redaction.Text(strings.TrimSpace(err.Error()))
}

func publicCreateResourceError(kind string, err error) string {
	if strings.EqualFold(strings.TrimSpace(kind), "Secret") {
		return "Kubernetes rejected the Secret resource; sensitive provider details were omitted"
	}
	return publicCreateError(err)
}
