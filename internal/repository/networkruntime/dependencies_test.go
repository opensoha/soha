package networkruntime_test

import (
	"testing"

	appnetworkaccess "github.com/opensoha/soha/internal/application/networkaccess"
	runtimerepo "github.com/opensoha/soha/internal/repository/networkruntime"
)

func TestRepositoryRequiresVPNGovernance(t *testing.T) {
	var typedNil *appnetworkaccess.VPNGovernancePolicy
	for _, tc := range []struct {
		name   string
		policy appnetworkaccess.VPNGovernanceEvaluator
	}{
		{"missing", nil},
		{"typed nil", typedNil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("missing policy accepted")
				}
			}()
			runtimerepo.New(nil, tc.policy)
		})
	}
	if runtimerepo.New(nil, appnetworkaccess.VPNGovernancePolicy{}) == nil {
		t.Fatal("explicit policy rejected")
	}
}
