package resource

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestLegacyListCreateRejectsManifestNamespaceBeforeAuthorization(t *testing.T) {
	t.Parallel()
	direct := &creationDirectStub{manifests: []domainresource.ResolvedCreateManifest{testCreateManifest("ConfigMap", "ops", true)}}
	authorizer := &recordingCreateAuthorizer{}
	creation := testResourceCreation(direct, authorizer, nil, domaincluster.ConnectionModeDirectKubeconfig)
	configuration := &Configuration{resourceAccess: creation.resourceAccess, creation: creation}

	_, err := configuration.CreateResourceFromYAML(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", "minio", "ConfigMap", "manifest")
	if !errors.Is(err, apperrors.ErrAccessDenied) || !strings.Contains(err.Error(), "namespace_mismatch") {
		t.Fatalf("CreateResourceFromYAML() error = %v, want namespace_mismatch access denied", err)
	}
	if len(authorizer.requests) != 0 {
		t.Fatalf("authorization requests = %#v, want none before scope mismatch", authorizer.requests)
	}
	if direct.dryRunCalls != 0 || direct.createCalls != 0 {
		t.Fatalf("provider calls dry-run=%d create=%d, want zero", direct.dryRunCalls, direct.createCalls)
	}
}

func TestPreflightCreateResolvesNamespaceRulesBeforeAuthorization(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		source           domainresource.ResourceCreateSource
		manifest         domainresource.ResolvedCreateManifest
		defaultNamespace string
		wantNamespace    string
		wantWarning      string
	}{
		{name: "list fallback", source: domainresource.ResourceCreateSourceList, manifest: testCreateManifest("ConfigMap", "", true), defaultNamespace: "minio", wantNamespace: "minio"},
		{name: "form fallback", source: domainresource.ResourceCreateSourceForm, manifest: testCreateManifest("ConfigMap", "", true), defaultNamespace: "minio", wantNamespace: "minio"},
		{name: "global explicit", source: domainresource.ResourceCreateSourceGlobal, manifest: testCreateManifest("ConfigMap", "ops", true), defaultNamespace: "minio", wantNamespace: "ops"},
		{name: "global fallback", source: domainresource.ResourceCreateSourceGlobal, manifest: testCreateManifest("ConfigMap", "", true), defaultNamespace: "minio", wantNamespace: "minio"},
		{name: "cluster namespace stripped", source: domainresource.ResourceCreateSourceGlobal, manifest: testCreateManifest("ClusterRole", "minio", false), defaultNamespace: "minio", wantNamespace: "", wantWarning: "cluster_scoped_namespace_ignored"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			direct := &creationDirectStub{manifests: []domainresource.ResolvedCreateManifest{test.manifest}}
			authorizer := &recordingCreateAuthorizer{allow: true}
			creation := testResourceCreation(direct, authorizer, allowRuntimePermission{}, domaincluster.ConnectionModeDirectKubeconfig)
			request := domainresource.ResourceCreateRequest{Source: test.source, DefaultNamespace: test.defaultNamespace, Content: "manifest"}
			if isScopedResourceCreateSource(test.source) {
				request.ExpectedKind = test.manifest.Ref.Kind
			}
			result, err := creation.PreflightCreate(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", request)
			if err != nil {
				t.Fatalf("PreflightCreate() error = %v", err)
			}
			if !result.Ready || len(result.Documents) != 1 || result.Documents[0].Resource.Namespace != test.wantNamespace {
				t.Fatalf("PreflightCreate() = %#v, want ready namespace %q", result, test.wantNamespace)
			}
			if len(authorizer.requests) != 1 || authorizer.requests[0].Namespace.Namespace != test.wantNamespace {
				t.Fatalf("authorized namespace = %#v, want %q", authorizer.requests, test.wantNamespace)
			}
			if direct.dryRunNamespace != test.wantNamespace {
				t.Fatalf("dry-run namespace = %q, want %q", direct.dryRunNamespace, test.wantNamespace)
			}
			if test.wantWarning != "" && (len(result.Documents[0].Warnings) != 1 || result.Documents[0].Warnings[0].Code != test.wantWarning) {
				t.Fatalf("warnings = %#v, want %q", result.Documents[0].Warnings, test.wantWarning)
			}
		})
	}
}

