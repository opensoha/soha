package cluster

import (
	"context"
	"errors"
	"reflect"
	"testing"

	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type qualityDeleteRepository struct {
	contractRepository
	items map[string]domaincluster.Summary
	fail  bool
}

func (r *qualityDeleteRepository) Get(_ context.Context, id string) (domaincluster.Summary, error) {
	if item, ok := r.items[id]; ok {
		return item, nil
	}
	return domaincluster.Summary{}, apperrors.ErrNotFound
}

func (r *qualityDeleteRepository) Delete(_ context.Context, id string) error {
	if r.fail {
		return errClusterRepository
	}
	delete(r.items, id)
	return nil
}

type qualityFailedAudit struct{}

func (qualityFailedAudit) Record(context.Context, domainaudit.Entry) error {
	return errors.New("synthetic audit unavailable")
}

func TestDeleteFailureAndRetryPreserveOtherClusters(t *testing.T) {
	for _, auditFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "persistence failure", true: "audit failure after persistence"}[auditFailure], func(t *testing.T) {
			other := domaincluster.Summary{ID: "other", Name: "unchanged"}
			repo := &qualityDeleteRepository{items: map[string]domaincluster.Summary{"owned": {ID: "owned"}, "other": other}, fail: !auditFailure}
			registry, cache := &contractRegistry{}, &contractCache{}
			var audit AuditRecorder
			if auditFailure {
				audit = qualityFailedAudit{}
			}
			service, err := New(registry, &contractRuntime{}, cache, nil, repo, nil, audit, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := service.Delete(t.Context(), domainidentity.Principal{}, "owned"); err == nil {
				t.Fatal("controlled failure was swallowed")
			}
			_, retained := repo.items["owned"]
			if retained == auditFailure || !reflect.DeepEqual(repo.items["other"], other) || (!auditFailure && len(registry.unregistered) != 0) {
				t.Fatalf("wrong partial state: items=%+v runtime=%v", repo.items, registry.unregistered)
			}
			repo.fail = false
			err = service.Delete(t.Context(), domainidentity.Principal{}, "owned")
			if auditFailure {
				if !errors.Is(err, apperrors.ErrNotFound) {
					t.Fatalf("already-deleted retry must report actual state: %v", err)
				}
			} else if err != nil {
				t.Fatalf("valid retry failed: %v", err)
			}
			if _, exists := repo.items["owned"]; exists || !reflect.DeepEqual(repo.items["other"], other) || !reflect.DeepEqual(registry.unregistered, []string{"owned"}) || !reflect.DeepEqual(cache.unregistered, []string{"owned"}) {
				t.Fatalf("retry changed unrelated state or repeated side effect: items=%+v runtime=%v cache=%v", repo.items, registry.unregistered, cache.unregistered)
			}
		})
	}
}
