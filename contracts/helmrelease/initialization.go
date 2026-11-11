package helmrelease

import "github.com/opensoha/soha-contracts/gen/go/sohaapi"

const InitializationCapability = "helm.initialize.v1"
const InitializationProviderPrefix = "helm_initialize_v1."

func IsInitialization(snapshot sohaapi.HelmDeliverySnapshot) bool {
	return snapshot.Migration != nil && snapshot.Migration.Initialization
}
