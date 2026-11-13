package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
)

func TestRoleBindingSubjectFilterRequiresCompleteServiceAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerPlatformRBACRoutes(router.Group(""), nil)
	request := httptest.NewRequest(
		http.MethodGet,
		"/access-control/rolebindings?namespace=platform&subjectKind=ServiceAccount&subjectName=app",
		nil,
	)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestNormalizeSubjectAccessReviewInput(t *testing.T) {
	valid := domainresource.SubjectAccessReviewInput{
		Subject: domainresource.AccessReviewSubject{Kind: " ServiceAccount ", Namespace: " platform ", Name: " builder "},
		Checks:  []domainresource.AccessReviewCheck{{Verb: " get ", Resource: " pods ", Namespace: " platform "}},
	}
	normalized, err := normalizeSubjectAccessReviewInput(valid)
	if err != nil {
		t.Fatalf("normalizeSubjectAccessReviewInput() error = %v", err)
	}
	if normalized.Subject.Kind != "ServiceAccount" || normalized.Subject.Name != "builder" || normalized.Checks[0].Verb != "get" {
		t.Fatalf("normalized input = %#v", normalized)
	}

	invalid := []domainresource.SubjectAccessReviewInput{
		{},
		{Subject: domainresource.AccessReviewSubject{Kind: "User", Name: "alice"}},
		{Subject: domainresource.AccessReviewSubject{Kind: "ServiceAccount", Name: "builder"}, Checks: []domainresource.AccessReviewCheck{{Verb: "get", Resource: "pods"}}},
		{Subject: domainresource.AccessReviewSubject{Kind: "Unknown", Name: "alice"}, Checks: []domainresource.AccessReviewCheck{{Verb: "get", Resource: "pods"}}},
		{Subject: domainresource.AccessReviewSubject{Kind: "User", Name: "alice"}, Checks: make([]domainresource.AccessReviewCheck, 51)},
		{Subject: domainresource.AccessReviewSubject{Kind: "User", Name: "alice"}, Checks: []domainresource.AccessReviewCheck{{Verb: "", Resource: "pods"}}},
	}
	for index, input := range invalid {
		if _, err := normalizeSubjectAccessReviewInput(input); err == nil {
			t.Errorf("invalid input %d succeeded: %#v", index, input)
		}
	}
}
