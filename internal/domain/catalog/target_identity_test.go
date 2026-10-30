package catalog

import (
	"maps"
	"testing"
	"time"
)

func TestTargetServiceIdentityCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input ReleaseTargetInput
		want  string
		bad   bool
	}{
		{name: "typed", input: ReleaseTargetInput{ServiceID: " logical "}, want: "logical"},
		{name: "legacy", input: ReleaseTargetInput{Metadata: map[string]any{"serviceId": "logical"}}, want: "logical"},
		{name: "matching", input: ReleaseTargetInput{ServiceID: "logical", Metadata: map[string]any{"serviceId": "logical"}}, want: "logical"},
		{name: "conflict", input: ReleaseTargetInput{ServiceID: "logical", Metadata: map[string]any{"serviceId": "another"}}, bad: true},
		{name: "invalid_legacy_type", input: ReleaseTargetInput{ServiceID: "logical", Metadata: map[string]any{"serviceId": 12}}, bad: true},
		{name: "unmapped", input: ReleaseTargetInput{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := tc.input.WithCanonicalServiceID()
			if (err != nil) != tc.bad {
				t.Fatalf("error=%v", err)
			}
			if tc.bad {
				return
			}
			if actual.ResolvedServiceID() != tc.want {
				t.Fatalf("identity=%q", actual.ResolvedServiceID())
			}
			if tc.want != "" && actual.Metadata["serviceId"] != tc.want {
				t.Fatal("legacy projection diverged")
			}
			if actual.Metadata != nil {
				actual.Metadata["test"] = "changed"
			}
			if tc.input.Metadata["test"] != nil {
				t.Fatal("input metadata was mutated")
			}
		})
	}
}

func TestTargetInputPreservesActualPosition(t *testing.T) {
	original := ReleaseTarget{ID: "target", ServiceID: "logical", ClusterID: "cluster-b", Namespace: "orders", WorkloadKind: "Deployment", WorkloadName: "api", ExecutorKind: "observe_only", Metadata: map[string]any{"resourceUid": "uid"}}
	input := original.Input()
	if input.ClusterID != original.ClusterID || input.Namespace != original.Namespace || input.ServiceID != "logical" || input.ExecutorKind != "observe_only" || input.Metadata["resourceUid"] != "uid" {
		t.Fatalf("input=%+v", input)
	}
	input.Metadata["resourceUid"] = "replacement"
	if original.Metadata["resourceUid"] != "uid" {
		t.Fatal("copy mutated original")
	}
}

func TestTargetConfigurationRevisionPreservesIdentityWithoutObservationTime(t *testing.T) {
	original := ReleaseTarget{ID: "target", ServiceID: "service", ClusterID: "cluster", Namespace: "team", ConfigRef: "binding", ExecutorKind: "manifest_ssa", Enabled: true}
	revision, err := original.ConfigurationRevision()
	if err != nil || len(revision) != 64 {
		t.Fatalf("revision=%q error=%v", revision, err)
	}
	for _, tc := range []struct {
		name   string
		change func(*ReleaseTarget)
		same   bool
		bad    bool
	}{
		{name: "observation_time", change: func(x *ReleaseTarget) { x.UpdatedAt = time.Now(); x.CreatedAt = time.Now() }, same: true},
		{name: "legacy_service", change: func(x *ReleaseTarget) { x.ServiceID = ""; x.Metadata = map[string]any{"serviceId": "service"} }, same: true},
		{name: "namespace", change: func(x *ReleaseTarget) { x.Namespace = "other" }},
		{name: "binding", change: func(x *ReleaseTarget) { x.ConfigRef = "other" }},
		{name: "disabled", change: func(x *ReleaseTarget) { x.Enabled = false }},
		{name: "metadata", change: func(x *ReleaseTarget) { x.Metadata = map[string]any{"ownershipMode": "observe_only"} }},
		{name: "ambiguous_service", change: func(x *ReleaseTarget) { x.Metadata = map[string]any{"serviceId": "other"} }, bad: true},
		{name: "invalid_json", change: func(x *ReleaseTarget) { x.Metadata = map[string]any{"invalid": make(chan int)} }, bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next := original
			next.Metadata = maps.Clone(original.Metadata)
			tc.change(&next)
			actual, err := next.ConfigurationRevision()
			if tc.bad {
				if err == nil {
					t.Fatal("invalid configuration accepted")
				}
				return
			}
			if err != nil || (actual == revision) != tc.same {
				t.Fatalf("same=%v actual=%q error=%v", tc.same, actual, err)
			}
		})
	}
}
