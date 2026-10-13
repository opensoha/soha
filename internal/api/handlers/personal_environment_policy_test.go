package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	appaiproduction "github.com/opensoha/soha/internal/application/aiproduction"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type personalPolicyStore struct {
	appaiproduction.Store
	saved []appaiproduction.EnvironmentTemplate
}

func (s *personalPolicyStore) PutEnvironmentTemplate(_ context.Context, item appaiproduction.EnvironmentTemplate) error {
	s.saved = append(s.saved, item)
	return nil
}

func TestPersonalTemplateHandlerValidatesWireAndScopedAssignment(t *testing.T) {
	valid := `{"id":"personal","name":"Personal","backend":"container","isolationMode":"disposable-write","personalDocker":{"agentClusterId":"00000000-0000-4000-8000-000000000048","leaseSeconds":600,"quota":{"diskBytes":33554432,"memoryBytes":67108864,"cpuUnits":250,"maxProcesses":16,"networkEgress":false}}}`
	vm := `{"id":"personal-vm","name":"Personal VM","backend":"vm","isolationMode":"disposable-write","personalVM":{"ownerId":"00000000-0000-4000-8000-000000000049","computeRef":"00000000-0000-4000-8000-000000005049","provider":"pve","dataDiskSerial":"testdata","dataDiskBytes":8589934592,"leaseSeconds":600,"quota":{"diskBytes":1073741824,"memoryBytes":67108864,"cpuUnits":250,"maxProcesses":16,"networkEgress":false}}}`
	for _, test := range []struct {
		name, body string
		denied     bool
		status     int
		checked    bool
	}{
		{"configured", valid, false, http.StatusCreated, true},
		{"node denied", valid, true, http.StatusForbidden, true},
		{"network", strings.Replace(valid, `"networkEgress":false`, `"networkEgress":true`, 1), false, http.StatusBadRequest, false},
		{"unknown host", strings.Replace(valid, `"leaseSeconds":600`, `"leaseSeconds":600,"endpoint":"http://caller.example"`, 1), false, http.StatusBadRequest, false},
		{"trailing", valid + `{}`, false, http.StatusBadRequest, false},
		{"VM configured", vm, false, http.StatusCreated, true},
		{"VM scope denied", vm, true, http.StatusForbidden, true},
		{"VM unknown host", strings.Replace(vm, `"provider":"pve"`, `"provider":"pve","hostURL":"https://caller.invalid"`, 1), false, http.StatusBadRequest, false},
		{"VM wrong backend", strings.Replace(vm, `"backend":"vm"`, `"backend":"container"`, 1), false, http.StatusBadRequest, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &personalPolicyStore{}
			checked := false
			service, err := appaiproduction.New(store, func(_ context.Context, actor domainidentity.Principal, id string) error {
				checked = true
				if actor.UserID != "operator" || id != "00000000-0000-4000-8000-000000000048" {
					t.Fatal("assignment lost actor/node")
				}
				if test.denied {
					return apperrors.ErrAccessDenied
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			service.SetPersonalVMValidator(func(_ context.Context, actor domainidentity.Principal, policy domaincopilot.PersonalVMPolicy) error {
				checked = true
				if actor.UserID != "operator" || policy.OwnerID != "00000000-0000-4000-8000-000000000049" || policy.ComputeRef != "00000000-0000-4000-8000-000000005049" {
					t.Fatal("VM assignment lost actor, owner or compute reference")
				}
				if test.denied {
					return apperrors.ErrAccessDenied
				}
				return nil
			})
			h := NewAIProductionHandler(service, evaluationAuthorizerStub{})
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/ai/environments/templates", strings.NewReader(test.body))
			c.Set("principal", domainidentity.Principal{UserID: "operator"})
			h.putEnvironmentTemplate(c)
			if response.Code != test.status || checked != test.checked || (len(store.saved) == 1) != (test.status == http.StatusCreated) {
				t.Fatalf("status=%d checked=%t saved=%d", response.Code, checked, len(store.saved))
			}
		})
	}
}
