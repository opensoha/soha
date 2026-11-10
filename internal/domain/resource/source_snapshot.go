package resource

// ResourceOwnerReference records Kubernetes evidence without guessing the owner's scope.
type ResourceOwnerReference struct {
	APIVersion         string `json:"apiVersion"`
	Kind               string `json:"kind"`
	Name               string `json:"name"`
	UID                string `json:"uid"`
	Controller         *bool  `json:"controller,omitempty"`
	BlockOwnerDeletion *bool  `json:"blockOwnerDeletion,omitempty"`
}

// ResourceSourceSnapshot deliberately excludes configuration values and raw YAML.
type ResourceSourceSnapshot struct {
	Replicas          *SourceReplicaObservation `json:"-"`
	ExternallyManaged bool                      `json:"-"`
	Resource          ResourceRef               `json:"resource"`
	SpecDigest        string                    `json:"specDigest"`
	Generation        int64                     `json:"generation,omitempty"`
	ObservedAt        string                    `json:"observedAt"`
	CRDName           string                    `json:"crdName,omitempty"`
	Plural            string                    `json:"plural,omitempty"`
	Owners            []ResourceOwnerReference  `json:"owners,omitempty"`
}

type SourceReplicaObservation struct {
	Desired   int32
	Ready     int32
	Updated   int32
	Available int32
}
