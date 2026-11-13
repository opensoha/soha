package manifest

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	"slices"
	"testing"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type rootSourceReader struct {
	source  domainresource.ResourceSourceSnapshot
	err     error
	content string
}

func (r *rootSourceReader) GetApplicationSourceYAML(context.Context, domainidentity.Principal, domainresource.ResourceSourceSnapshot) (domainresource.ResourceYAMLView, error) {
	return domainresource.ResourceYAMLView{Content: r.content}, r.err
}

func (r *rootSourceReader) GetImportSource(context.Context, domainidentity.Principal, string, string, string, string, string) (domainresource.ResourceSourceSnapshot, error) {
	return r.source, nil
}
func (r *rootSourceReader) AuthorizeRootCRUpdate(context.Context, domainidentity.Principal, domainresource.ResourceSourceSnapshot) error {
	return r.err
}

type rootEnvironment struct {
	item domaincatalog.ApplicationEnvironment
}

func (r *rootEnvironment) GetApplicationEnvironment(context.Context, domainidentity.Principal, string) (domaincatalog.ApplicationEnvironment, error) {
	return r.item, nil
}

func TestRootCRControlAndApprovalSnapshot(t *testing.T) {
	for _, mode := range []string{"allowed", "observe-only", "external", "owner", "uid", "permission", "changed-after-approval", "control-withdrawn", "guard-stripped", "wrong-document", "status-write", "children", "applied-retry", "changed-retry"} {
		t.Run(mode, func(t *testing.T) {
			service, repo, _, target := newSnapshotTestService()
			source := domainresource.ResourceSourceSnapshot{Resource: domainresource.ResourceRef{ClusterID: target.ClusterID, Namespace: target.Namespace, Name: "db", APIVersion: "unknown.example/v1", Kind: "Database", UID: "uid-1"}, CRDName: "databases.unknown.example", Generation: 2}
			reader := &rootSourceReader{source: source}
			service.rootResources = reader
			encoded, _ := json.Marshal(source)
			target.WorkloadKind = "CustomResource"
			target.WorkloadName = "db"
			target.Metadata = map[string]any{"importSource": "kubernetes", "ownershipMode": "managed", "resourceSource": string(encoded), "rootCRGeneration": int64(2)}
			environment := &rootEnvironment{item: domaincatalog.ApplicationEnvironment{ID: "payments-dev", ApplicationID: "payments", EnvironmentID: "dev", EnvironmentKey: "dev", Targets: []domaincatalog.ReleaseTarget{target}}}
			service.base.environments = environment
			documents := []domainmanifest.RenderedDocument{{APIVersion: source.Resource.APIVersion, Kind: "Database", Namespace: target.Namespace, Name: "db", Content: `{"apiVersion":"unknown.example/v1","kind":"Database","metadata":{"name":"db","namespace":"payments"},"spec":{"size":2}}`}}
			documents = rootControlMode(mode, target, reader, documents)
			root, err := service.freezeRootCR(t.Context(), testPrincipal(), target, "plan", documents)
			valid := slices.Contains([]string{"allowed", "changed-after-approval", "control-withdrawn", "guard-stripped", "applied-retry", "changed-retry"}, mode)
			if !valid {
				if err == nil {
					t.Fatal("unsafe root accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			snapshot := domainmanifest.DeliverySnapshot{RootResource: root, DeliveryPlanID: "plan", TargetID: target.ID, BindingID: repo.binding.ID, ApplicationEnvironmentID: "payments-dev", ClusterID: target.ClusterID, Namespace: target.Namespace, Documents: documents}
			if mode == "guard-stripped" {
				snapshot.RootResource = nil
			}
			if mode == "changed-after-approval" {
				reader.source.Generation++
			}
			if mode == "control-withdrawn" {
				environment.item.Targets[0].ExecutorKind = "observe_only"
			}
			if mode == "applied-retry" || mode == "changed-retry" {
				reader.source.Generation++
				reader.content = documents[0].Content
				if mode == "changed-retry" {
					reader.content = `{"spec":{"size":3}}`
				}
			}
			err = service.validateRootCRSnapshot(t.Context(), testPrincipal(), snapshot, mode == "applied-retry" || mode == "changed-retry")
			allowed := mode == "allowed" || mode == "applied-retry"
			if allowed && err != nil || !allowed && !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("approval revalidation=%v", err)
			}
		})
	}
}

func TestCustomCRManifestPermissionSurvivesCopyAndHistory(t *testing.T) {
	service := newTestService(&testRepository{}, testAuthorizer{})
	files := []domainmanifest.File{{Path: "root.yaml", Content: "apiVersion: unknown.example/v1\nkind: Database\nspec:\n  password: synthetic-secret\n"}}
	if err := service.authorizeSensitiveFiles(t.Context(), domainidentity.Principal{Roles: []string{"editor"}}, files); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("copied CR readable without sensitive permission: %v", err)
	}
}

type rootExecutionPrincipal struct {
	principal      domainidentity.Principal
	err            error
	subject, token string
}

func (r *rootExecutionPrincipal) CurrentExecutionPrincipal(_ context.Context, subject, token string) (domainidentity.Principal, error) {
	r.subject = subject
	r.token = token
	return r.principal, r.err
}
func TestRootCRClaimRestoresCurrentIdentityRatherThanSavedRoles(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		service, _, _, _ := newSnapshotTestService()
		identities := &rootExecutionPrincipal{principal: domainidentity.Principal{UserID: "actor", Roles: []string{"editor"}}}
		if disabled {
			identities.err = apperrors.ErrUnauthorized
		}
		service.executionPrincipals = identities
		payload := structMap(domainmanifest.TaskPayload{RootResource: &sohaapi.ManifestRootResource{UID: "root", Generation: 2}, Action: "apply"})
		task := domaindelivery.ExecutionTask{Payload: payload, SecretPrincipal: domainidentity.Principal{UserID: "actor", Roles: []string{"admin"}, AccessTokenID: "durable-token-id"}}
		err := service.ValidateRootCRExecutionTask(t.Context(), task)
		want := apperrors.ErrAccessDenied
		if disabled {
			want = apperrors.ErrUnauthorized
		}
		if !errors.Is(err, want) || identities.subject != "actor" || identities.token != "durable-token-id" {
			t.Fatalf("stale identity used: %v %+v", err, identities)
		}
	}
}

