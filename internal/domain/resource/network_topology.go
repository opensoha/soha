package resource

import (
	cr "github.com/opensoha/soha-contracts/resource"
	shared "github.com/opensoha/soha-contracts/resource/runtime"
)

// BuildNetworkTopology applies the shared, provider-neutral snapshot rules.
func BuildNetworkTopology(clusterID, namespace string, snapshot cr.NetworkTopologySnapshot) cr.NetworkTopologyView {
	return shared.BuildNetworkTopology(clusterID, namespace, snapshot)
}