func TestExecuteCreateStopsBeforeAnyWriteWhenOneDocumentDenied(t *testing.T) {
	t.Parallel()
	direct := &creationDirectStub{manifests: []domainresource.ResolvedCreateManifest{
		testCreateManifest("ConfigMap", "minio", true), testCreateManifest("Secret", "ops", true),
	}}
	authorizer := &recordingCreateAuthorizer{allow: true, denyNamespace: "ops"}
	creation := testResourceCreation(direct, authorizer, allowRuntimePermission{}, domaincluster.ConnectionModeDirectKubeconfig)

	result, err := creation.ExecuteCreate(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", domainresource.ResourceCreateRequest{
		Source: domainresource.ResourceCreateSourceGlobal, DefaultNamespace: "minio", Content: "two documents",
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) || result.Status != "rejected" {
		t.Fatalf("ExecuteCreate() result=%#v error=%v, want rejected preflight", result, err)
	}
	if direct.createCalls != 0 {
		t.Fatalf("create calls = %d, want zero", direct.createCalls)
	}
}

func TestExecuteCreateRechecksAuthorizationAndAgentCapability(t *testing.T) {
	t.Parallel()
	t.Run("permission revoked after UI preflight", func(t *testing.T) {
		direct := &creationDirectStub{manifests: []domainresource.ResolvedCreateManifest{testCreateManifest("ConfigMap", "minio", true)}}
		authorizer := &recordingCreateAuthorizer{allow: true}
		creation := testResourceCreation(direct, authorizer, allowRuntimePermission{}, domaincluster.ConnectionModeDirectKubeconfig)
		request := domainresource.ResourceCreateRequest{Source: domainresource.ResourceCreateSourceGlobal, DefaultNamespace: "minio", Content: "manifest"}
		if result, err := creation.PreflightCreate(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", request); err != nil || !result.Ready {
			t.Fatalf("initial preflight = %#v, %v", result, err)
		}
		authorizer.allow = false
		_, err := creation.ExecuteCreate(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", request)
		if !errors.Is(err, apperrors.ErrAccessDenied) || direct.createCalls != 0 {
			t.Fatalf("execute error=%v createCalls=%d, want reauthorization rejection", err, direct.createCalls)
		}
	})

	t.Run("agent unsupported", func(t *testing.T) {
		direct := &creationDirectStub{manifests: []domainresource.ResolvedCreateManifest{testCreateManifest("ConfigMap", "minio", true)}}
		creation := testResourceCreation(direct, &recordingCreateAuthorizer{allow: true}, allowRuntimePermission{}, domaincluster.ConnectionModeAgent)
		result, err := creation.PreflightCreate(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", domainresource.ResourceCreateRequest{
			Source: domainresource.ResourceCreateSourceGlobal, DefaultNamespace: "minio", Content: "manifest",
		})
		if err != nil || result.Ready || result.Documents[0].ErrorCode != "resource_capability_unsupported" || direct.dryRunCalls != 0 {
			t.Fatalf("agent preflight = %#v, err=%v", result, err)
		}
	})
}

func TestExecuteCreateReturnsOrderedPartialResultsWithoutRollback(t *testing.T) {
	t.Parallel()
	direct := &creationDirectStub{
		manifests: []domainresource.ResolvedCreateManifest{
			testCreateManifest("ConfigMap", "minio", true),
			testCreateManifest("Secret", "minio", true),
			testCreateManifest("Service", "minio", true),
		},
		createErrAt: 2,
	}
	creation := testResourceCreation(direct, &recordingCreateAuthorizer{allow: true}, allowRuntimePermission{}, domaincluster.ConnectionModeDirectKubeconfig)
	result, err := creation.ExecuteCreate(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", domainresource.ResourceCreateRequest{
		Source: domainresource.ResourceCreateSourceGlobal, DefaultNamespace: "minio", Content: "three documents",
	})
	if err != nil {
		t.Fatalf("ExecuteCreate() error = %v", err)
	}
	if result.Status != "partial" || len(result.Documents) != 3 {
		t.Fatalf("ExecuteCreate() = %#v, want partial three results", result)
	}
	want := []string{"succeeded", "failed", "not_started"}
	for index, status := range want {
		if result.Documents[index].Status != status {
			t.Fatalf("document %d status = %q, want %q", index, result.Documents[index].Status, status)
		}
	}
	if direct.createCalls != 2 {
		t.Fatalf("create calls = %d, want 2", direct.createCalls)
	}
}

