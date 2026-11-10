package delivery

import (
	"context"
	"errors"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"strings"
	"testing"
	"time"
)

type activeReferenceStoreStub struct {
	*referenceCandidateStoreStub
	active       dd.ServiceReferenceRecord
	beforeSave   func()
	activeReads  int
	changeOnRead bool
}

func (r *activeReferenceStoreStub) GetServiceReference(_ context.Context, consumer, service string) (dd.ServiceReferenceRecord, error) {
	if r.active.ConsumerBindingID != consumer || r.active.ServiceID != service || r.active.Reference.ID == "" {
		return dd.ServiceReferenceRecord{}, apperrors.ErrNotFound
	}
	r.activeReads++
	item := r.active
	if r.changeOnRead && r.activeReads > 1 {
		item.Reference.Revision++
	}
	return item, nil
}
func (r *activeReferenceStoreStub) SaveServiceReference(ctx context.Context, w dd.ServiceReferenceWrite, check func(context.Context) (dd.ServiceReferenceConfiguration, error)) (dd.ServiceReferenceRecord, error) {
	if r.beforeSave != nil {
		r.beforeSave()
	}
	c, err := check(ctx)
	if err != nil {
		return dd.ServiceReferenceRecord{}, err
	}
	rev, err := c.Revision()
	if err != nil {
		return dd.ServiceReferenceRecord{}, err
	}
	if rev != w.ConfigurationRevision || r.item.ID != w.ExpectedCandidateID || r.item.Revision != w.ExpectedCandidateRevision {
		return dd.ServiceReferenceRecord{}, apperrors.ErrConflict
	}
	if r.active.Reference.ID != "" && (w.ExpectedReferenceID != r.active.Reference.ID || w.ExpectedRevision != r.active.Reference.Revision) {
		return dd.ServiceReferenceRecord{}, apperrors.ErrConflict
	}
	w.Record.Reference.ID = "reference"
	w.Record.Reference.Revision = r.active.Reference.Revision + 1
	w.Record.Reference.State = "active"
	r.active = w.Record
	return r.active, nil
}
func (r *activeReferenceStoreStub) DeleteServiceReference(ctx context.Context, _, _ string, i dd.ServiceReferenceDeleteInput, check func(context.Context) error) error {
	if err := check(ctx); err != nil {
		return err
	}
	if r.active.Reference.ID != i.ExpectedReferenceID || r.active.Reference.Revision != i.ExpectedRevision {
		return apperrors.ErrConflict
	}
	r.active = dd.ServiceReferenceRecord{}
	return nil
}
func (r *activeReferenceStoreStub) ListServiceReferences(context.Context, string, string, string, int) (dd.ServiceReferencePage, error) {
	return dd.ServiceReferencePage{Items: []dd.ServiceReferenceSummary{{ID: r.active.Reference.ID, ServiceID: r.active.ServiceID, State: "active", Revision: r.active.Reference.Revision}}}, nil
}

func activeReferenceFixture(t *testing.T) (referenceFixture, *activeReferenceStoreStub, dd.ServiceReferenceInput) {
	t.Helper()
	f := newReferenceFixture(t)
	consumer := f.catalog.bindings["test-a"]
	consumer.Targets = nil
	f.catalog.bindings["test-a"] = consumer
	store := &activeReferenceStoreStub{referenceCandidateStoreStub: &referenceCandidateStoreStub{}}
	f.service.repository = store
	preview, err := f.preview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	candidate, err := putCandidate(f, dd.ServiceReferenceCandidateInput{SourceApplicationEnvironmentID: "test-b", ExpectedConsumerUpdatedAt: f.input.ExpectedConsumerUpdatedAt, ExpectedSourceUpdatedAt: f.input.ExpectedSourceUpdatedAt, SourceTargetIDs: []string{"target-b"}, ExpectedSourceRevision: preview.SourceRevision, ExpectedRevision: &zero})
	if err != nil {
		t.Fatal(err)
	}
	return f, store, dd.ServiceReferenceInput{ExpectedCandidateID: candidate.ID, ExpectedCandidateRevision: candidate.Revision, ExpectedConsumerUpdatedAt: f.input.ExpectedConsumerUpdatedAt, ExpectedSourceUpdatedAt: f.input.ExpectedSourceUpdatedAt, ExpectedSourceRevision: preview.SourceRevision, ExpectedRevision: &zero}
}

func TestActiveServiceReferenceActivationAndRead(t *testing.T) {
	f, store, input := activeReferenceFixture(t)
	item, err := f.service.PutEnvironmentServiceReference(context.Background(), deliveryActionPrincipal(), "test-a", "svc", input)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != "active" || len(f.catalog.bindings["test-a"].Targets) != 0 || len(f.catalog.bindings["test-b"].Targets) != 1 {
		t.Fatal("activation mutated owned targets")
	}
	f.catalog.updateErr = apperrors.ErrAccessDenied
	current, err := f.service.GetEnvironmentServiceReference(context.Background(), deliveryActionPrincipal(), "test-a", "svc")
	if err != nil {
		t.Fatalf("view-only read: %v", err)
	}
	resolved, err := f.service.ResolveEnvironmentServiceReferenceTarget(context.Background(), deliveryActionPrincipal(), "test-a", "svc", "target-b", current.ID, current.Revision)
	if err != nil || resolved.View.ApplicationEnvironmentID != "test-a" || resolved.Origin.ApplicationEnvironmentID != "test-b" || resolved.Target.OwnershipMode != "observe_only" {
		t.Fatalf("resolve=%+v err=%v", resolved, err)
	}
	if store.active.Reference.Preview.Targets[0].OwnershipMode != "observe_only" {
		t.Fatal("control upgraded")
	}
}

