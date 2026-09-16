package deliverytrigger

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domain "github.com/opensoha/soha/internal/domain/deliverytrigger"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type triggerStore struct {
	Repository
	item     domain.StoredTrigger
	saves    int
	event    domain.StoredEvent
	finished bool
}

func (r *triggerStore) Get(context.Context, string) (domain.StoredTrigger, error) { return r.item, nil }
func (r *triggerStore) Save(_ context.Context, item domain.StoredTrigger, _ int) (domain.StoredTrigger, error) {
	r.item = item
	r.saves++
	return item, nil
}
func (r *triggerStore) Checkpoint(_ context.Context, event domain.StoredEvent) error {
	r.event = event
	return nil
}
func (r *triggerStore) Finish(_ context.Context, event domain.StoredEvent) error {
	r.event = event
	r.finished = true
	return nil
}
func (r *triggerStore) Enqueue(_ context.Context, event domain.StoredEvent) (domain.Event, error) {
	if r.event.EventID == event.EventID {
		if r.event.PayloadDigest != event.PayloadDigest {
			return domain.Event{}, apperrors.ErrConflict
		}
		return r.event.Event, nil
	}
	r.event = event
	return event.Event, nil
}

type triggerIdentities struct{ revoked map[string]bool }

func (r *triggerIdentities) CurrentExecutionPrincipal(_ context.Context, id, token string) (domainidentity.Principal, error) {
	if r.revoked[id] {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	return domainidentity.Principal{UserID: id, UserName: id, Roles: []string{"release"}, AccessTokenID: token}, nil
}
func (r *triggerIdentities) ParseAccessToken(ctx context.Context, token string) (domainidentity.Principal, domainidentity.AccessContext, error) {
	if token != "proof" {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, apperrors.ErrUnauthorized
	}
	p, err := r.CurrentExecutionPrincipal(ctx, "service_account:automation", "token")
	return p, domainidentity.AccessContext{TokenKind: "service_account_token", SubjectID: "automation", TokenID: "token"}, err
}

type triggerPermissions struct{}

func (triggerPermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"release": {"delivery.triggers.view", "delivery.triggers.create", "delivery.triggers.update", "delivery.template-sources.sync"}}, nil
}

type triggerSources struct {
	Sources
	source         domaindocument.Source
	denied         map[string]bool
	syncs, applies int
	afterSync      func()
	input          domaindocument.SyncInput
}

func (r *triggerSources) Get(_ context.Context, p domainidentity.Principal, _ string) (domaindocument.Source, error) {
	if r.denied[p.UserID] {
		return r.source, apperrors.ErrAccessDenied
	}
	return r.source, nil
}
func (r *triggerSources) Sync(_ context.Context, _ domainidentity.Principal, _ string, input domaindocument.SyncInput) (domaindocument.SyncRun, error) {
	r.syncs++
	r.input = input
	if r.afterSync != nil {
		r.afterSync()
	}
	return domaindocument.SyncRun{ID: "sync", Status: "ready", Preview: &domaindocument.Preview{CandidateDigest: "sha256:" + strings.Repeat("a", 64)}}, nil
}
func (r *triggerSources) Apply(context.Context, domainidentity.Principal, string, string, domaindocument.SyncApplyInput) (domaindocument.SyncRun, error) {
	r.applies++
	return domaindocument.SyncRun{ID: "sync", Status: "applied"}, nil
}

type triggerRepositories struct{}

func (triggerRepositories) GetRepository(context.Context, domainidentity.Principal, string) (domainapp.SourceRepository, error) {
	return domainapp.SourceRepository{ID: "repo", Provider: "gitlab", ProviderRepositoryID: "42", SourceConnectionID: "connection", URL: "https://git.example/repo.git"}, nil
}

type triggerGit struct{ head string }

func (r *triggerGit) ResolveDeliveryCommit(context.Context, domainapp.SourceRepository, string, string) (string, error) {
	return r.head, nil
}

type triggerWorkflows struct {
	Workflows
	creates   int
	workflow  domainworkflow.DeliveryWorkflow
	input     domainworkflow.DeliveryBatchInput
	principal domainidentity.Principal
	batch     domainworkflow.DeliveryBatch
}

