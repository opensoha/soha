package directorysync

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
)

type Service struct {
	repository domain.Repository
	projector  LocalProjector
	now        func() time.Time
}

// PullSnapshot enforces the people gate before invoking any people-facing
// connector method. This is the safety boundary shared by scheduled/manual work.
func (s *Service) PullSnapshot(ctx context.Context, connectionID string, connector Connector) (domain.Snapshot, string, error) {
	connection, policy, err := s.repository.GetConnection(ctx, connectionID)
	if err != nil {
		return domain.Snapshot{}, "", err
	}
	organizations, cursor, err := connector.ListOrganizations(ctx, connection)
	if err != nil {
		return domain.Snapshot{}, "", err
	}
	snapshot := domain.Snapshot{Organizations: organizations}
	if !policy.SyncPeople {
		return snapshot, cursor, nil
	}
	people, err := connector.ListPeople(ctx, connection)
	if err != nil {
		return domain.Snapshot{}, "", err
	}
	memberships, err := connector.ListMemberships(ctx, connection)
	if err != nil {
		return domain.Snapshot{}, "", err
	}
	snapshot.People, snapshot.Memberships = people, memberships
	return snapshot, cursor, nil
}

func New(repository domain.Repository, projector LocalProjector) *Service {
	return &Service{repository: repository, projector: projector, now: time.Now}
}

func (s *Service) CreateConnection(ctx context.Context, connection domain.Connection, policy domain.Policy) (domain.Connection, error) {
	if connection.ID == "" {
		connection.ID = uuid.NewString()
	}
	if policy.ConnectionID == "" {
		policy = domain.DefaultPolicy(connection.ID)
	}
	policy.ConnectionID = connection.ID
	if err := policy.Validate(); err != nil {
		return domain.Connection{}, err
	}
	if policy.Mode != domain.PolicyManual {
		if err := ValidateSchedule(policy.Schedule); err != nil {
			return domain.Connection{}, fmt.Errorf("%w: %v", domain.ErrInvalidPolicy, err)
		}
	}
	now := s.now().UTC()
	connection.CreatedAt = now
	connection.UpdatedAt = now
	if connection.Status == "" {
		connection.Status = domain.ConnectionPending
	}
	return s.repository.CreateConnection(ctx, connection, policy)
}

func (s *Service) Preview(ctx context.Context, connectionID string, snapshot domain.Snapshot) (domain.Plan, error) {
	_, policy, existingOrgs, existingPeople, err := s.load(ctx, connectionID)
	if err != nil {
		return domain.Plan{}, err
	}
	return BuildPlan(snapshot, existingOrgs, existingPeople, policy.SyncPeople)
}

func (s *Service) Apply(ctx context.Context, connectionID string, snapshot domain.Snapshot, requestedBy string) (domain.Run, domain.Plan, error) {
	return s.apply(ctx, connectionID, snapshot, requestedBy, "manual")
}

func (s *Service) ApplyTriggered(ctx context.Context, connectionID string, snapshot domain.Snapshot, requestedBy, trigger string) (domain.Run, domain.Plan, error) {
	if trigger == "" {
		trigger = "manual"
	}
	return s.apply(ctx, connectionID, snapshot, requestedBy, trigger)
}

func (s *Service) apply(ctx context.Context, connectionID string, snapshot domain.Snapshot, requestedBy, trigger string) (domain.Run, domain.Plan, error) {
	connection, policy, existingOrgs, existingPeople, err := s.load(ctx, connectionID)
	if err != nil {
		return domain.Run{}, domain.Plan{}, err
	}
	plan, err := BuildPlan(snapshot, existingOrgs, existingPeople, policy.SyncPeople)
	if err != nil {
		return domain.Run{}, domain.Plan{}, err
	}
	now := s.now().UTC()
	run := domain.Run{ID: uuid.NewString(), ConnectionID: connectionID, Trigger: trigger, Mode: "full", Status: domain.RunQueued, IncludePeople: policy.SyncPeople, RequestedBy: requestedBy, CreatedAt: now}
	run, err = s.repository.CreateRun(ctx, run)
	if err != nil {
		return domain.Run{}, domain.Plan{}, err
	}
	if err = s.repository.TransitionRun(ctx, run.ID, domain.RunRunning, domain.RunStats{}, "", ""); err != nil {
		return run, plan, err
	}
	if s.projector != nil {
		if err = s.projector.Apply(ctx, connection, policy, plan); err != nil {
			return run, plan, s.fail(ctx, run.ID, "projection_apply_failed", err)
		}
	} else if err = s.repository.ApplyProjections(ctx, connectionID, plan.Organizations, plan.People, plan.Memberships, policy.SyncPeople); err != nil {
		return run, plan, s.fail(ctx, run.ID, "projection_apply_failed", err)
	}
	stats := statsFor(plan)
	if err = s.repository.TransitionRun(ctx, run.ID, domain.RunSucceeded, stats, "", ""); err != nil {
		return run, plan, err
	}
	run.Status = domain.RunSucceeded
	run.Stats = stats
	return run, plan, nil
}

func (s *Service) load(ctx context.Context, id string) (domain.Connection, domain.Policy, []domain.Organization, []domain.Person, error) {
	c, p, err := s.repository.GetConnection(ctx, id)
	if err != nil {
		return c, p, nil, nil, err
	}
	if err := p.Validate(); err != nil {
		return c, p, nil, nil, err
	}
	o, err := s.repository.ListOrganizations(ctx, id)
	if err != nil {
		return c, p, nil, nil, err
	}
	people, err := s.repository.ListPeople(ctx, id)
	return c, p, o, people, err
}

func (s *Service) fail(ctx context.Context, runID, code string, cause error) error {
	_ = s.repository.TransitionRun(ctx, runID, domain.RunFailed, domain.RunStats{}, code, cause.Error())
	return fmt.Errorf("%s: %w", code, cause)
}

func statsFor(plan domain.Plan) domain.RunStats {
	var s domain.RunStats
	for _, c := range plan.Changes {
		switch c.Kind + ":" + c.Action {
		case "organization:create":
			s.OrganizationsCreated++
		case "organization:update", "organization:restore":
			s.OrganizationsUpdated++
		case "organization:move":
			s.OrganizationsMoved++
		case "organization:archive":
			s.OrganizationsArchived++
		case "person:create":
			s.PeopleCreated++
		case "person:update", "person:restore":
			s.PeopleUpdated++
		case "person:archive":
			s.PeopleArchived++
		}
	}
	s.MembershipsUpdated = len(plan.Memberships)
	return s
}