func TestExecuteCreateReturnsExistingIdempotentResultBeforePreflight(t *testing.T) {
	t.Parallel()
	request := domainresource.ResourceCreateRequest{
		Source: domainresource.ResourceCreateSourceGlobal, DefaultNamespace: "minio",
		Content: "manifest", RequestID: "request-1",
	}
	direct := &creationDirectStub{manifests: []domainresource.ResolvedCreateManifest{testCreateManifest("ConfigMap", "minio", true)}}
	creation := testResourceCreation(direct, &recordingCreateAuthorizer{allow: true}, allowRuntimePermission{}, domaincluster.ConnectionModeDirectKubeconfig)
	creation.batches = &resourceCreationBatchStub{batch: domainresource.ResourceCreateBatch{
		ID: "batch-1", ActorID: "user-1", ClusterID: "cluster-a", IdempotencyKey: request.RequestID,
		ContentHash: hashResourceCreateRequest(request), Status: domainresource.ResourceCreateBatchSucceeded,
		Documents: []domainresource.ResourceCreateExecutionDocument{{
			Index: 0, Resource: testCreateManifest("ConfigMap", "minio", true).Ref, Status: "succeeded",
		}},
	}}

	result, err := creation.ExecuteCreate(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", request)
	if err != nil || result.Status != "succeeded" || result.OperationID != "batch-1" {
		t.Fatalf("ExecuteCreate() = %#v, %v, want existing successful batch", result, err)
	}
	if direct.dryRunCalls != 0 || direct.createCalls != 0 {
		t.Fatalf("provider calls dry-run=%d create=%d, want zero", direct.dryRunCalls, direct.createCalls)
	}
}

func TestExecuteCreateContinuesWhenOperationRecordingFails(t *testing.T) {
	t.Parallel()
	direct := &creationDirectStub{manifests: []domainresource.ResolvedCreateManifest{testCreateManifest("ConfigMap", "minio", true)}}
	creation := testResourceCreation(direct, &recordingCreateAuthorizer{allow: true}, allowRuntimePermission{}, domaincluster.ConnectionModeDirectKubeconfig)
	batches := &resourceCreationBatchStub{}
	creation.batches = batches
	creation.operations = failingCreateOperations{}

	result, err := creation.ExecuteCreate(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", domainresource.ResourceCreateRequest{
		Source: domainresource.ResourceCreateSourceGlobal, DefaultNamespace: "minio",
		Content: "manifest", RequestID: "request-1",
	})
	if err != nil || result.Status != "succeeded" {
		t.Fatalf("ExecuteCreate() = %#v, %v, want successful creation", result, err)
	}
	if direct.createCalls != 1 || batches.batch.Status != domainresource.ResourceCreateBatchSucceeded {
		t.Fatalf("create calls=%d batch=%#v, want one create and completed batch", direct.createCalls, batches.batch)
	}
}

func TestPreflightCreateAuthorizesResolvedResourceGroup(t *testing.T) {
	t.Parallel()
	manifest := testCreateManifest("Deployment", "minio", true)
	manifest.Ref.APIVersion = "example.io/v1"
	manifest.Group = "example.io"
	manifest.Resource = "deployments"
	direct := &creationDirectStub{manifests: []domainresource.ResolvedCreateManifest{manifest}}
	authorizer := &recordingCreateAuthorizer{allow: true}
	creation := testResourceCreation(direct, authorizer, allowRuntimePermission{}, domaincluster.ConnectionModeDirectKubeconfig)

	result, err := creation.PreflightCreate(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", domainresource.ResourceCreateRequest{
		Source: domainresource.ResourceCreateSourceGlobal, DefaultNamespace: "minio", Content: "manifest",
	})
	if err != nil || !result.Ready {
		t.Fatalf("PreflightCreate() = %#v, %v, want ready", result, err)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Resource.Group != "extensions" {
		t.Fatalf("authorization requests = %#v, want extensions resource group", authorizer.requests)
	}
	if got := result.Documents[0].Authorization.ResourceGroups; len(got) != 1 || got[0] != "extensions" {
		t.Fatalf("authorization resource groups = %#v, want extensions", got)
	}
}

