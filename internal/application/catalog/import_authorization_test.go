package catalog

import (
	"context"
	"testing"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func TestImportBindingChecksCreateOrExistingUpdateScope(t *testing.T) {
	for _, existing := range []bool{true, false} {
		authorizer := &recordingCatalogAuthorizer{}
		repo := &stubCatalogRepository{applicationEnvironments: map[string]domaincatalog.ApplicationEnvironment{"binding": {ID: "binding", ApplicationID: "app", EnvironmentKey: "prod", BusinessLineID: "finance", ApplicationGroup: "payments"}}}
		service := New(repo, authorizer, nil, catalogPermissions("delivery.application-environments.create", "delivery.application-environments.update"), nil, nil)
		bindingID, key, permission := "", "test", "delivery.application-environments.create"
		if existing {
			bindingID = "binding"
			permission = "delivery.application-environments.update"
			key = "prod"
		}
		if err := service.AuthorizeImportBinding(context.Background(), domainidentity.Principal{Roles: []string{"admin"}}, "app", "test", bindingID); err != nil {
			t.Fatal(err)
		}
		request := authorizer.request
		if request.PermissionKey != permission || request.Delivery.ApplicationID != "app" || request.Delivery.EnvironmentKey != key || request.Delivery.BusinessLineID != "finance" {
			t.Fatalf("scope=%+v", request)
		}
	}
}
