package resourcebackend

import (
	"context"
	"errors"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestAgentResourceManifestDecoderRejectsBoundedInvalidInputs(t *testing.T) {
	t.Parallel()
	creator := &agentResourceCreator{}
	tests := []struct {
		name    string
		content string
		limit   int
	}{
		{name: "invalid yaml", content: "metadata: [", limit: 20},
		{name: "empty documents", content: "---\n---\n", limit: 20},
		{name: "kind list", content: "apiVersion: v1\nkind: List\nmetadata:\n  name: resources\n", limit: 20},
		{name: "document count", content: repeatedConfigMapDocuments(3), limit: 2},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			_, err := creator.ResolveCreateManifests(context.Background(), test.content, test.limit, allowAgentCreateDiscovery)
			if !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("ResolveCreateManifests() error = %v, want invalid argument", err)
			}
		})
	}
}

func repeatedConfigMapDocuments(count int) string {
	var builder strings.Builder
	for index := 0; index < count; index++ {
		if index > 0 {
			builder.WriteString("---\n")
		}
		builder.WriteString("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: item-")
		builder.WriteString(string(rune('a' + index)))
		builder.WriteString("\n")
	}
	return builder.String()
}

type agentCreateDiscoveryStub struct {
	items    []domainresource.CRDView
	err      error
	calls    int
	deadline time.Time
}

func (s *agentCreateDiscoveryStub) ListCRDs(ctx context.Context) ([]domainresource.CRDView, error) {
	s.calls++
	s.deadline, _ = ctx.Deadline()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return s.items, s.err
}
func (*agentCreateDiscoveryStub) DryRunCreateManifest(context.Context, string, string, domainresource.ResolvedCreateManifest) error {
	return nil
}
func (*agentCreateDiscoveryStub) CreateResolvedManifest(context.Context, string, string, domainresource.ResolvedCreateManifest) (domainresource.ResourceYAMLView, error) {
	return domainresource.ResourceYAMLView{}, nil
}

func testAgentCRD() domainresource.CRDView {
	return domainresource.CRDView{Name: "widgets.example.io", Group: "example.io", Kind: "Widget", Plural: "widgets", Scope: "Namespaced", Version: "v1", Versions: []string{"v1", "v1beta1"}}
}
func agentCreateYAML(apiVersion, kind, name string) string {
	return "apiVersion: " + apiVersion + "\nkind: " + kind + "\nmetadata:\n  name: " + name + "\n  namespace: team\n"
}
func TestAgentResourceCreateResolvesServedCRDsOncePerRequest(t *testing.T) {
	stub := &agentCreateDiscoveryStub{items: []domainresource.CRDView{testAgentCRD()}}
	creator := &agentResourceCreator{client: stub}
	content := agentCreateYAML("example.io/v1beta1", "Widget", "first") + "---\n" + agentCreateYAML("example.io/v1", "Widget", "second")
	manifests, err := creator.ResolveCreateManifests(context.Background(), content, 20, allowAgentCreateDiscovery)
	if err != nil || len(manifests) != 2 || stub.calls != 1 {
		t.Fatalf("manifests=%+v calls=%d err=%v", manifests, stub.calls, err)
	}
	if stub.deadline.IsZero() || time.Until(stub.deadline) > agentCreateDiscoveryTimeout {
		t.Fatalf("unbounded deadline: %v", stub.deadline)
	}
	for i, m := range manifests {
		if m.Group != "example.io" || m.Resource != "widgets" || !m.Ref.Namespaced || m.Ref.Namespace != "team" {
			t.Fatalf("manifest %d = %+v", i, m)
		}
	}
	if manifests[0].Ref.APIVersion != "example.io/v1beta1" || manifests[0].Version != "v1beta1" {
		t.Fatalf("input version lost: %+v", manifests[0])
	}
	// A second request must fetch fresh metadata rather than retain stale mappings.
	_, err = creator.ResolveCreateManifests(context.Background(), content, 20, allowAgentCreateDiscovery)
	if err != nil || stub.calls != 2 {
		t.Fatalf("second request calls=%d err=%v", stub.calls, err)
	}
}

func TestAgentResourceCreatePreservesKnownVersionWithoutCRDInventory(t *testing.T) {
	stub := &agentCreateDiscoveryStub{err: errors.New("must not query")}
	creator := &agentResourceCreator{client: stub}
	for _, input := range []struct{ version, kind, resource string }{{"autoscaling/v1", "HorizontalPodAutoscaler", "horizontalpodautoscalers"}, {"gateway.networking.k8s.io/v1beta1", "Gateway", "gateways"}} {
		manifests, err := creator.ResolveCreateManifests(context.Background(), agentCreateYAML(input.version, input.kind, "app"), 20, allowAgentCreateDiscovery)
		if err != nil || len(manifests) != 1 || manifests[0].Ref.APIVersion != input.version || manifests[0].Resource != input.resource {
			t.Fatalf("%s: manifests=%+v err=%v", input.version, manifests, err)
		}
	}
	if stub.calls != 0 {
		t.Fatalf("known resources queried CRDs %d times", stub.calls)
	}
}