func (r *triggerWorkflows) CreateDeliveryBatch(_ context.Context, principal domainidentity.Principal, input domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryBatch, error) {
	r.creates++
	r.input, r.principal = input, principal
	r.batch = domainworkflow.DeliveryBatch{ID: "batch", Status: "queued"}
	return r.batch, nil
}
func (r *triggerWorkflows) GetDeliveryWorkflow(context.Context, domainidentity.Principal, string) (domainworkflow.DeliveryWorkflow, error) {
	return r.workflow, nil
}
func (r *triggerWorkflows) PrepareDeliveryWorkflow(_ context.Context, _ domainidentity.Principal, _ string, input domainworkflow.DeliveryWorkflowInput) (domainworkflow.DeliveryWorkflowInput, error) {
	return input, nil
}
func (r *triggerWorkflows) GetDeliveryBatch(context.Context, domainidentity.Principal, string) (domainworkflow.DeliveryBatch, error) {
	return r.batch, nil
}
func (r *triggerWorkflows) FindDeliveryBatch(context.Context, domainidentity.Principal, domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryBatch, error) {
	if r.batch.ID == "" {
		return r.batch, apperrors.ErrNotFound
	}
	return r.batch, nil
}

func TestWorkflowTriggerPinsIdentityVersionCommitAndRecoversAcceptedBatch(t *testing.T) {
	s, repo, _, _, workflows, input := triggerFixture(t)
	workflows.workflow = domainworkflow.DeliveryWorkflow{ID: "workflow", Version: 3, Definition: domainworkflow.DeliveryWorkflowDefinition{Targets: []domainworkflow.DeliveryTargetInput{{Action: "build", RepositoryRefs: []domainbuild.RepositoryRef{{RepositoryID: "repo", RefType: "branch", RefName: "main"}}}}}}
	input.TargetKind, input.TargetID, input.WorkflowVersion, input.Type = "workflow", "workflow", 3, "webhook"
	input.Schedule = nil
	input.Webhook = &domain.Webhook{Provider: "gitlab_standard", RepositoryID: "repo", RefType: "branch", RefValue: "main"}
	input.WebhookSigningSecret = "whsec_" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	owner := triggerOwner()
	owner.AccessTokenID = "owner-token"
	if _, err := s.Save(t.Context(), owner, "", input); err != nil {
		t.Fatal(err)
	}
	event := pendingEvent(repo.item)
	event.EventType, event.ResolvedCommit = "webhook", strings.Repeat("a", 40)
	status, reason, err := s.dispatch(t.Context(), &event)
	if err != nil || status != "succeeded" || reason != "batch_accepted" || event.BatchID != "batch" || event.PreparedAt == nil || workflows.creates != 1 || workflows.principal.UserID != "service_account:automation" || workflows.principal.AccessTokenID != "token" || workflows.input.WorkflowVersion != 3 || workflows.input.SourceCommit.Commit != event.ResolvedCommit || workflows.input.TriggerAuthorizerID != "owner" || workflows.input.TriggerAuthorizerTokenID != "owner-token" {
		t.Fatalf("incorrect frozen dispatch: %+v %v", workflows.input, err)
	}
	assertWorkflowTriggerRecovery(t, s, repo, workflows, event)
}

func assertWorkflowTriggerRecovery(t *testing.T, s *Service, repo *triggerStore, workflows *triggerWorkflows, event domain.StoredEvent) {
	t.Helper()
	workflows.workflow.Version = 4
	event.BatchID = ""
	if _, _, err := s.dispatch(t.Context(), &event); err != nil || event.BatchID != "batch" || workflows.creates != 1 {
		t.Fatalf("accepted batch recovery failed: %v", err)
	}
	fresh := pendingEvent(repo.item)
	if _, _, err := s.dispatch(t.Context(), &fresh); !errors.Is(err, apperrors.ErrConflict) || workflows.creates != 1 {
		t.Fatalf("new event followed a changed workflow: %v", err)
	}
	workflows.workflow.Version = 3
	repo.item.LastBatchID = "batch"
	if status, reason, err := s.dispatch(t.Context(), &fresh); err != nil || status != "skipped" || reason != "previous_batch_active" || workflows.creates != 1 {
		t.Fatalf("overlapping batch accepted: %s %s %v", status, reason, err)
	}
}

func triggerFixture(t *testing.T) (*Service, *triggerStore, *triggerIdentities, *triggerSources, *triggerWorkflows, domain.Input) {
	t.Helper()
	repo := &triggerStore{}
	identities := &triggerIdentities{revoked: map[string]bool{}}
	sources := &triggerSources{source: domaindocument.Source{ID: "source", RepositoryID: "repo", RefType: "branch", RefValue: "main", Path: ".", Enabled: true, Generation: 4}, denied: map[string]bool{}}
	workflows := &triggerWorkflows{}
	service := New(repo, identities, sources, workflows, triggerRepositories{}, &triggerGit{head: strings.Repeat("a", 40)}, appaccess.NewPermissionResolver(triggerPermissions{}), nil, nil)
	input := domain.Input{Name: "Poll templates", Enabled: true, TargetKind: "template_source", TargetID: "source", Type: "poll", ServiceAccountToken: "proof", Schedule: &domain.Schedule{Cron: "* * * * *", TimeZone: "UTC"}}
	return service, repo, identities, sources, workflows, input
}

