package contentdlp

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domain "github.com/opensoha/soha/internal/domain/contentdlp"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	PermissionView    = "docker.dlp-policy.view"
	PermissionUpdate  = "docker.dlp-policy.update"
	PermissionPublish = "docker.dlp-policy.publish"
	PermissionEvents  = "docker.dlp-events.view"
)

type Store interface {
	Policy(context.Context, string) (domain.Policy, error)
	Save(context.Context, domain.Policy, domain.Event) (domain.Policy, error)
	Publish(context.Context, string, int64, domain.Event) (domain.Policy, error)
	Commit(context.Context, domain.Event, time.Time) error
	Events(context.Context, string) ([]domain.Event, error)
}
type Projects interface {
	GetProject(context.Context, string) (domaindocker.Project, error)
}
type Scope interface {
	Authorize(context.Context, domainaccess.Request) (domainaccess.Decision, error)
}

type Service struct {
	stepUp         StepUpVerifier
	workflows      WorkflowStore
	transfers      TransferStore
	linkage        NetworkLinkage
	store          Store
	projects       Projects
	scope          Scope
	permissions    *appaccess.PermissionResolver
	slots          chan struct{}
	fingerprintKey [32]byte
}
type Snapshot struct {
	Policy  domain.Policy
	Project domaindocker.Project
}

func New(store Store, projects Projects, scope Scope, permissions *appaccess.PermissionResolver, opts ...Option) (*Service, error) {
	for _, v := range []any{store, projects, scope, permissions} {
		if v == nil || reflect.ValueOf(v).Kind() == reflect.Pointer && reflect.ValueOf(v).IsNil() {
			return nil, fmt.Errorf("content DLP dependencies are required")
		}
	}
	s := &Service{store: store, projects: projects, scope: scope, permissions: permissions, slots: make(chan struct{}, 8)}
	if _, err := rand.Read(s.fingerprintKey[:]); err != nil {
		return nil, fmt.Errorf("content DLP fingerprint key: %w", err)
	}
	s.workflows, _ = store.(WorkflowStore)
	s.transfers, _ = store.(TransferStore)
	if s.workflows == nil || s.transfers == nil {
		return nil, fmt.Errorf("content DLP workflow store is required")
	}
	for _, option := range opts {
		option(s)
	}
	return s, nil
}

func (s *Service) authorize(ctx context.Context, p domainidentity.Principal, key string, project domaindocker.Project) error {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, p, key); err != nil {
		return err
	}
	d, err := s.scope.Authorize(ctx, domainaccess.Request{Principal: p, PermissionKey: key,
		Subject:  domainaccess.SubjectAttributes{UserID: p.UserID, Roles: p.Roles, Teams: p.Teams, Projects: p.Projects, Tags: p.Tags},
		Resource: domainaccess.ResourceAttributes{Group: "docker", Kind: "DockerProject", Name: project.ID, Owner: project.Owner, Labels: map[string]string{"docker.projectId": project.ID, "docker.hostId": project.HostID, "docker.team": project.Team, "docker.environment": project.Environment}},
		Context:  domainaccess.ContextAttributes{Source: "api", OccurredAt: time.Now().UTC()}})
	if err != nil {
		return err
	}
	if !d.Allowed {
		return apperrors.ErrAccessDenied
	}
	return nil
}

func (s *Service) project(ctx context.Context, p domainidentity.Principal, id, key string) (domaindocker.Project, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, p, key); err != nil {
		return domaindocker.Project{}, err
	}
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 128 {
		return domaindocker.Project{}, apperrors.ErrInvalidArgument
	}
	project, err := s.projects.GetProject(ctx, id)
	if err != nil {
		return project, err
	}
	return project, s.authorize(ctx, p, key, project)
}