func TestAgentCreateCapabilityNotPublishedDoesNotResolveAgentClient(t *testing.T) {
	t.Parallel()
	clientCalls := 0
	creation := &ResourceCreation{
		resourceAccess: &resourceAccess{
			resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: domaincluster.ConnectionModeAgent}}},
			authorizer: &recordingCreateAuthorizer{allow: true}, permissions: allowRuntimePermission{}, audit: discardAuditRecorder{},
		},
		agent: func(domaincluster.Connection) (AgentResourceCreator, error) {
			clientCalls++
			return &agentCreationStub{}, nil
		},
	}
	_, err := creation.PreflightCreate(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", domainresource.ResourceCreateRequest{
		Source: domainresource.ResourceCreateSourceGlobal, DefaultNamespace: "minio", Content: "manifest",
	})
	if !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("PreflightCreate() error = %v, want unsupported", err)
	}
	if clientCalls != 0 {
		t.Fatalf("agent client factory calls = %d, want zero", clientCalls)
	}
}

func TestResourceCreateInputBoundsAndDuplicateIdentity(t *testing.T) {
	t.Parallel()
	principal := domainidentity.Principal{UserID: "user-1"}
	t.Run("body size", func(t *testing.T) {
		creation := testResourceCreation(&creationDirectStub{}, &recordingCreateAuthorizer{allow: true}, allowRuntimePermission{}, domaincluster.ConnectionModeDirectKubeconfig)
		_, err := creation.PreflightCreate(context.Background(), principal, "cluster-a", domainresource.ResourceCreateRequest{
			Source: domainresource.ResourceCreateSourceGlobal, Content: strings.Repeat("x", domainresource.ResourceCreateMaxBodyBytes+1),
		})
		if !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("oversized preflight error = %v", err)
		}
	})

	t.Run("list multi document", func(t *testing.T) {
		direct := &creationDirectStub{manifests: []domainresource.ResolvedCreateManifest{testCreateManifest("ConfigMap", "", true), testCreateManifest("Secret", "", true)}}
		creation := testResourceCreation(direct, &recordingCreateAuthorizer{allow: true}, allowRuntimePermission{}, domaincluster.ConnectionModeDirectKubeconfig)
		_, err := creation.PreflightCreate(context.Background(), principal, "cluster-a", domainresource.ResourceCreateRequest{
			Source: domainresource.ResourceCreateSourceList, DefaultNamespace: "minio", ExpectedKind: "ConfigMap", Content: "two documents",
		})
		if !errors.Is(err, apperrors.ErrInvalidArgument) || !strings.Contains(err.Error(), "multi_document_not_allowed") {
			t.Fatalf("list multi-document error = %v", err)
		}
	})

	t.Run("duplicate identity", func(t *testing.T) {
		first := testCreateManifest("ConfigMap", "minio", true)
		second := first
		direct := &creationDirectStub{manifests: []domainresource.ResolvedCreateManifest{first, second}}
		creation := testResourceCreation(direct, &recordingCreateAuthorizer{allow: true}, allowRuntimePermission{}, domaincluster.ConnectionModeDirectKubeconfig)
		result, err := creation.PreflightCreate(context.Background(), principal, "cluster-a", domainresource.ResourceCreateRequest{
			Source: domainresource.ResourceCreateSourceGlobal, DefaultNamespace: "minio", Content: "duplicates",
		})
		if err != nil || result.Ready || result.Documents[1].ErrorCode == "" {
			t.Fatalf("duplicate preflight = %#v, err=%v", result, err)
		}
		if direct.createCalls != 0 {
			t.Fatalf("create calls = %d", direct.createCalls)
		}
	})
}

func TestResourceCreateContentHashBindsScopeAndSource(t *testing.T) {
	t.Parallel()
	base := domainresource.ResourceCreateRequest{Source: domainresource.ResourceCreateSourceGlobal, DefaultNamespace: "minio", Content: "same yaml"}
	otherNamespace := base
	otherNamespace.DefaultNamespace = "ops"
	otherSource := base
	otherSource.Source = domainresource.ResourceCreateSourceList
	otherSource.ExpectedKind = "ConfigMap"
	if hashResourceCreateRequest(base) == hashResourceCreateRequest(otherNamespace) {
		t.Fatal("content hash must bind default namespace")
	}
	if hashResourceCreateRequest(base) == hashResourceCreateRequest(otherSource) {
		t.Fatal("content hash must bind create source and expected kind")
	}
}

