package resourcebackend

import (
	"errors"
	"strings"
	"testing"

	"github.com/opensoha/soha/internal/platform/apperrors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestKubernetesCreateErrorUsesStructuredCauseWithoutRejectedValue(t *testing.T) {
	const privateValue = "private-service-token"
	err := kubernetesCreateError("Service", k8serrors.NewInvalid(
		schema.GroupKind{Kind: "Service"},
		"checkout",
		field.ErrorList{field.Invalid(field.NewPath("spec", "ports").Index(0).Child("port"), privateValue, "must be between 1 and 65535")},
	))

	var business *apperrors.BusinessError
	if !errors.As(err, &business) {
		t.Fatalf("kubernetesCreateError() = %v, want BusinessError", err)
	}
	want := "Kubernetes rejected Service/checkout: field spec.ports[0].port has an invalid value"
	if got := business.Message(""); got != want {
		t.Fatalf("Message() = %q, want %q", got, want)
	}
	if strings.Contains(business.Message(""), privateValue) {
		t.Fatal("public resource error exposed the rejected value")
	}
}
