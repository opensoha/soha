package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type referenceCandidateStoreStub struct {
	domaindelivery.Repository
	item                   domaindelivery.ServiceReferenceCandidate
	writes, deletes, reads int
	beforeWrite            func()
	changeOnSecondRead     bool
}

func (r *referenceCandidateStoreStub) ListServiceReferenceCandidates(_ context.Context, consumerID, applicationID, cursor string, limit int) (domaindelivery.ServiceReferenceCandidatePage, error) {
	page := domaindelivery.ServiceReferenceCandidatePage{Items: make([]domaindelivery.ServiceReferenceCandidateSummary, 0)}
	if r.item.ID != "" && r.item.Preview.Consumer.ApplicationEnvironmentID == consumerID && r.item.Preview.ApplicationID == applicationID && r.item.Preview.ServiceID > cursor && limit > 0 {
		page.Items = append(page.Items, domaindelivery.ServiceReferenceCandidateSummary{ID: r.item.ID, ServiceID: r.item.Preview.ServiceID, Revision: r.item.Revision, State: "candidate", CreatedAt: r.item.CreatedAt, UpdatedAt: r.item.UpdatedAt})
	}
	return page, nil
}

func (r *referenceCandidateStoreStub) GetServiceReferenceCandidate(context.Context, string, string) (domaindelivery.ServiceReferenceCandidate, error) {
	r.reads++
	if r.item.ID == "" {
		return r.item, apperrors.ErrNotFound
	}
	item := r.item
	if r.changeOnSecondRead && r.reads == 2 {
		item.Revision++
	}
	return item, nil
}
func (r *referenceCandidateStoreStub) SaveServiceReferenceCandidate(ctx context.Context, input domaindelivery.ServiceReferenceCandidateWrite, revalidate func(context.Context) (domaindelivery.ServiceReferenceConfiguration, error)) (domaindelivery.ServiceReferenceCandidate, error) {
	if r.beforeWrite != nil {
		r.beforeWrite()
	}
	configuration, err := revalidate(ctx)
	if err != nil {
		return domaindelivery.ServiceReferenceCandidate{}, err
	}
	revision, err := configuration.Revision()
	if err != nil {
		return domaindelivery.ServiceReferenceCandidate{}, err
	}
	if revision != input.Candidate.ContextRevision {
		return domaindelivery.ServiceReferenceCandidate{}, apperrors.ErrConflict
	}
	if r.item.ID != "" && (input.ExpectedCandidateID != r.item.ID || input.ExpectedRevision != r.item.Revision) {
		return domaindelivery.ServiceReferenceCandidate{}, apperrors.ErrConflict
	}
	r.writes++
	input.Candidate.ID, input.Candidate.Revision = "candidate", r.item.Revision+1
	input.Candidate.State, input.Candidate.Status = "candidate", "current"
	input.Candidate.CurrentSourceRevision = input.Candidate.Preview.SourceRevision
	r.item = input.Candidate
	return r.item, nil
}
func (r *referenceCandidateStoreStub) DeleteServiceReferenceCandidate(ctx context.Context, _, _ string, input domaindelivery.ServiceReferenceCandidateDeleteInput, authorize func(context.Context) error) error {
	if r.beforeWrite != nil {
		r.beforeWrite()
	}
	if err := authorize(ctx); err != nil {
		return err
	}
	if input.ExpectedCandidateID != r.item.ID || input.ExpectedRevision != r.item.Revision {
		return apperrors.ErrConflict
	}
	r.deletes++
	r.item = domaindelivery.ServiceReferenceCandidate{}
	return nil
}

func referenceCandidateFixture(t *testing.T) (referenceFixture, *referenceCandidateStoreStub, domaindelivery.ServiceReferenceCandidateInput) {
	t.Helper()
	f := newReferenceFixture(t)
	preview, err := f.preview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r := &referenceCandidateStoreStub{}
	f.service.repository = r
	zero := int64(0)
	return f, r, domaindelivery.ServiceReferenceCandidateInput{SourceApplicationEnvironmentID: f.input.SourceApplicationEnvironmentID,
		ExpectedConsumerUpdatedAt: f.input.ExpectedConsumerUpdatedAt, ExpectedSourceUpdatedAt: f.input.ExpectedSourceUpdatedAt,
		SourceTargetIDs: []string{"target-b"}, ExpectedSourceRevision: preview.SourceRevision, ExpectedRevision: &zero}
}

func putCandidate(f referenceFixture, input domaindelivery.ServiceReferenceCandidateInput) (domaindelivery.ServiceReferenceCandidate, error) {
	return f.service.PutEnvironmentServiceReferenceCandidate(context.Background(), deliveryActionPrincipal(), "test-a", "svc", input)
}

