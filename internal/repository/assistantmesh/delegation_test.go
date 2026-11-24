package assistantmesh

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestDelegationContractValidation(t *testing.T) {
	now := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	item := domain.DelegationRecord{
		Delegation: domain.Delegation{
			ID: uuid.NewString(), CallerID: uuid.NewString(), OwnerID: uuid.NewString(),
			Capability: domain.ManagedOSVersion, Field: "productVersion", InputHash: strings.Repeat("a", 64),
			Status: domain.DelegationAwaitingApproval, CreatedAt: now, ExpiresAt: now.Add(4 * time.Hour),
		},
		RequestID: uuid.NewString(),
	}
	if err := validateDelegation(item, true); err != nil {
		t.Fatalf("valid delegation: %v", err)
	}

	for name, mutate := range map[string]func(*domain.DelegationRecord){
		"capability": func(v *domain.DelegationRecord) { v.Capability = "desktop.other.v1" },
		"field":      func(v *domain.DelegationRecord) { v.Field = "productVersion -productName" },
		"input hash": func(v *domain.DelegationRecord) { v.InputHash = "not-a-hash" },
		"self":       func(v *domain.DelegationRecord) { v.OwnerID = v.CallerID },
		"status":     func(v *domain.DelegationRecord) { v.Status = "pending" },
		"deadline":   func(v *domain.DelegationRecord) { v.ExpiresAt = v.CreatedAt },
	} {
		t.Run(name, func(t *testing.T) {
			changed := item
			mutate(&changed)
			if err := validateDelegation(changed, true); !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("validation error = %v", err)
			}
		})
	}

	changed := item
	changed.Capability = "desktop.other.v1"
	if err := sameFrozenDelegation(item, changed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("frozen mutation error = %v", err)
	}
}

func TestDelegationRequestReceipt(t *testing.T) {
	item := domain.DelegationRecord{
		Delegation: domain.Delegation{OwnerID: uuid.NewString(), InputHash: strings.Repeat("b", 64)},
	}
	if got, err := delegationReceipt(item, item); err != nil || got.InputHash != item.InputHash {
		t.Fatalf("receipt = %#v, %v", got, err)
	}
	changed := item
	changed.InputHash = strings.Repeat("c", 64)
	if _, err := delegationReceipt(item, changed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("changed receipt error = %v", err)
	}
}
