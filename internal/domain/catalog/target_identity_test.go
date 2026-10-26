package catalog

import "testing"

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
