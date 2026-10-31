package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	contractresource "github.com/opensoha/soha-contracts/resource"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type conditionalRouteSpy struct {
	GenericResourceService
	calls int
	input contractresource.ConditionalResourceYAMLInput
}

func (s *conditionalRouteSpy) GetResourceIdentity(context.Context, domainidentity.Principal, string, string, string, string) (contractresource.ResourceIdentity, error) {
	s.calls++
	return contractresource.ResourceIdentity{UID: "uid", ResourceVersion: "rv"}, nil
}
func (s *conditionalRouteSpy) PlanResourceYAMLConditional(_ context.Context, _ domainidentity.Principal, _, _, _, _ string, input contractresource.ConditionalResourceYAMLInput) (domainoperation.Plan, error) {
	s.calls++
	s.input = input
	return domainoperation.Plan{Ready: true}, nil
}
func (s *conditionalRouteSpy) ApplyResourceYAMLConditional(_ context.Context, _ domainidentity.Principal, _, namespace, kind, name string, input contractresource.ConditionalResourceYAMLInput) (domainresource.ResourceYAMLView, error) {
	s.calls++
	s.input = input
	return domainresource.ResourceYAMLView{Kind: kind, Name: name, Namespace: namespace}, nil
}
func (s *conditionalRouteSpy) DeleteResourceConditional(_ context.Context, _ domainidentity.Principal, _, namespace, kind, name string, input contractresource.ResourceMutationPreconditions) (contractresource.ResourceDeletionReceipt, error) {
	s.calls++
	s.input.Preconditions = input
	return contractresource.ResourceDeletionReceipt{Accepted: true, State: "deletion_requested", Kind: kind, Name: name, Namespace: namespace, Preconditions: input}, nil
}

func TestConditionalResourceRoutesRequireOneStrictPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, method, suffix, body string
		want                       int
	}{
		{"identity", http.MethodGet, "/identity", "", 200},
		{"edit", http.MethodPut, "/conditional/yaml", `{"content":"kind: ConfigMap","preconditions":{"expectedUid":"uid","expectedResourceVersion":"rv"}}`, 200},
		{"preflight", http.MethodPost, "/conditional/yaml/preflight", `{"content":"kind: ConfigMap","preconditions":{"expectedUid":"uid","expectedResourceVersion":"rv"}}`, 200},
		{"unknown field", http.MethodPut, "/conditional/yaml", `{"content":"kind: ConfigMap","force":true,"preconditions":{"expectedUid":"uid","expectedResourceVersion":"rv"}}`, 400},
		{"two payloads", http.MethodPut, "/conditional/yaml", `{"content":"kind: ConfigMap"} {}`, 400},
		{"deletion accepted", http.MethodDelete, "/conditional?expectedUid=uid&expectedResourceVersion=rv", "", 202},
	} {
		t.Run(test.name, func(t *testing.T) {
			spy := &conditionalRouteSpy{}
			router := gin.New()
			(&genericResourceHandler{service: spy}).registerConditionalResourceRoutes(router)
			request := httptest.NewRequest(test.method, "/clusters/cluster-a/resources/ConfigMap/app"+test.suffix, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if test.want == 400 && spy.calls != 0 {
				t.Fatal("invalid payload reached service")
			}
			if test.want == 202 && !strings.Contains(response.Body.String(), "deletion_requested") {
				t.Fatal("acceptance presented as completion")
			}
		})
	}
}
