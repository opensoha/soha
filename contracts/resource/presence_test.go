package resource

import "testing"

func TestPresenceRequiresVersionedOriginalGVRAndIdentity(t *testing.T) {
	base := ResourcePresence{Protocol: ResourcePresenceProtocol, APIVersion: "v1", Resource: "configmaps", Kind: "ConfigMap", Namespace: "team-a", Name: "app", CollectionResourceVersion: "opaque", State: "absent"}
	if err := base.ValidateFor("ConfigMap", "team-a", "app"); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*ResourcePresence){
		func(p *ResourcePresence) { p.Protocol = "" },
		func(p *ResourcePresence) { p.APIVersion = "apps/v1" },
		func(p *ResourcePresence) { p.Resource = "secrets" },
		func(p *ResourcePresence) { p.Kind = "Secret" },
		func(p *ResourcePresence) { p.Namespace = "other" },
		func(p *ResourcePresence) { p.Name = "other" },
		func(p *ResourcePresence) { p.CollectionResourceVersion = "" },
		func(p *ResourcePresence) { p.UID = "uid" },
		func(p *ResourcePresence) { p.State = "present" },
		func(p *ResourcePresence) { p.State = "" },
	} {
		changed := base
		mutate(&changed)
		if changed.ValidateFor("ConfigMap", "team-a", "app") == nil {
			t.Fatal("missing protocol, target or identity became a presence proof")
		}
	}
}