func TestRootCRObservationUsesDurableIdentityAndCurrentReadPermission(t *testing.T) {
	service, repo, _, _ := newSnapshotTestService()
	root := &sohaapi.ManifestRootResource{TargetID: "target", UID: "root", APIVersion: "unknown.example/v1", Kind: "Database", Namespace: "payments", Name: "db", CrdName: "databases.unknown.example"}
	principal := domainidentity.Principal{UserID: "actor", Roles: []string{"admin"}, AccessTokenID: "token"}
	task := domaindelivery.ExecutionTask{ApplicationEnvironmentID: "payments-dev", Payload: structMap(domainmanifest.TaskPayload{PackageID: "package", BindingID: repo.binding.ID, ClusterID: repo.binding.ClusterID, RootResource: root, Action: "observe"}), SecretPrincipal: principal}
	repo.tasks["last"] = task
	deployment := domainmanifest.Deployment{Spec: domainmanifest.DeploymentSpec{DeliverySnapshot: &domainmanifest.DeliverySnapshot{RootResource: root}}, Status: domainmanifest.DeploymentStatus{LastExecutionTaskID: "last"}}
	got, err := service.rootObservationPrincipal(t.Context(), deployment)
	if err != nil || got.AccessTokenID != principal.AccessTokenID {
		t.Fatalf("observation lost original identity: %v %+v", err, got)
	}
	identities := &rootExecutionPrincipal{principal: principal}
	service.executionPrincipals = identities
	service.rootResources = &rootSourceReader{source: domainresource.ResourceSourceSnapshot{Resource: domainresource.ResourceRef{UID: root.UID, APIVersion: root.APIVersion, Kind: root.Kind}, Generation: 3}}
	source := domainresource.ResourceSourceSnapshot{Resource: domainresource.ResourceRef{ClusterID: repo.binding.ClusterID, Namespace: root.Namespace, Name: root.Name, UID: root.UID}, CRDName: root.CrdName}
	encoded, _ := json.Marshal(source)
	environment := &rootEnvironment{item: domaincatalog.ApplicationEnvironment{Targets: []domaincatalog.ReleaseTarget{{ID: root.TargetID, Enabled: true, ExecutorKind: "manifest_ssa", ConfigRef: repo.binding.ID, ClusterID: repo.binding.ClusterID, Namespace: root.Namespace, WorkloadName: root.Name, Metadata: map[string]any{"importSource": "kubernetes", "ownershipMode": "managed", "resourceSource": string(encoded)}}}}}
	service.base.environments = environment
	if err = service.ValidateRootCRExecutionTask(t.Context(), task); err != nil {
		t.Fatalf("observation must allow changed generation after apply: %v", err)
	}
	environment.item.Targets[0].ExecutorKind = "observe_only"
	if err = service.ValidateRootCRExecutionTask(t.Context(), task); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("withdrawn binding still dispatches frozen configuration: %v", err)
	}
	identities.err = apperrors.ErrUnauthorized
	if err = service.ValidateRootCRExecutionTask(t.Context(), task); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("revoked identity still observes: %v", err)
	}
	delete(repo.tasks, "last")
	if _, err = service.rootObservationPrincipal(t.Context(), deployment); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("missing identity accepted: %v", err)
	}
}

func rootControlMode(mode string, target domaincatalog.ReleaseTarget, reader *rootSourceReader, documents []domainmanifest.RenderedDocument) []domainmanifest.RenderedDocument {
	switch mode {
	case "observe-only":
		target.Metadata["ownershipMode"] = "observe_only"
	case "external":
		reader.source.ExternallyManaged = true
	case "owner":
		reader.source.Owners = []domainresource.ResourceOwnerReference{{UID: "parent"}}
	case "uid":
		reader.source.Resource.UID = "replaced"
	case "permission":
		reader.err = apperrors.ErrAccessDenied
	case "wrong-document":
		documents[0].Content = `{"apiVersion":"unknown.example/v1","kind":"Database","metadata":{"name":"other","namespace":"payments"},"spec":{}}`
	case "status-write":
		documents[0].Content = `{"apiVersion":"unknown.example/v1","kind":"Database","metadata":{"name":"db","namespace":"payments"},"spec":{},"status":{"ready":true}}`
	case "children":
		documents = append(documents, documents[0])
	}
	return documents
}
