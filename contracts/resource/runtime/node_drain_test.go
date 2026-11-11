package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/resource"
	policyv1 "k8s.io/api/policy/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestDrainFeedbackClassifiesTypedEvictionCausesAndClearsRecoveredBlockers(t *testing.T) {
	pdb := &apierrors.StatusError{ErrStatus: metav1.Status{Code: 429, Reason: metav1.StatusReasonTooManyRequests, Message: "private provider message", Details: &metav1.StatusDetails{Causes: []metav1.StatusCause{{Type: policyv1.DisruptionBudgetCause}}}}}
	for _, beta := range []bool{false, true} {
		t.Run(map[bool]string{false: "v1", true: "v1beta1"}[beta], func(t *testing.T) {
			client := fake.NewClientset()
			var evictionError error = pdb
			client.PrependReactor("create", "pods", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, evictionError })
			observed := NewDrainClient(client)
			evict := func() error {
				meta := metav1.ObjectMeta{Name: "pod", Namespace: "team"}
				if beta {
					return observed.PolicyV1beta1().Evictions("team").Evict(context.Background(), &policyv1beta1.Eviction{ObjectMeta: meta})
				}
				return observed.PolicyV1().Evictions("team").Evict(context.Background(), &policyv1.Eviction{ObjectMeta: meta})
			}
			_ = evict()
			assertDrainCode(t, observed.Failure(context.Background(), errors.New("private token")), "node_drain_pdb_blocked")
			evictionError = apierrors.NewTooManyRequests("private throttle", 1)
			_ = evict()
			assertDrainCode(t, observed.Failure(context.Background(), errors.New("private")), "node_drain_eviction_blocked")
			evictionError = nil
			_ = evict()
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			defer cancel()
			assertDrainCode(t, observed.Failure(ctx, errors.New("private")), "node_drain_timeout")
			evictionError = apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "pod", errors.New("private"))
			_ = evict()
			if !apierrors.IsForbidden(observed.Failure(context.Background(), errors.New("private"))) {
				t.Fatal("eviction RBAC denial must retain forbidden semantics")
			}
		})
	}
}

func TestDrainFeedbackKeepsCancellationAndSuccessDistinct(t *testing.T) {
	client := NewDrainClient(fake.NewClientset())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assertDrainCode(t, client.Failure(ctx, ctx.Err()), "node_drain_canceled")
	if err := client.Failure(ctx, nil); err != nil {
		t.Fatalf("success changed: %v", err)
	}
	if resource.IsNodeDrainErrorCode("provider_arbitrary_error") {
		t.Fatal("untrusted error code accepted")
	}
}

func TestDrainFeedbackDoesNotHidePermissionDenialBehindPDB(t *testing.T) {
	client := NewDrainClient(fake.NewClientset())
	client.failures.Store("invalid", struct{}{})
	assertDrainCode(t, client.Failure(context.Background(), errors.New("private")), "node_drain_failed")
	blocked := &apierrors.StatusError{ErrStatus: metav1.Status{Code: 429, Details: &metav1.StatusDetails{Causes: []metav1.StatusCause{{Type: policyv1.DisruptionBudgetCause}}}}}
	_ = client.observe("team", "blocked", blocked)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	assertDrainCode(t, client.Failure(canceled, canceled.Err()), "node_drain_canceled")
	_ = client.observe("team", "denied", apierrors.NewForbidden(schema.GroupResource{Resource: "pods/eviction"}, "denied", errors.New("private")))
	if err := client.Failure(canceled, canceled.Err()); !apierrors.IsForbidden(err) {
		t.Fatalf("permission denial hidden: %v", err)
	}
	if err := client.PodSelectionFailure(context.Background(), []error{apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("private"))}); !apierrors.IsForbidden(err) {
		t.Fatalf("pod list permission hidden: %v", err)
	}
}

func assertDrainCode(t *testing.T, err error, want string) {
	t.Helper()
	var failure *resource.NodeDrainError
	if !errors.As(err, &failure) || failure.Code != want {
		t.Fatalf("got %v; want %s", err, want)
	}
	if strings.Contains(err.Error(), "private") {
		t.Fatal("provider text leaked")
	}
}