func TestActiveServiceReferenceReadFreshIdentityControlAndConfiguration(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(referenceFixture, *activeReferenceStoreStub)
		want   error
	}{
		{"source permission", func(f referenceFixture, _ *activeReferenceStoreStub) {
			f.catalog.denied["test-b"] = apperrors.ErrAccessDenied
		}, apperrors.ErrAccessDenied},
		{"source missing", func(f referenceFixture, _ *activeReferenceStoreStub) { delete(f.catalog.bindings, "test-b") }, apperrors.ErrNotFound},
		{"live permission", func(f referenceFixture, _ *activeReferenceStoreStub) { f.targets.readErr = apperrors.ErrAccessDenied }, apperrors.ErrAccessDenied},
		{"live UID", func(f referenceFixture, _ *activeReferenceStoreStub) {
			for key, s := range f.targets.sources {
				s.Resource.UID = "replacement"
				f.targets.sources[key] = s
			}
		}, apperrors.ErrConflict},
		{"executor", func(f referenceFixture, _ *activeReferenceStoreStub) {
			b := f.catalog.bindings["test-b"]
			b.Targets[0].ExecutorKind = "changed"
			f.catalog.bindings["test-b"] = b
		}, apperrors.ErrConflict},
		{"configRef", func(f referenceFixture, _ *activeReferenceStoreStub) {
			b := f.catalog.bindings["test-b"]
			b.Targets[0].ConfigRef = "changed"
			f.catalog.bindings["test-b"] = b
		}, apperrors.ErrConflict},
		{"concurrent reference", func(_ referenceFixture, r *activeReferenceStoreStub) { r.activeReads = 0; r.changeOnRead = true }, apperrors.ErrConflict},
		{"ordinary config", func(f referenceFixture, _ *activeReferenceStoreStub) {
			b := f.catalog.bindings["test-b"]
			b.UpdatedAt = b.UpdatedAt.Add(time.Second)
			b.Targets[0].UpdatedAt = b.Targets[0].UpdatedAt.Add(time.Second)
			f.catalog.bindings["test-b"] = b
			for key, s := range f.targets.sources {
				s.SpecDigest = strings.Repeat("b", 64)
				f.targets.sources[key] = s
			}
		}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, r, input := activeReferenceFixture(t)
			saved, err := f.service.PutEnvironmentServiceReference(context.Background(), deliveryActionPrincipal(), "test-a", "svc", input)
			if err != nil {
				t.Fatal(err)
			}
			c.mutate(f, r)
			got, err := f.service.GetEnvironmentServiceReference(context.Background(), deliveryActionPrincipal(), "test-a", "svc")
			if !errors.Is(err, c.want) {
				t.Fatalf("err=%v want=%v", err, c.want)
			}
			if c.want != nil && got.ID != "" {
				t.Fatal("cached source leaked")
			}
			if c.want == nil && got.CurrentSourceRevision == saved.CurrentSourceRevision {
				t.Fatal("ordinary config did not refresh")
			}
		})
	}
}

func TestActiveServiceReferenceCleanupWithoutSourceAccess(t *testing.T) {
	f, _, input := activeReferenceFixture(t)
	saved, err := f.service.PutEnvironmentServiceReference(context.Background(), deliveryActionPrincipal(), "test-a", "svc", input)
	if err != nil {
		t.Fatal(err)
	}
	f.catalog.denied["test-b"] = apperrors.ErrAccessDenied
	page, err := f.service.ListEnvironmentServiceReferences(context.Background(), deliveryActionPrincipal(), "test-a", "", 10)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	remove := dd.ServiceReferenceDeleteInput{ExpectedReferenceID: saved.ID, ExpectedRevision: saved.Revision, ExpectedConsumerUpdatedAt: input.ExpectedConsumerUpdatedAt}
	remove.ExpectedRevision++
	if err := f.service.DeleteEnvironmentServiceReference(context.Background(), deliveryActionPrincipal(), "test-a", "svc", remove); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale delete: %v", err)
	}
	remove.ExpectedRevision--
	if err := f.service.DeleteEnvironmentServiceReference(context.Background(), deliveryActionPrincipal(), "test-a", "svc", remove); err != nil {
		t.Fatal(err)
	}
}

func TestActiveServiceReferenceRejectsLocalTargetsAndStaleCandidate(t *testing.T) {
	for _, mode := range []string{"local target", "candidate revision", "consumer CAS", "source revision", "configuration changed under lock", "revision zero with ID", "missing revision"} {
		t.Run(mode, func(t *testing.T) {
			f, r, input := activeReferenceFixture(t)
			switch mode {
			case "local target":
				b := f.catalog.bindings["test-a"]
				b.Targets = f.catalog.bindings["test-b"].Targets
				f.catalog.bindings["test-a"] = b
			case "candidate revision":
				input.ExpectedCandidateRevision++
			case "consumer CAS":
				input.ExpectedConsumerUpdatedAt = input.ExpectedConsumerUpdatedAt.Add(time.Second)
			case "source revision":
				input.ExpectedSourceRevision = strings.Repeat("0", 64)
			case "configuration changed under lock":
				r.beforeSave = func() { b := f.catalog.bindings["test-b"]; b.Alias = "changed"; f.catalog.bindings["test-b"] = b }
			case "revision zero with ID":
				input.ExpectedReferenceID = "old"
			case "missing revision":
				input.ExpectedRevision = nil
			}
			if _, err := f.service.PutEnvironmentServiceReference(context.Background(), deliveryActionPrincipal(), "test-a", "svc", input); err == nil {
				t.Fatal("invalid activation accepted")
			}
			if r.active.Reference.ID != "" {
				t.Fatal("partial activation")
			}
		})
	}
}