func (s *Service) Get(ctx context.Context, p domainidentity.Principal, id string) (domain.Policy, error) {
	project, err := s.project(ctx, p, id, PermissionView)
	if err != nil {
		return domain.Policy{}, err
	}
	return s.store.Policy(ctx, project.ID)
}
func (s *Service) Save(ctx context.Context, p domainidentity.Principal, id string, input domain.SaveInput) (domain.Policy, error) {
	project, err := s.project(ctx, p, id, PermissionUpdate)
	if err != nil {
		return domain.Policy{}, err
	}
	if input.ExpectedRevision < 0 {
		return domain.Policy{}, apperrors.ErrInvalidArgument
	}
	if err := validateDockerPolicy(input.Draft); err != nil {
		return domain.Policy{}, err
	}
	if err := s.authorizeContentLink(ctx, p, input.Draft); err != nil {
		return domain.Policy{}, err
	}
	policy := domain.Policy{ProjectID: project.ID, Revision: input.ExpectedRevision, Draft: input.Draft}
	event := mutationEvent(p, policy, "policy_saved")
	event.ProjectUpdatedAt = project.UpdatedAt
	return s.store.Save(ctx, policy, event)
}
func (s *Service) Publish(ctx context.Context, p domainidentity.Principal, id string, input domain.PublishInput) (domain.Policy, error) {
	project, err := s.project(ctx, p, id, PermissionPublish)
	if err != nil {
		return domain.Policy{}, err
	}
	policy, err := s.store.Policy(ctx, project.ID)
	if err != nil {
		return policy, err
	}
	if input.ExpectedRevision < 1 || policy.Revision != input.ExpectedRevision {
		return domain.Policy{}, apperrors.ErrConflict
	}
	if err := validateDockerPolicy(policy.Draft); err != nil {
		return domain.Policy{}, err
	}
	if err := s.authorizeContentLink(ctx, p, policy.Draft); err != nil {
		return domain.Policy{}, err
	}
	event := mutationEvent(p, policy, "policy_published")
	event.ProjectUpdatedAt = project.UpdatedAt
	return s.store.Publish(ctx, project.ID, input.ExpectedRevision, event)
}
func (s *Service) Events(ctx context.Context, p domainidentity.Principal, id string) ([]domain.Event, error) {
	project, err := s.project(ctx, p, id, PermissionEvents)
	if err != nil {
		return nil, err
	}
	return s.store.Events(ctx, project.ID)
}

func mutationEvent(p domainidentity.Principal, policy domain.Policy, action string) domain.Event {
	id := uuid.NewString()
	return domain.Event{ID: id, ProjectID: policy.ProjectID, ActorID: p.UserID, Action: action, Revision: policy.Revision + 1, CreatedAt: time.Now().UTC(), Decision: domain.Decision{EventID: id, Mode: policy.Draft.Mode, Status: "clean", Allowed: true, DetectorVersion: domain.DetectorVersion, MatchedRuleIDs: []string{}}}
}

// Prepare does not release content. A missing policy retains the legacy channel,
// but a failed policy lookup never bypasses the guard.
func (s *Service) Prepare(ctx context.Context, p domainidentity.Principal, id string) (Snapshot, error) {
	policy, err := s.store.Policy(ctx, id)
	if err != nil {
		return Snapshot{}, err
	}
	project, err := s.projects.GetProject(ctx, id)
	if err != nil {
		return Snapshot{}, err
	}
	if policy.Published != nil {
		if err := validateDockerPolicy(*policy.Published); err != nil {
			return Snapshot{}, apperrors.ErrServiceUnavailable
		}
	}
	if policy.Published != nil && policy.Published.Enabled {
		if err := s.authorize(ctx, p, appaccess.PermDockerServicesView, project); err != nil {
			return Snapshot{}, err
		}
	}
	return Snapshot{Policy: policy, Project: project}, nil
}

