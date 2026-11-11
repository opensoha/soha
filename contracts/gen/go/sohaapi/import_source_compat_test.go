package sohaapi

import "testing"

func TestDeliveryServiceKindPublishedNamesRemainUsable(t *testing.T) {
	values := []DeliveryDraftServiceServiceKind{ExternalService, HelmRelease, Job, KubernetesWorkload}
	for _, value := range values {
		if !value.Valid() {
			t.Errorf("published service kind %q is invalid", value)
		}
	}
}