func TestServiceReferenceCandidateSavesInactiveProposalWithoutChangingTargets(t *testing.T) {
	f, store, input := referenceCandidateFixture(t)
	before, err := json.Marshal(f.catalog.bindings)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := putCandidate(f, input)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(f.catalog.bindings)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if saved.State != "candidate" || saved.Revision != 1 || store.writes != 1 || len(saved.Preview.CurrentLocalTargetIDs) != 1 || string(before) != string(after) || strings.Contains(string(raw), "synthetic-sensitive") || strings.Contains(string(raw), saved.ContextRevision) {
		t.Fatalf("candidate state/scope or disclosure invariant failed: %s", raw)
	}
	current, err := f.service.GetEnvironmentServiceReferenceCandidate(context.Background(), deliveryActionPrincipal(), "test-a", "svc")
	if err != nil || current.Status != "current" || current.ID != saved.ID {
		t.Fatalf("current=%+v err=%v", current, err)
	}
	input.ExpectedCandidateID, input.ExpectedRevision = saved.ID, &saved.Revision
	updated, err := putCandidate(f, input)
	if err != nil || updated.ID != saved.ID || updated.Revision != 2 {
		t.Fatalf("update=%+v err=%v", updated, err)
	}
	if _, err := putCandidate(f, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale update=%v", err)
	}
}

func TestServiceReferenceCandidateReadRechecksConfigurationIdentityAndPermission(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		mutate func(*testing.T, referenceFixture, *referenceCandidateStoreStub)
		status string
		err    error
	}{
		{name: "configuration stale", status: "outdated", mutate: func(t *testing.T, f referenceFixture, _ *referenceCandidateStoreStub) {
			key := "cluster-b/backend/Deployment/api/"
			source := f.targets.sources[key]
			source.SpecDigest = strings.Repeat("b", 64)
			f.targets.sources[key] = source
		}},
		{name: "uid replacement", err: apperrors.ErrConflict, mutate: func(t *testing.T, f referenceFixture, _ *referenceCandidateStoreStub) {
			key := "cluster-b/backend/Deployment/api/"
			source := f.targets.sources[key]
			source.Resource.UID = "replacement"
			f.targets.sources[key] = source
		}},
		{name: "source denied", err: apperrors.ErrAccessDenied, mutate: func(t *testing.T, f referenceFixture, _ *referenceCandidateStoreStub) {
			f.catalog.denied["test-b"] = apperrors.ErrAccessDenied
		}},
		{name: "resource denied", err: apperrors.ErrAccessDenied, mutate: func(t *testing.T, f referenceFixture, _ *referenceCandidateStoreStub) {
			f.targets.readErr = apperrors.ErrAccessDenied
		}},
		{name: "source deleted", err: apperrors.ErrNotFound, mutate: func(t *testing.T, f referenceFixture, _ *referenceCandidateStoreStub) {
			delete(f.catalog.bindings, "test-b")
		}},
		{name: "candidate concurrent edit", err: apperrors.ErrConflict, mutate: func(_ *testing.T, _ referenceFixture, r *referenceCandidateStoreStub) {
			r.reads = 0
			r.changeOnSecondRead = true
		}},
		{name: "source reassociation never leaks cached old namespace", err: apperrors.ErrConflict, mutate: func(t *testing.T, f referenceFixture, _ *referenceCandidateStoreStub) {
			b := f.catalog.bindings["test-b"]
			target := b.Targets[0]
			target.Namespace = "new-namespace"
			source := f.targets.sources["cluster-b/backend/Deployment/api/"]
			source.Resource.Namespace, source.Resource.UID = target.Namespace, "replacement"
			raw, err := json.Marshal(source)
			if err != nil {
				t.Fatal(err)
			}
			target.Metadata["resourceSource"] = string(raw)
			b.Targets[0] = target
			f.catalog.bindings[b.ID] = b
			f.targets.sources["cluster-b/new-namespace/Deployment/api/"] = source
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f, r, input := referenceCandidateFixture(t)
			if _, err := putCandidate(f, input); err != nil {
				t.Fatal(err)
			}
			scenario.mutate(t, f, r)
			item, err := f.service.GetEnvironmentServiceReferenceCandidate(context.Background(), deliveryActionPrincipal(), "test-a", "svc")
			if !errors.Is(err, scenario.err) || (err == nil && item.Status != scenario.status) || (err != nil && item.ID != "") {
				t.Fatalf("status=%s id=%s err=%v", item.Status, item.ID, err)
			}
		})
	}
}