func (s *Service) Check(ctx context.Context, p domainidentity.Principal, snapshot Snapshot, file domaindocker.ProjectVolumeFileContent, authorization ...domain.TransferAccess) (*domain.Decision, error) {
	policy := snapshot.Policy
	if policy.Published == nil || !policy.Published.Enabled {
		// Even a previously disabled policy may have been published while reading.
		current, err := s.store.Policy(ctx, policy.ProjectID)
		if err != nil {
			return nil, err
		}
		if current.PublishedVersion != policy.PublishedVersion {
			return nil, apperrors.ErrConflict
		}
		return nil, nil
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return nil, apperrors.ErrServiceUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := s.project(ctx, p, policy.ProjectID, appaccess.PermDockerServicesView)
	if err != nil {
		return nil, err
	}
	if !current.UpdatedAt.Equal(snapshot.Project.UpdatedAt) || current.HostID != snapshot.Project.HostID {
		return nil, apperrors.ErrConflict
	}
	event, binding, err := s.checkedEvent(ctx, p, policy, file, current.UpdatedAt)
	if err != nil {
		return nil, err
	}
	d := event.Decision
	access := domain.TransferAccess{}
	if len(authorization) > 0 {
		access = authorization[0]
	}
	var proof *domain.Proof
	if d.Status == "matched" {
		proof, err = s.transferProof(ctx, p, policy, file, access)
		if proof != nil && !proof.Record.Binding.ProjectUpdatedAt.Equal(current.UpdatedAt) {
			proof = nil
		}
		if err != nil {
			return nil, err
		}
	}
	d, err = s.commitCheckedTransfer(ctx, event, binding, proof, current.UpdatedAt, policy.Published.NetworkAction)
	if err != nil {
		return nil, err
	}
	if !d.Allowed {
		return nil, &Denial{Decision: d}
	}

	return &d, nil
}

// Per-event fingerprints bind the response to its receipt without publishing an
// offline dictionary oracle. The process key is random and never persisted.
func (s *Service) fingerprint(kind, eventID string, content []byte) string {
	mac := hmac.New(sha256.New, s.fingerprintKey[:])
	_, _ = mac.Write([]byte(kind + "\x00" + eventID + "\x00"))
	_, _ = mac.Write(content)
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Service) checkedEvent(ctx context.Context, p domainidentity.Principal, policy domain.Policy, file domaindocker.ProjectVolumeFileContent, updated time.Time) (domain.Event, domain.Binding, error) {
	d := scan(ctx, *policy.Published, file.Content, file.SizeBytes, file.Truncated)
	if d.Status == "error" {
		d.Allowed = false
	}
	d.EventID = uuid.NewString()
	d.PolicyVersion = policy.PublishedVersion
	target, _ := json.Marshal([]string{policy.ProjectID, file.ServiceName, file.Target, file.Path, "core-to-browser-volume-file"})
	inspected := int64(0)
	if d.Status == "clean" || d.Status == "matched" {
		inspected = int64(len(file.Content))
		d.ContentFingerprint = s.fingerprint("content", d.EventID, []byte(file.Content))
	}
	if !d.Allowed && d.Status == "matched" {
		if d.Mode == "warn" {
			d.RequiredAction = "acknowledge"
		} else {
			d.RequiredAction = "appeal"
		}
	}
	event := domain.Event{Decision: d, ID: d.EventID, ProjectID: policy.ProjectID, ActorID: p.UserID, Action: "transfer", Revision: policy.Revision, TargetFingerprint: s.fingerprint("target", d.EventID, target), SizeBytes: max(0, file.SizeBytes), InspectedBytes: inspected, CreatedAt: time.Now().UTC()}
	binding := domain.Binding{Key: make([]byte, 32), ExpiresAt: event.CreatedAt.Add(24 * time.Hour), ProjectUpdatedAt: updated}
	if _, err := rand.Read(binding.Key); err != nil {
		return domain.Event{}, domain.Binding{}, err
	}
	binding.Tag = bindingTag(binding.Key, p.UserID, policy, file)

	return event, binding, nil
}

func (s *Service) commitCheckedTransfer(ctx context.Context, event domain.Event, binding domain.Binding, proof *domain.Proof, updated time.Time, networkAction string) (domain.Decision, error) {
	d := event.Decision
	err := s.atomic(ctx, func(ctx context.Context) error {
		var err error
		d, err = s.transfers.CommitTransfer(ctx, event, binding, proof, updated)
		if err != nil {
			return err
		}
		if !d.Allowed && d.Status == "matched" && networkAction == "revoke_network" {
			if s.linkage == nil {
				return apperrors.ErrServiceUnavailable
			}
			holdID, err := s.linkage.Restrict(ctx, event)
			if err != nil {
				return err
			}
			return s.transfers.AttachHold(ctx, event.ID, holdID)
		}
		return nil
	})
	return d, err
}
