package providerportal

import (
	"testing"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func TestCanAccessApplicationDenyTakesPrecedence(t *testing.T) {
	application := Application{Assignments: []ApplicationAssignment{
		{SubjectType: AssignmentSubjectRole, SubjectID: "admin", Effect: AssignmentEffectAllow},
		{SubjectType: AssignmentSubjectUser, SubjectID: "user-1", Effect: AssignmentEffectDeny},
	}}
	if CanAccessApplication(domainidentity.Principal{UserID: "user-1", Roles: []string{"admin"}}, application) {
		t.Fatal("matching deny must override matching allow")
	}
	if !CanAccessApplication(domainidentity.Principal{UserID: "user-2", Roles: []string{"admin"}}, application) {
		t.Fatal("non-denied admin should be allowed")
	}
}

func TestCanAccessApplicationEnforcesConditions(t *testing.T) {
	application := Application{Metadata: map[string]any{"accessPolicy": map[string]any{
		"requireMfa": true, "allowedCidrs": []any{"10.0.0.0/8"}, "startTimeUtc": "22:00", "endTimeUtc": "02:00",
	}}}
	valid := AccessPolicyContext{SourceIP: "10.1.2.3", MFAAuthenticated: true, Now: time.Date(2026, 7, 25, 23, 0, 0, 0, time.UTC)}
	if !CanAccessApplicationWithContext(domainidentity.Principal{UserID: "user-1"}, application, valid) {
		t.Fatal("valid MFA, network, and overnight window should allow")
	}
	tests := []struct {
		name   string
		access AccessPolicyContext
	}{
		{name: "missing MFA", access: AccessPolicyContext{SourceIP: valid.SourceIP, Now: valid.Now}},
		{name: "outside network", access: AccessPolicyContext{SourceIP: "192.168.1.1", MFAAuthenticated: true, Now: valid.Now}},
		{name: "outside time", access: AccessPolicyContext{SourceIP: valid.SourceIP, MFAAuthenticated: true, Now: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if CanAccessApplicationWithContext(domainidentity.Principal{UserID: "user-1"}, application, test.access) {
				t.Fatal("condition failure should deny")
			}
		})
	}
}