func TestSecretCreateNeverPersistsOrReturnsManifestValues(t *testing.T) {
	t.Parallel()
	const marker = "LEAK-ME-SECRET-VALUE"
	manifest := testCreateManifest("Secret", "minio", true)
	manifest.Content = "apiVersion: v1\nkind: Secret\ndata:\n  token: " + marker
	direct := &creationDirectStub{manifests: []domainresource.ResolvedCreateManifest{manifest}, createErrAt: 1, createErr: errors.New("admission rejected value " + marker)}
	authorizer := &recordingCreateAuthorizer{allow: true}
	creation := testResourceCreation(direct, authorizer, allowRuntimePermission{}, domaincluster.ConnectionModeDirectKubeconfig)
	audit := &captureCreateAudit{}
	operations := &captureCreateOperations{}
	creation.audit = audit
	creation.operations = operations

	result, err := creation.ExecuteCreate(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", domainresource.ResourceCreateRequest{
		Source: domainresource.ResourceCreateSourceGlobal, DefaultNamespace: "minio", Content: manifest.Content,
	})
	if err != nil {
		t.Fatalf("ExecuteCreate() error = %v", err)
	}
	encoded, marshalErr := json.Marshal(struct {
		Result     domainresource.ResourceCreateExecution
		Audits     []domainaudit.Entry
		Operations []domainoperation.Entry
	}{result, audit.entries, operations.entries})
	if marshalErr != nil {
		t.Fatalf("marshal captured records: %v", marshalErr)
	}
	if strings.Contains(string(encoded), marker) || strings.Contains(string(encoded), manifest.Content) {
		t.Fatalf("sensitive manifest value leaked: %s", encoded)
	}
}

func testResourceCreation(direct DirectResourceCreator, authorizer domainaccess.Authorizer, permissions RuntimePermissionAuthorizer, mode domaincluster.ConnectionMode) *ResourceCreation {
	capabilities := []string(nil)
	if mode == domaincluster.ConnectionModeAgent {
		capabilities = []string{"resource.creation"}
	}
	access := &resourceAccess{
		resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: mode, Capabilities: capabilities}}},
		authorizer: authorizer, permissions: permissions, audit: discardAuditRecorder{},
	}
	creation := &ResourceCreation{resourceAccess: access, direct: direct}
	if mode == domaincluster.ConnectionModeAgent {
		directStub, ok := direct.(*creationDirectStub)
		if !ok {
			panic("direct backend must be a *creationDirectStub")
		}
		stub := &agentCreationStub{manifests: directStub.manifests, dryRunErr: apperrors.ErrUnsupportedOperation}
		creation.agent = func(domaincluster.Connection) (AgentResourceCreator, error) { return stub, nil }
	}
	return creation
}

func testCreateManifest(kind, namespace string, namespaced bool) domainresource.ResolvedCreateManifest {
	return domainresource.ResolvedCreateManifest{
		Index: 0, Ref: domainresource.ResourceCreateRef{APIVersion: "v1", Kind: kind, Name: strings.ToLower(kind), Namespace: namespace, Namespaced: namespaced},
		Version: "v1", Resource: strings.ToLower(kind) + "s", Content: "apiVersion: v1", ContentHash: "document-hash", Object: map[string]any{"metadata": map[string]any{"namespace": namespace}},
	}
}

type creationDirectStub struct {
	manifests       []domainresource.ResolvedCreateManifest
	dryRunCalls     int
	createCalls     int
	dryRunNamespace string
	createErrAt     int
	createErr       error
}

func (s *creationDirectStub) ResolveCreateManifests(context.Context, string, string, int) ([]domainresource.ResolvedCreateManifest, error) {
	result := make([]domainresource.ResolvedCreateManifest, len(s.manifests))
	copy(result, s.manifests)
	for index := range result {
		result[index].Index = index
	}
	return result, nil
}

func (s *creationDirectStub) DryRunCreateManifest(_ context.Context, _ string, _ domainresource.ResolvedCreateManifest, namespace string) error {
	s.dryRunCalls++
	s.dryRunNamespace = namespace
	return nil
}

func (s *creationDirectStub) CreateResolvedManifest(_ context.Context, _ string, manifest domainresource.ResolvedCreateManifest, namespace string) (domainresource.ResourceYAMLView, error) {
	s.createCalls++
	if s.createErrAt > 0 && s.createCalls == s.createErrAt {
		if s.createErr != nil {
			return domainresource.ResourceYAMLView{}, s.createErr
		}
		return domainresource.ResourceYAMLView{}, errors.New("provider create failed")
	}
	return domainresource.ResourceYAMLView{Kind: manifest.Ref.Kind, Name: manifest.Ref.Name, Namespace: namespace}, nil
}