func TestServiceReferenceCandidateReauthorizesAtWriteAndCleansWithoutSource(t *testing.T) {
	f, r, input := referenceCandidateFixture(t)
	r.beforeWrite = func() { f.catalog.updateErr = apperrors.ErrAccessDenied }
	if _, err := putCandidate(f, input); !errors.Is(err, apperrors.ErrAccessDenied) || r.writes != 0 {
		t.Fatalf("write=%v writes=%d", err, r.writes)
	}
	r.beforeWrite, f.catalog.updateErr = nil, nil
	saved, err := putCandidate(f, input)
	if err != nil {
		t.Fatal(err)
	}
	f.catalog.denied["test-b"] = apperrors.ErrAccessDenied
	f.targets.readErr = apperrors.ErrAccessDenied
	apps := referenceApplicationsFixture(t, &f)
	apps.services[0].Enabled = false
	f.service.applications = apps
	remove := domaindelivery.ServiceReferenceCandidateDeleteInput{ExpectedCandidateID: saved.ID, ExpectedRevision: saved.Revision, ExpectedConsumerUpdatedAt: input.ExpectedConsumerUpdatedAt}
	remove.ExpectedCandidateID = "replaced"
	if err := f.service.DeleteEnvironmentServiceReferenceCandidate(context.Background(), deliveryActionPrincipal(), "test-a", "svc", remove); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("wrong identity=%v", err)
	}
	remove.ExpectedCandidateID = saved.ID
	if err := f.service.DeleteEnvironmentServiceReferenceCandidate(context.Background(), deliveryActionPrincipal(), "test-a", "svc", remove); err != nil || r.deletes != 1 {
		t.Fatalf("cleanup=%v deletes=%d", err, r.deletes)
	}
}

func TestServiceReferenceCandidateRejectsUnconfirmedOrActivatingInputs(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		mutate func(*domaindelivery.ServiceReferenceCandidateInput)
	}{
		{name: "missing candidate CAS", mutate: func(i *domaindelivery.ServiceReferenceCandidateInput) { i.ExpectedRevision = nil }},
		{name: "negative revision", mutate: func(i *domaindelivery.ServiceReferenceCandidateInput) { n := int64(-1); i.ExpectedRevision = &n }},
		{name: "update missing id", mutate: func(i *domaindelivery.ServiceReferenceCandidateInput) { n := int64(1); i.ExpectedRevision = &n }},
		{name: "create old id", mutate: func(i *domaindelivery.ServiceReferenceCandidateInput) { i.ExpectedCandidateID = "old" }},
		{name: "missing source preview", mutate: func(i *domaindelivery.ServiceReferenceCandidateInput) { i.ExpectedSourceRevision = "" }},
		{name: "missing complete selection", mutate: func(i *domaindelivery.ServiceReferenceCandidateInput) { i.SourceTargetIDs = nil }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f, r, input := referenceCandidateFixture(t)
			scenario.mutate(&input)
			if _, err := putCandidate(f, input); !errors.Is(err, apperrors.ErrInvalidArgument) || r.writes != 0 {
				t.Fatalf("err=%v writes=%d", err, r.writes)
			}
		})
	}
}

func TestServiceReferenceCandidateRejectsTypedNilPersistence(t *testing.T) {
	f, _, input := referenceCandidateFixture(t)
	var repository *referenceCandidateStoreStub
	f.service.repository = repository
	if _, err := putCandidate(f, input); !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("typed nil=%v", err)
	}
}

func TestServiceReferenceCandidateMetadataSupportsColdCleanupWithoutSourceDisclosure(t *testing.T) {
	f, r, input := referenceCandidateFixture(t)
	if _, err := putCandidate(f, input); err != nil {
		t.Fatal(err)
	}
	f.catalog.denied["test-b"] = apperrors.ErrAccessDenied
	page, err := f.service.ListEnvironmentServiceReferenceCandidates(context.Background(), deliveryActionPrincipal(), "test-a", "", 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].Revision != 1 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"source", "test-b", "backend", "cluster-b", "preview", "SourceRevision"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("source information in metadata page: %s", raw)
		}
	}
	if _, err := f.service.GetEnvironmentServiceReferenceCandidate(context.Background(), deliveryActionPrincipal(), "test-a", "svc"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("full candidate source access=%v", err)
	}
	remove := domaindelivery.ServiceReferenceCandidateDeleteInput{ExpectedCandidateID: page.Items[0].ID, ExpectedRevision: page.Items[0].Revision, ExpectedConsumerUpdatedAt: input.ExpectedConsumerUpdatedAt}
	if err := f.service.DeleteEnvironmentServiceReferenceCandidate(context.Background(), deliveryActionPrincipal(), "test-a", "svc", remove); err != nil || r.deletes != 1 {
		t.Fatalf("metadata cleanup=%v", err)
	}
	f.catalog.updateErr = apperrors.ErrAccessDenied
	if page, err := f.service.ListEnvironmentServiceReferenceCandidates(context.Background(), deliveryActionPrincipal(), "test-a", "", 1); !errors.Is(err, apperrors.ErrAccessDenied) || len(page.Items) != 0 {
		t.Fatalf("denied page=%+v err=%v", page, err)
	}
	for _, limit := range []int{0, 101} {
		if _, err := f.service.ListEnvironmentServiceReferenceCandidates(context.Background(), deliveryActionPrincipal(), "test-a", "", limit); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("invalid limit=%v", err)
		}
	}
}