func TestAgentResourceCreateRejectsUntrustedDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name      string
		change    func(*domainresource.CRDView)
		duplicate bool
		want      error
	}{
		{name: "wrong group", change: func(c *domainresource.CRDView) { c.Group = "other.io" }, want: apperrors.ErrUnsupportedOperation},
		{name: "wrong kind", change: func(c *domainresource.CRDView) { c.Kind = "Other" }, want: apperrors.ErrUnsupportedOperation},
		{name: "unserved version", change: func(c *domainresource.CRDView) { c.Versions = []string{"v2"} }, want: apperrors.ErrUnsupportedOperation},
		{name: "ambiguous", duplicate: true, want: apperrors.ErrInvalidArgument},
		{name: "bad scope", change: func(c *domainresource.CRDView) { c.Scope = "Unknown" }, want: apperrors.ErrInvalidArgument},
		{name: "bad identity", change: func(c *domainresource.CRDView) { c.Name = "other.example.io" }, want: apperrors.ErrInvalidArgument},
		{name: "subresource", change: func(c *domainresource.CRDView) { c.Plural = "widgets/status"; c.Name = "widgets/status.example.io" }, want: apperrors.ErrInvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			crd := testAgentCRD()
			if tc.change != nil {
				tc.change(&crd)
			}
			stub := &agentCreateDiscoveryStub{items: []domainresource.CRDView{crd}}
			if tc.duplicate {
				stub.items = append(stub.items, crd)
			}
			_, err := (&agentResourceCreator{client: stub}).ResolveCreateManifests(context.Background(), agentCreateYAML("example.io/v1", "Widget", "app"), 20, allowAgentCreateDiscovery)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
		})
	}
}

func TestAgentResourceCreateDiscoveryFailureAndBudget(t *testing.T) {
	sentinel := errors.New("sensitive upstream discovery detail")
	for _, tc := range []struct {
		name      string
		stub      *agentCreateDiscoveryStub
		cancelled bool
		cause     error
	}{
		{name: "list failed", stub: &agentCreateDiscoveryStub{err: sentinel}, cause: sentinel},
		{name: "old Agent empty discovery", stub: &agentCreateDiscoveryStub{}},
		{name: "count budget", stub: &agentCreateDiscoveryStub{items: make([]domainresource.CRDView, agentCreateMaxCRDs+1)}},
		{name: "cancelled", stub: &agentCreateDiscoveryStub{}, cancelled: true, cause: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelled {
				cancel()
			}
			_, err := (&agentResourceCreator{client: tc.stub}).ResolveCreateManifests(ctx, agentCreateYAML("example.io/v1", "Widget", "app"), 20, allowAgentCreateDiscovery)
			if !errors.Is(err, apperrors.ErrUnsupportedOperation) || tc.cause != nil && !errors.Is(err, tc.cause) {
				t.Fatalf("err=%v", err)
			}
			if strings.Contains(err.Error(), "sensitive upstream") {
				t.Fatalf("discovery leaked upstream details: %v", err)
			}
			if tc.stub.calls != 1 {
				t.Fatalf("calls=%d", tc.stub.calls)
			}
		})
	}
}

func TestAgentResourceCreateSameKindCRDUsesItsDiscoveredGroup(t *testing.T) {
	crd := testAgentCRD()
	crd.Kind = "Role" // Same spelling must not borrow builtin RBAC's GVR.
	stub := &agentCreateDiscoveryStub{items: []domainresource.CRDView{crd}}
	manifests, err := (&agentResourceCreator{client: stub}).ResolveCreateManifests(context.Background(), agentCreateYAML("example.io/v1", "Role", "app"), 20, allowAgentCreateDiscovery)
	if err != nil || len(manifests) != 1 || manifests[0].Group != "example.io" || manifests[0].Resource != "widgets" || stub.calls != 1 {
		t.Fatalf("manifests=%+v err=%v", manifests, err)
	}
}

func TestAgentResourceCreateChecksDocumentSizeBeforeDiscovery(t *testing.T) {
	stub := &agentCreateDiscoveryStub{}
	content := agentCreateYAML("example.io/v1", "Widget", "app") + "data:\n  payload: " + strings.Repeat("x", domainresource.ResourceCreateMaxDocumentBytes) + "\n"
	_, err := (&agentResourceCreator{client: stub}).ResolveCreateManifests(context.Background(), content, 20, allowAgentCreateDiscovery)
	if !errors.Is(err, apperrors.ErrInvalidArgument) || stub.calls != 0 {
		t.Fatalf("err=%v discoveryCalls=%d", err, stub.calls)
	}
}

func TestAgentResourceCreateResolvesClusterScopedCRD(t *testing.T) {
	crd := testAgentCRD()
	crd.Scope = "Cluster"
	stub := &agentCreateDiscoveryStub{items: []domainresource.CRDView{crd}}
	manifests, err := (&agentResourceCreator{client: stub}).ResolveCreateManifests(context.Background(), agentCreateYAML("example.io/v1", "Widget", "app"), 20, allowAgentCreateDiscovery)
	if err != nil || len(manifests) != 1 || manifests[0].Ref.Namespaced {
		t.Fatalf("manifests=%+v err=%v", manifests, err)
	}
	// Application resolveManifestTarget owns removal/warning for ignored namespaces.
	if manifests[0].Resource != "widgets" || manifests[0].Ref.APIVersion != "example.io/v1" {
		t.Fatalf("manifest=%+v", manifests[0])
	}
}

func allowAgentCreateDiscovery(context.Context, string, string, string) error { return nil }

func TestAgentCreateDiscoveryRequiresAuthorizationBeforeRemoteList(t *testing.T) {
	client := &agentCreateDiscoveryStub{}
	creator := &agentResourceCreator{client: client}
	manifest := agentCreateYAML("example.io/v1", "Role", "app")
	_, err := creator.ResolveCreateManifests(t.Context(), manifest, 1, func(_ context.Context, version, kind, namespace string) error {
		if version != "example.io/v1" || kind != "Role" || namespace != "team" {
			t.Fatalf("wrong manifest target: %s %s %s", version, kind, namespace)
		}
		return apperrors.ErrAccessDenied
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) || client.calls != 0 {
		t.Fatalf("discovery reached the Agent before authorization: calls=%d, err=%v", client.calls, err)
	}
}
