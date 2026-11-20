package assistantmesh

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/domain/knowledge"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type memoryStore struct {
	records map[string]domain.Record
}

func newMemoryStore() *memoryStore { return &memoryStore{records: map[string]domain.Record{}} }

func (m *memoryStore) Create(_ context.Context, record domain.Record) error {
	if _, ok := m.records[record.ID]; ok {
		return apperrors.ErrConflict
	}
	m.records[record.ID] = record
	return nil
}

func (m *memoryStore) Get(_ context.Context, id string) (domain.Record, error) {
	record, ok := m.records[id]
	if !ok {
		return domain.Record{}, apperrors.ErrNotFound
	}
	return record, nil
}

func (m *memoryStore) List(_ context.Context, kind, owner string, limit, offset int) ([]domain.Record, error) {
	records := make([]domain.Record, 0, len(m.records))
	for _, record := range m.records {
		if kind != "" && record.Kind != kind || owner != "" && record.OwnerID != owner {
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	if offset >= len(records) {
		return nil, nil
	}
	records = records[offset:]
	if len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

func (m *memoryStore) Update(_ context.Context, id string, mutate func(context.Context, domain.Record) (json.RawMessage, error)) error {
	record, ok := m.records[id]
	if !ok {
		return apperrors.ErrNotFound
	}
	raw, err := mutate(context.Background(), record)
	if err != nil {
		return err
	}
	record.Payload = raw
	m.records[id] = record
	return nil
}

type fakeSettings struct {
	value domainsettings.AssistantMeshSettings
}

func (f *fakeSettings) ResolveAssistantMeshSettings(context.Context) (domainsettings.AssistantMeshSettings, error) {
	return f.value, nil
}

type fakeUsers struct {
	users map[string]domainidentity.User
}

func (f *fakeUsers) GetByID(_ context.Context, id string) (domainidentity.User, error) {
	user, ok := f.users[id]
	if !ok {
		return domainidentity.User{}, apperrors.ErrNotFound
	}
	return user, nil
}

type fakeRoles struct{}

func (fakeRoles) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{
		"employee": {appaccess.PermObserveAIChatUse},
		"admin":    {appaccess.PermObserveAIChatUse, appaccess.ManagedActionPermission(appaccess.PermSettingsAIManage, "update")},
	}, nil
}

type fakeRunner struct {
	launches []RunSpec
	status   string
}

func (f *fakeRunner) LaunchMeshSkill(_ context.Context, _ domainidentity.Principal, spec RunSpec) (string, error) {
	f.launches = append(f.launches, spec)
	return "run-1", nil
}

func (f *fakeRunner) MeshRunStatus(context.Context, domainidentity.Principal, string, string, string) (string, error) {
	return f.status, nil
}

type fakeAudit struct{ entries []domainaudit.Entry }

func (f *fakeAudit) Record(_ context.Context, entry domainaudit.Entry) error {
	f.entries = append(f.entries, entry)
	return nil
}

type fixture struct {
	service *Service
	store   *memoryStore
	runner  *fakeRunner
	audit   *fakeAudit
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	store := newMemoryStore()
	runner := &fakeRunner{status: "completed"}
	audit := &fakeAudit{}
	settings := &fakeSettings{value: domainsettings.AssistantMeshSettings{Enabled: true, DefaultVisibility: "private", ExperienceCardLifetimeDays: 90, AllowPermanentExperienceCards: true}}
	users := &fakeUsers{users: map[string]domainidentity.User{
		"user-a": {ID: "user-a", Status: "active"}, "user-b": {ID: "user-b", Status: "active"}, "admin": {ID: "admin", Status: "active"},
	}}
	service := New(store, settings, runner, users, appaccess.NewPermissionResolver(fakeRoles{}), audit)
	return &fixture{service: service, store: store, runner: runner, audit: audit}
}

func employee(id string, teams ...string) domainidentity.Principal {
	return domainidentity.Principal{UserID: id, UserName: id, Roles: []string{"employee"}, Teams: teams}
}

func admin() domainidentity.Principal {
	return domainidentity.Principal{UserID: "admin", UserName: "admin", Roles: []string{"admin"}}
}

func skillInput(body string) domain.AssetInput {
	return domain.AssetInput{Kind: domain.KindSkill, Title: "排查工具", Body: body}
}

func (f *fixture) trial(t *testing.T, principal domainidentity.Principal, asset Asset) {
	t.Helper()
	if _, err := f.service.Act(context.Background(), principal, asset.ID, "trial", ActionRequest{ExpectedDigest: asset.DraftDigest, SessionID: "session-1", Prompt: "run"}); err != nil {
		t.Fatalf("trial: %v", err)
	}
}

func TestCreateRejectsUnsupportedKindAndInactiveCaller(t *testing.T) {
	f := newFixture(t)
	if _, err := f.service.Create(context.Background(), employee("user-a"), domain.AssetInput{Kind: "mcp", Title: "x", Body: "y"}); !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("mcp create err = %v", err)
	}
	if _, err := f.service.Create(context.Background(), employee("user-x"), skillInput("body")); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("inactive create err = %v", err)
	}
}