type recordingCreateAuthorizer struct {
	allow         bool
	denyNamespace string
	requests      []domainaccess.Request
}

func (a *recordingCreateAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	a.requests = append(a.requests, request)
	allowed := a.allow && (a.denyNamespace == "" || request.Namespace.Namespace != a.denyNamespace)
	return domainaccess.Decision{Allowed: allowed, Reason: "test policy"}, nil
}

type allowRuntimePermission struct{}

func (allowRuntimePermission) Authorize(context.Context, domainidentity.Principal, string) error {
	return nil
}

type captureCreateAudit struct{ entries []domainaudit.Entry }

func (c *captureCreateAudit) Record(_ context.Context, entry domainaudit.Entry) error {
	c.entries = append(c.entries, entry)
	return nil
}

type captureCreateOperations struct{ entries []domainoperation.Entry }

func (c *captureCreateOperations) Record(_ context.Context, entry domainoperation.Entry) error {
	c.entries = append(c.entries, entry)
	return nil
}

func (c *captureCreateOperations) List(context.Context, domainoperation.Filter) ([]domainoperation.Entry, error) {
	return append([]domainoperation.Entry(nil), c.entries...), nil
}

type failingCreateOperations struct{}

func (failingCreateOperations) Record(context.Context, domainoperation.Entry) error {
	return errors.New("operation store unavailable")
}

func (failingCreateOperations) List(context.Context, domainoperation.Filter) ([]domainoperation.Entry, error) {
	return nil, nil
}

type resourceCreationBatchStub struct {
	batch domainresource.ResourceCreateBatch
}

func (s *resourceCreationBatchStub) GetByIdentity(context.Context, string, string, string) (domainresource.ResourceCreateBatch, error) {
	if s.batch.ID == "" {
		return domainresource.ResourceCreateBatch{}, apperrors.ErrNotFound
	}
	return s.batch, nil
}

func (s *resourceCreationBatchStub) Claim(_ context.Context, actorID, clusterID, key, contentHash string, documents []domainresource.ResourceCreateExecutionDocument) (domainresource.ResourceCreateBatchClaim, error) {
	if s.batch.ID != "" {
		return domainresource.ResourceCreateBatchClaim{Batch: s.batch}, nil
	}
	s.batch = domainresource.ResourceCreateBatch{
		ID: "batch-1", ActorID: actorID, ClusterID: clusterID, IdempotencyKey: key,
		ContentHash: contentHash, Status: domainresource.ResourceCreateBatchRunning,
		Documents: append([]domainresource.ResourceCreateExecutionDocument(nil), documents...),
	}
	return domainresource.ResourceCreateBatchClaim{Batch: s.batch, Created: true}, nil
}

func (s *resourceCreationBatchStub) Get(context.Context, string) (domainresource.ResourceCreateBatch, error) {
	return s.batch, nil
}

func (s *resourceCreationBatchStub) UpdateDocument(_ context.Context, _ string, document domainresource.ResourceCreateExecutionDocument) error {
	for index := range s.batch.Documents {
		if s.batch.Documents[index].Index == document.Index {
			s.batch.Documents[index] = document
			return nil
		}
	}
	return apperrors.ErrNotFound
}

func (s *resourceCreationBatchStub) Complete(_ context.Context, _ string, status domainresource.ResourceCreateBatchStatus) (domainresource.ResourceCreateBatch, error) {
	s.batch.Status = status
	return s.batch, nil
}

type agentCreationStub struct {
	manifests []domainresource.ResolvedCreateManifest
	dryRunErr error
}

func (s *agentCreationStub) ResolveCreateManifests(context.Context, string, int) ([]domainresource.ResolvedCreateManifest, error) {
	return append([]domainresource.ResolvedCreateManifest(nil), s.manifests...), nil
}

func (s *agentCreationStub) DryRunCreateManifest(context.Context, string, string, domainresource.ResolvedCreateManifest) error {
	return s.dryRunErr
}

func (s *agentCreationStub) CreateResolvedManifest(context.Context, string, string, domainresource.ResolvedCreateManifest) (domainresource.ResourceYAMLView, error) {
	return domainresource.ResourceYAMLView{}, nil
}
