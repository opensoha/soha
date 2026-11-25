package bootstrap

import (
	"context"
	"net/url"
	"strings"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/infrastructure/hostedappdata"
	"github.com/opensoha/soha/internal/infrastructure/hostedruntime"
	corev1 "k8s.io/api/core/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestHostedRuntimeCleanupUsesSecretOwnershipWithoutEnvironmentSnapshot(t *testing.T) {
	rev := domain.Revision{ID: "rev_0123456789abcdef0123456789abcdef", AppID: "app_fedcba9876543210fedcba9876543210"}
	runtimeAdapter, err := hostedruntime.New(dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme()), "soha-apps")
	if err != nil {
		t.Fatal(err)
	}
	name, err := runtimeAdapter.ServiceName(rev.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, owned := range []bool{true, false} {
		labels := hostedRuntimeSecretLabels(rev)
		if !owned {
			labels["soha.io/app-id"] = "another-app"
		}
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "soha-apps", UID: "runtime-secret-uid", Labels: labels},
			Data:       map[string][]byte{"APP_TOKEN": []byte("synthetic")},
		}
		hosted := &hostedDynamic{kube: k8sfake.NewClientset(secret), runtime: runtimeAdapter, namespace: secret.Namespace}
		ctx := t.Context()
		deleteErr := hosted.DeleteRuntime(ctx, rev)
		_, readErr := hosted.kube.CoreV1().Secrets(secret.Namespace).Get(ctx, name, metav1.GetOptions{})
		if !owned {
			if deleteErr == nil || readErr != nil {
				t.Fatal("cleanup accepted or removed another app's Secret")
			}
			continue
		}
		if deleteErr != nil || !kubeerrors.IsNotFound(readErr) {
			t.Fatal("cleanup did not remove its revision's Secret")
		}
		if err := hosted.DeleteRuntime(ctx, rev); err != nil {
			t.Fatalf("retry after deletion: %v", err)
		}
	}
}

func TestHostedRollbackRuntimeSecretRequiresExactCredentials(t *testing.T) {
	rev := domain.Revision{
		ID:    "rev_0123456789abcdef0123456789abcdef",
		AppID: "app_fedcba9876543210fedcba9876543210",
	}
	runtimeAdapter, err := hostedruntime.New(dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme()), "soha-apps")
	if err != nil {
		t.Fatal(err)
	}
	name, err := runtimeAdapter.ServiceName(rev.ID)
	if err != nil {
		t.Fatal(err)
	}
	immutable := true
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "soha-apps", Labels: hostedRuntimeSecretLabels(rev)},
		Immutable:  &immutable,
		Data:       map[string][]byte{"DATABASE_URL": []byte("postgres://attacker.invalid/arbitrary")},
	}
	hosted := &hostedDynamic{
		kube:      k8sfake.NewClientset(secret),
		runtime:   runtimeAdapter,
		namespace: "soha-apps",
		dataKey:   []byte(strings.Repeat("k", 32)),
	}
	err = hosted.checkRuntimeSecret(context.Background(), rev)
	if err == nil {
		t.Fatal("an owned immutable secret with unexpected credentials was accepted")
	}
	if strings.Contains(err.Error(), "attacker.invalid") {
		t.Fatal("the rollback error exposed secret data")
	}
	if hostedRuntimeSecretMatches(secret, rev, map[string][]byte{}) {
		t.Fatal("secret matching accepted data that the revision does not inject")
	}

	expected := map[string][]byte{"APP_TOKEN": []byte("expected")}
	secret.Data = map[string][]byte{"APP_TOKEN": []byte("expected")}
	if !hostedRuntimeSecretMatches(secret, rev, expected) {
		t.Fatal("an exact immutable owned secret did not match")
	}
	for name, data := range map[string]map[string][]byte{
		"missing": {},
		"wrong":   {"APP_TOKEN": []byte("wrong")},
		"extra":   {"APP_TOKEN": []byte("expected"), "EXTRA": []byte("value")},
	} {
		t.Run(name, func(t *testing.T) {
			if hostedRuntimeSecretMatches(secret, rev, data) {
				t.Fatal("secret matching accepted a different environment")
			}
		})
	}
	mutable := false
	secret.Immutable = &mutable
	if hostedRuntimeSecretMatches(secret, rev, expected) {
		t.Fatal("secret matching accepted a mutable secret")
	}
	secret.Immutable = &immutable
	secret.Labels["soha.io/revision-id"] = "rev_other"
	if hostedRuntimeSecretMatches(secret, rev, expected) {
		t.Fatal("secret matching accepted another revision's owned secret")
	}
}

func TestHostedRollbackExpectedDatabaseDSNIsStable(t *testing.T) {
	rev := domain.Revision{
		ID:       "rev_0123456789abcdef0123456789abcdef",
		AppID:    "app_fedcba9876543210fedcba9876543210",
		Database: true,
	}
	provisioner, err := hostedappdata.New("postgres://admin:pool-admin@pool.invalid/maintenance", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	hosted := &hostedDynamic{data: provisioner, dataKey: []byte(strings.Repeat("k", 32))}
	data, err := hosted.expectedRuntimeEnvironment(rev)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 1 {
		t.Fatalf("expected one injected database credential, got %d", len(data))
	}
	parsed, err := url.Parse(string(data["DATABASE_URL"]))
	if err != nil {
		t.Fatal("expected database DSN is invalid")
	}
	if parsed.Path != "/soha_"+rev.AppID || !strings.HasPrefix(parsed.User.Username(), "soha_rev_") {
		t.Fatal("expected database DSN did not use the bound pool IDs")
	}
	password, set := parsed.User.Password()
	if !set || password != hosted.password(rev.AppID+"/"+rev.ID) {
		t.Fatal("expected database DSN did not use the stable revision password")
	}
	again, err := hosted.expectedRuntimeEnvironment(rev)
	if err != nil || string(again["DATABASE_URL"]) != string(data["DATABASE_URL"]) {
		t.Fatal("expected database DSN was not stable")
	}
}