func TestDraftAllowsFindingsButTrialBlocks(t *testing.T) {
	f := newFixture(t)
	asset, err := f.service.Create(context.Background(), employee("user-a"), skillInput("Authorization: Bearer abcdefghijklmnopqrstuvwxyz123456"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(asset.Findings) == 0 {
		t.Fatal("expected findings on draft")
	}
	_, err = f.service.Act(context.Background(), employee("user-a"), asset.ID, "trial", ActionRequest{ExpectedDigest: asset.DraftDigest, SessionID: "s", Prompt: "p"})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("trial err = %v", err)
	}
}

func TestUpdateClearsTrialAndRejectsStaleDigest(t *testing.T) {
	f := newFixture(t)
	asset, err := f.service.Create(context.Background(), employee("user-a"), skillInput("v1"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	f.trial(t, employee("user-a"), asset)
	if _, err := f.service.Update(context.Background(), employee("user-a"), asset.ID, "deadbeef", skillInput("v2")); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale update err = %v", err)
	}
	updated, err := f.service.Update(context.Background(), employee("user-a"), asset.ID, asset.DraftDigest, skillInput("v2"))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Trial != nil {
		t.Fatal("trial must be cleared on digest change")
	}
}

func TestPublishRequiresTrialAndSharedGetHidesDraft(t *testing.T) {
	f := newFixture(t)
	asset, err := f.service.Create(context.Background(), employee("user-a", "team-1"), skillInput("body"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = f.service.Act(context.Background(), employee("user-a", "team-1"), asset.ID, "publish", ActionRequest{ExpectedDigest: asset.DraftDigest, Audience: knowledge.AccessScope{Visibility: "teams", Teams: []string{"team-1"}}})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("publish without trial err = %v", err)
	}
	f.trial(t, employee("user-a", "team-1"), asset)
	result, err := f.service.Act(context.Background(), employee("user-a", "team-1"), asset.ID, "publish", ActionRequest{ExpectedDigest: asset.DraftDigest, Audience: knowledge.AccessScope{Visibility: "teams", Teams: []string{"team-1"}}})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if result.Asset.Published == nil || result.Asset.Published.Version != 1 {
		t.Fatalf("published = %+v", result.Asset.Published)
	}
	shared, err := f.service.Get(context.Background(), employee("user-b", "team-1"), asset.ID)
	if err != nil {
		t.Fatalf("shared get: %v", err)
	}
	if shared.Draft != nil || shared.Trial != nil || len(shared.Findings) > 0 || shared.Published == nil {
		t.Fatalf("shared asset leaked owner fields: %+v", shared)
	}
	if _, err := f.service.Get(context.Background(), employee("user-b", "team-2"), asset.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("out-of-scope get err = %v", err)
	}
	if _, err := f.service.Act(context.Background(), employee("user-a", "team-1"), asset.ID, "revoke", ActionRequest{ExpectedDigest: asset.DraftDigest}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := f.service.Get(context.Background(), employee("user-b", "team-1"), asset.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("revoked get err = %v", err)
	}
}

func TestOrganizationReviewRequiresIndependentReviewer(t *testing.T) {
	f := newFixture(t)
	owner := employee("user-a")
	asset, err := f.service.Create(context.Background(), owner, skillInput("body"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	f.trial(t, owner, asset)
	result, err := f.service.Act(context.Background(), owner, asset.ID, "publish", ActionRequest{ExpectedDigest: asset.DraftDigest, Audience: knowledge.AccessScope{Visibility: "organization"}})
	if err != nil {
		t.Fatalf("request review: %v", err)
	}
	if result.Asset.Review == nil || result.Asset.Review.Status != "pending" || result.Asset.Published != nil {
		t.Fatalf("review = %+v", result.Asset.Review)
	}
	if _, err := f.service.Get(context.Background(), employee("user-b"), asset.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("pending get err = %v", err)
	}
	pending, err := f.service.Get(context.Background(), admin(), asset.ID)
	if err != nil || pending.Review == nil || pending.Draft != nil {
		t.Fatalf("reviewer view err = %v asset = %+v", err, pending)
	}
	if _, err := f.service.Act(context.Background(), owner, asset.ID, "approve", ActionRequest{}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("self approve err = %v", err)
	}
	if _, err := f.service.Act(context.Background(), admin(), asset.ID, "approve", ActionRequest{ExpectedDigest: asset.DraftDigest}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("draft fingerprint accepted as approval: %v", err)
	}
	if _, err := f.service.Act(context.Background(), admin(), asset.ID, "approve", ActionRequest{ExpectedDigest: result.Asset.Review.Candidate.Digest}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	shared, err := f.service.Get(context.Background(), employee("user-b"), asset.ID)
	if err != nil || shared.Published == nil || shared.Review != nil {
		t.Fatalf("approved get err = %v asset = %+v", err, shared)
	}
}

func TestAssistantMeshExperienceCardPublishesWithoutRun(t *testing.T) {
	f := newFixture(t)
	owner := employee("user-a", "team-1")
	asset, err := f.service.Create(t.Context(), owner, domain.AssetInput{Kind: domain.KindExperienceCard, Title: "Onboarding", Body: "Follow the checklist."})
	if err != nil {
		t.Fatal(err)
	}
	request := ActionRequest{ExpectedDigest: asset.DraftDigest, Audience: knowledge.AccessScope{Visibility: "teams", Teams: []string{"team-1"}}}
	published, err := f.service.Act(t.Context(), owner, asset.ID, "publish", request)
	if err != nil || published.Asset.Published == nil || published.Asset.Published.ExpiresAt == nil {
		t.Fatalf("expected published experience card with default expiry, got %+v, %v", published, err)
	}
	request.SessionID, request.Prompt = "session", "run"
	if _, err := f.service.Act(t.Context(), owner, asset.ID, "run", request); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("experience card entered executable runtime: %v", err)
	}
}

func TestAssistantMeshDLPFindsPrivateAddressInZipAttachment(t *testing.T) {
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	entry, err := archive.Create("notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("example internal endpoint: 10.24.12.45")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	input := skillInput("Archived notes")
	input.Files = map[string]string{"notes.zip": base64.StdEncoding.EncodeToString(data.Bytes())}
	f := newFixture(t)
	owner := employee("user-a")
	asset, err := f.service.Create(t.Context(), owner, input)
	if err != nil || len(asset.Findings) != 1 || asset.Findings[0] != "internal_ip" {
		t.Fatalf("ZIP attachment findings = %v, %v", asset.Findings, err)
	}
	_, err = f.service.Act(t.Context(), owner, asset.ID, "trial", ActionRequest{ExpectedDigest: asset.DraftDigest, SessionID: "s", Prompt: "p"})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("sensitive archive trial returned %v", err)
	}
}
