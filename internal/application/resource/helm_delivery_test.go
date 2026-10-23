package resource

import (
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func TestHelmDirectInitializationUsesDedicatedProvider(t *testing.T) {
	connection := agentConnection("")
	connection.Summary.ConnectionMode = domaincluster.ConnectionModeDirectKubeconfig
	service := New(Dependencies{Connections: stubConnectionResolver{connection: connection}, Authorizer: allowAllResourceAuthorizer{}, Permissions: allowRuntimePermission{}, Audit: noopResourceAuditRecorder{}})
	snapshot := sohaapi.HelmDeliverySnapshot{ClusterID: "cluster", Namespace: "team", Operation: sohaapi.HelmDeliverySnapshotOperationInstall}
	principal := domainidentity.Principal{UserID: "user"}
	provider, err := service.Helm().AuthorizeHelmDelivery(t.Context(), principal, snapshot, true)
	if err != nil || provider != "helm_direct" {
		t.Fatalf("ordinary install provider=%s error=%v", provider, err)
	}
	snapshot.Migration = &sohaapi.TemplateMigrationAssessment{Initialization: true}
	provider, err = service.Helm().AuthorizeHelmDelivery(t.Context(), principal, snapshot, true)
	if err != nil || provider != domaindelivery.HelmDirectInitializationProvider {
		t.Fatalf("initialization provider=%s error=%v", provider, err)
	}
}