func triggerOwner() domainidentity.Principal {
	return domainidentity.Principal{UserID: "owner", Roles: []string{"release"}}
}
func pendingEvent(item domain.StoredTrigger) domain.StoredEvent {
	return domain.StoredEvent{Event: domain.Event{ID: "event", EventID: "slot", TriggerID: item.ID, TriggerRevision: item.Revision, EventType: "poll", Status: "processing", OccurredAt: time.Now().UTC(), CreatedAt: time.Now().UTC()}, Lease: "lease"}
}

func TestTriggerSaveDoesNotExecuteAndDispatchOnlyAppliesDrafts(t *testing.T) {
	s, repo, _, sources, workflows, input := triggerFixture(t)
	item, err := s.Save(t.Context(), triggerOwner(), "", input)
	if err != nil || repo.saves != 1 || sources.syncs != 0 || workflows.creates != 0 {
		t.Fatalf("save executed work: %v", err)
	}
	encoded, err := json.Marshal(item)
	if err != nil || strings.Contains(string(encoded), "proof") || strings.Contains(string(encoded), "executionToken") {
		t.Fatal("credentials escaped into response")
	}
	event := pendingEvent(repo.item)
	status, reason, err := s.dispatch(t.Context(), &event)
	if err != nil || status != "succeeded" || reason != "drafts_applied" || sources.syncs != 1 || sources.applies != 1 || workflows.creates != 0 || sources.input.ExpectedGeneration != 4 || sources.input.ResolvedCommit != strings.Repeat("a", 40) || event.SyncRunID != "sync" {
		t.Fatalf("incorrect source dispatch: %s %s %v %+v", status, reason, err, event)
	}
}

func TestTriggerRequiresBothSubjectsAndRechecksAfterGitRead(t *testing.T) {
	for _, subject := range []string{"owner", "service_account:automation"} {
		t.Run(subject, func(t *testing.T) {
			s, repo, identities, sources, _, input := triggerFixture(t)
			sources.denied[subject] = true
			if _, err := s.Save(t.Context(), triggerOwner(), "", input); !errors.Is(err, apperrors.ErrAccessDenied) || repo.saves != 0 {
				t.Fatalf("unauthorized delegation saved: %v", err)
			}
			delete(sources.denied, subject)
			if _, err := s.Save(t.Context(), triggerOwner(), "", input); err != nil {
				t.Fatal(err)
			}
			sources.afterSync = func() { identities.revoked[subject] = true }
			event := pendingEvent(repo.item)
			s.process(t.Context(), event)
			if sources.applies != 0 || !repo.finished || repo.event.Reason != "authorization_revoked" {
				t.Fatalf("revoked delegation applied drafts: %+v", repo.event)
			}
		})
	}
}

func TestTriggerDisableDoesNotRequireStillValidExecutionToken(t *testing.T) {
	s, repo, identities, _, _, input := triggerFixture(t)
	item, err := s.Save(t.Context(), triggerOwner(), "", input)
	if err != nil {
		t.Fatal(err)
	}
	identities.revoked["service_account:automation"] = true
	input.Enabled, input.ExpectedRevision, input.ServiceAccountToken = false, item.Revision, ""
	item, err = s.Save(t.Context(), triggerOwner(), item.ID, input)
	if err != nil || item.Enabled {
		t.Fatalf("cannot disable revoked automation: %v", err)
	}
	event := pendingEvent(repo.item)
	status, reason, err := s.dispatch(t.Context(), &event)
	if err != nil || status != "skipped" || reason != "trigger_changed" {
		t.Fatalf("disabled trigger dispatched: %s %s %v", status, reason, err)
	}
}

