package resource

import (
	"context"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"testing"
)

type statefulHistoryDirect struct {
	DirectWorkloads
	calls                              int
	kind                               string
	cluster, namespace, name, revision string
}

func (d *statefulHistoryDirect) ListStatefulSetRolloutHistory(_ context.Context, cluster, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	d.calls++
	d.kind = "StatefulSet"
	d.cluster = cluster
	d.namespace = namespace
	d.name = name
	return []domainresource.RolloutHistoryView{{Name: "db-1", Namespace: namespace, Revision: "1"}}, nil
}
func (d *statefulHistoryDirect) GetStatefulSetRevisionYAML(_ context.Context, cluster, namespace, name, revision string) (domainresource.ResourceYAMLView, error) {
	d.calls++
	d.kind = "StatefulSet"
	d.cluster = cluster
	d.namespace = namespace
	d.name = name
	d.revision = revision
	return domainresource.ResourceYAMLView{Kind: "ControllerRevision", Name: revision, Namespace: namespace}, nil
}

func (d *statefulHistoryDirect) ListDaemonSetRolloutHistory(ctx context.Context, cluster, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	rows, err := d.ListStatefulSetRolloutHistory(ctx, cluster, namespace, name)
	d.kind = "DaemonSet"
	return rows, err
}
func (d *statefulHistoryDirect) GetDaemonSetRevisionYAML(ctx context.Context, cluster, namespace, name, revision string) (domainresource.ResourceYAMLView, error) {
	view, err := d.GetStatefulSetRevisionYAML(ctx, cluster, namespace, name, revision)
	d.kind = "DaemonSet"
	return view, err
}

type statefulHistoryAuthorizer struct {
	requests []domainaccess.Request
	allowed  bool
}

func (a *statefulHistoryAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	a.requests = append(a.requests, request)
	return domainaccess.Decision{Allowed: a.allowed}, nil
}
func TestControllerHistoryAuthorizesBeforeProvider(t *testing.T) {
	for _, kind := range []string{"StatefulSet", "DaemonSet"} {
		for _, allowed := range []bool{true, false} {
			t.Run(kind+map[bool]string{true: "allowed", false: "denied"}[allowed], func(t *testing.T) {
				audit := &workloadAuditRecorder{}
				w := workloadRoutingTestService(audit, nil)
				direct := &statefulHistoryDirect{}
				auth := &statefulHistoryAuthorizer{allowed: allowed}
				w.direct = direct
				if !allowed {
					w.direct = nil
				}
				w.authorizer = auth
				principal := domainidentity.Principal{UserID: "user-1"}
				listRead, yamlRead := w.ListStatefulSetRolloutHistory, w.GetStatefulSetRevisionYAML
				if kind == "DaemonSet" {
					listRead, yamlRead = w.ListDaemonSetRolloutHistory, w.GetDaemonSetRevisionYAML
				}
				_, listErr := listRead(t.Context(), principal, "direct-cluster", "team-a", "db")
				_, yamlErr := yamlRead(t.Context(), principal, "direct-cluster", "team-a", "db", "db-1")
				if !allowed {
					if listErr == nil || yamlErr == nil || direct.calls != 0 {
						t.Fatalf("denied read reached provider: %d %v %v", direct.calls, listErr, yamlErr)
					}
					return
				}
				if listErr != nil || yamlErr != nil || direct.calls != 2 || direct.kind != kind || direct.namespace != "team-a" || direct.name != "db" || direct.revision != "db-1" {
					t.Fatalf("scope/provider error: %#v %v %v", direct, listErr, yamlErr)
				}
				if len(audit.entries) != 2 {
					t.Fatalf("missing read audit: %#v", audit.entries)
				}
				assertControllerViewChecks(t, auth.requests, kind)
			})
		}
	}
}

func assertControllerViewChecks(t *testing.T, requests []domainaccess.Request, kind string) {
	t.Helper()
	viewChecks := 0
	for _, request := range requests {
		if request.Resource.Kind != kind || request.Namespace.Namespace != "team-a" {
			t.Fatalf("wrong authorization scope: %#v", request)
		}
		if request.Action == domainaccess.ActionView {
			viewChecks++
		}
	}
	if viewChecks < 2 {
		t.Fatal("both history reads must authorize view")
	}
}