func signWebhook(secret, id, timestamp string, body []byte) string {
	key, _ := decodeSigningSecret(secret)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(id + "." + timestamp + "."))
	_, _ = mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestWebhookSignatureBindsEventTimeBodyAndRotation(t *testing.T) {
	now := time.Now().UTC()
	timestamp := strconv.FormatInt(now.Unix(), 10)
	secret := "whsec_" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))
	body := []byte(`{"object_kind":"push"}`)
	signature := signWebhook(secret, "event", timestamp, body)
	if _, err := verifyWebhook(secret, "event", timestamp, "v0,unknown "+signature, body, now); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, key, id, stamp string
		body                 []byte
		now                  time.Time
	}{
		{"event", secret, "other", timestamp, body, now}, {"body", secret, "event", timestamp, []byte(`{}`), now}, {"expired", secret, "event", timestamp, body, now.Add(6 * time.Minute)}, {"future", secret, "event", timestamp, body, now.Add(-6 * time.Minute)}, {"rotated", "whsec_" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("b", 32))), "event", timestamp, body, now},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := verifyWebhook(test.key, test.id, test.stamp, signature, test.body, test.now); !errors.Is(err, apperrors.ErrUnauthorized) {
				t.Fatal("invalid signature accepted")
			}
		})
	}
}

func TestWebhookDedupeAndOldCommitNeverDispatch(t *testing.T) {
	s, repo, _, sources, _, input := triggerFixture(t)
	secret := "whsec_" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))
	input.Type, input.Schedule, input.WebhookSigningSecret = "webhook", nil, secret
	input.Webhook = &domain.Webhook{Provider: "gitlab_standard", RepositoryID: "repo", RefType: "branch", RefValue: "main"}
	item, err := s.Save(t.Context(), triggerOwner(), "", input)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(fmt.Sprintf(`{"object_kind":"push","ref":"refs/heads/main","after":"%s","project":{"id":42},"provider_extension":true}`, strings.Repeat("b", 40)))
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature := signWebhook(secret, "provider-event", timestamp, body)
	first, err := s.ReceiveWebhook(t.Context(), item.ID, "provider-event", timestamp, signature, body)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ReceiveWebhook(t.Context(), item.ID, "provider-event", timestamp, signature, body)
	if err != nil || second.ID != first.ID {
		t.Fatal("duplicate event lost identity")
	}
	status, reason, err := s.dispatch(t.Context(), &repo.event)
	if err != nil || status != "skipped" || reason != "stale_commit" || sources.syncs != 0 {
		t.Fatalf("old commit dispatched: %s %s %v", status, reason, err)
	}
	body = []byte(strings.ReplaceAll(string(body), strings.Repeat("b", 40), strings.Repeat("a", 40)))
	if _, err := s.ReceiveWebhook(t.Context(), item.ID, "provider-event", timestamp, signWebhook(secret, "provider-event", timestamp, body), body); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("same ID with different signed body accepted")
	}
}

func TestScheduleTimeZonesDSTCalendarAndExclusions(t *testing.T) {
	parse := func(value string) time.Time {
		at, err := time.Parse(time.RFC3339, value)
		if err != nil {
			t.Fatal(err)
		}
		return at
	}
	schedule := domain.Schedule{Cron: "30 1 * * *", TimeZone: "America/New_York"}
	a, first := scheduleSlot(schedule, parse("2026-11-01T05:30:00Z"))
	b, second := scheduleSlot(schedule, parse("2026-11-01T06:30:00Z"))
	if !first || !second || a != b {
		t.Fatal("DST repeated wall-clock minute changed event identity")
	}
	schedule.Cron = "30 2 * * *"
	for _, at := range []string{"2026-03-08T06:30:00Z", "2026-03-08T07:30:00Z"} {
		if _, due := scheduleSlot(schedule, parse(at)); due {
			t.Fatal("nonexistent DST slot executed")
		}
	}
	schedule = domain.Schedule{TimeZone: "Asia/Shanghai", RunAt: []time.Time{parse("2026-09-13T10:00:00+08:00")}}
	if _, due := scheduleSlot(schedule, parse("2026-09-13T02:00:30Z")); !due {
		t.Fatal("calendar offset lost")
	}
	if _, due := scheduleSlot(schedule, parse("2026-09-13T02:01:00Z")); due {
		t.Fatal("missed calendar slot caught up")
	}
	schedule.ExcludedDates = []string{"2026-09-13"}
	if _, due := scheduleSlot(schedule, parse("2026-09-13T02:00:00Z")); due {
		t.Fatal("excluded date executed")
	}
	for _, invalid := range []domain.Schedule{{Cron: "* * *", TimeZone: "UTC"}, {Cron: "* * * * *", TimeZone: "bad/timezone"}, {TimeZone: "UTC"}, {Cron: "* * * * *", TimeZone: "UTC", ExcludedDates: []string{"2026-02-30"}}} {
		if validateSchedule(invalid) == nil {
			t.Fatalf("invalid schedule accepted: %+v", invalid)
		}
	}
}
