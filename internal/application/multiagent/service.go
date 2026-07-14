package multiagent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("multi-agent plan not found")

type Budget struct {
	MaxSteps     int     `json:"maxSteps"`
	MaxTokens    int     `json:"maxTokens"`
	MaxCost      float64 `json:"maxCost"`
	DeadlineSecs int     `json:"deadlineSeconds"`
}

type Subtask struct {
	ID              string   `json:"id"`
	AgentProfileRef string   `json:"agentProfileRef"`
	Input           string   `json:"input"`
	DependsOn       []string `json:"dependsOn,omitempty"`
	PermissionKeys  []string `json:"permissionKeys"`
	Budget          Budget   `json:"budget"`
	Status          string   `json:"status"`
	OutputRef       string   `json:"outputRef,omitempty"`
	ErrorCode       string   `json:"errorCode,omitempty"`
}

type Plan struct {
	ID                   string     `json:"id"`
	CoordinatorRef       string     `json:"coordinatorRef"`
	PrincipalPermissions []string   `json:"principalPermissions"`
	Subtasks             []Subtask  `json:"subtasks"`
	SharedBudget         Budget     `json:"sharedBudget"`
	Status               string     `json:"status"`
	MergedOutputRefs     []string   `json:"mergedOutputRefs,omitempty"`
	CreatedAt            time.Time  `json:"createdAt"`
	CompletedAt          *time.Time `json:"completedAt,omitempty"`
}

type Store interface {
	Put(context.Context, Plan) error
	Get(context.Context, string) (Plan, error)
	List(context.Context) ([]Plan, error)
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("multi-agent store is required")
	}
	return &Service{store: store, now: time.Now}, nil
}

func (s *Service) Create(ctx context.Context, plan Plan, grantedPermissions []string) (Plan, error) {
	plan.ID = strings.TrimSpace(plan.ID)
	plan.CoordinatorRef = strings.TrimSpace(plan.CoordinatorRef)
	if plan.ID == "" || plan.CoordinatorRef == "" || len(plan.Subtasks) == 0 || len(plan.Subtasks) > 8 {
		return Plan{}, fmt.Errorf("invalid bounded multi-agent plan")
	}
	if err := validateBudget(plan.SharedBudget); err != nil {
		return Plan{}, err
	}
	plan.PrincipalPermissions = normalizePermissionKeys(grantedPermissions)
	allowed := make(map[string]struct{}, len(plan.PrincipalPermissions))
	for _, permission := range plan.PrincipalPermissions {
		allowed[permission] = struct{}{}
	}
	ids := map[string]struct{}{}
	for i := range plan.Subtasks {
		task := &plan.Subtasks[i]
		task.ID = strings.TrimSpace(task.ID)
		if task.ID == "" || task.AgentProfileRef == "" || task.Input == "" {
			return Plan{}, fmt.Errorf("multi-agent subtask identity is required")
		}
		if _, duplicate := ids[task.ID]; duplicate {
			return Plan{}, fmt.Errorf("duplicate multi-agent subtask %q", task.ID)
		}
		ids[task.ID] = struct{}{}
		if err := validateBudget(task.Budget); err != nil {
			return Plan{}, err
		}
		for _, permission := range task.PermissionKeys {
			if _, ok := allowed[permission]; !ok {
				return Plan{}, fmt.Errorf("subtask %q exceeds principal permissions", task.ID)
			}
		}
		task.Status = "pending"
	}
	for _, task := range plan.Subtasks {
		for _, dependency := range task.DependsOn {
			if _, ok := ids[dependency]; !ok || dependency == task.ID {
				return Plan{}, fmt.Errorf("invalid multi-agent dependency")
			}
		}
	}
	plan.Status = "running"
	plan.CreatedAt = s.now().UTC()
	if err := s.store.Put(ctx, clonePlan(plan)); err != nil {
		return Plan{}, err
	}
	return clonePlan(plan), nil
}

func normalizePermissionKeys(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func (s *Service) CompleteSubtask(ctx context.Context, planID, subtaskID, outputRef string) (Plan, error) {
	plan, err := s.store.Get(ctx, strings.TrimSpace(planID))
	if err != nil {
		return Plan{}, err
	}
	if plan.Status != "running" {
		return Plan{}, fmt.Errorf("multi-agent plan is terminal")
	}
	found := false
	for i := range plan.Subtasks {
		if plan.Subtasks[i].ID != subtaskID {
			continue
		}
		for _, dependency := range plan.Subtasks[i].DependsOn {
			if subtaskStatus(plan.Subtasks, dependency) != "completed" {
				return Plan{}, fmt.Errorf("multi-agent dependencies are incomplete")
			}
		}
		plan.Subtasks[i].Status = "completed"
		plan.Subtasks[i].OutputRef = strings.TrimSpace(outputRef)
		found = true
		break
	}
	if !found || outputRef == "" {
		return Plan{}, fmt.Errorf("multi-agent subtask or output is invalid")
	}
	allCompleted := true
	outputs := make([]string, 0, len(plan.Subtasks))
	for _, task := range plan.Subtasks {
		allCompleted = allCompleted && task.Status == "completed"
		if task.OutputRef != "" {
			outputs = append(outputs, task.OutputRef)
		}
	}
	if allCompleted {
		now := s.now().UTC()
		plan.Status = "completed"
		plan.CompletedAt = &now
		plan.MergedOutputRefs = outputs
	}
	if err := s.store.Put(ctx, plan); err != nil {
		return Plan{}, err
	}
	return clonePlan(plan), nil
}

func (s *Service) Cancel(ctx context.Context, id string) (Plan, error) {
	plan, err := s.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return Plan{}, err
	}
	if plan.Status == "completed" || plan.Status == "cancelled" {
		return plan, nil
	}
	for i := range plan.Subtasks {
		if plan.Subtasks[i].Status != "completed" {
			plan.Subtasks[i].Status = "cancelled"
		}
	}
	now := s.now().UTC()
	plan.Status = "cancelled"
	plan.CompletedAt = &now
	if err := s.store.Put(ctx, plan); err != nil {
		return Plan{}, err
	}
	return clonePlan(plan), nil
}

func (s *Service) List(ctx context.Context) ([]Plan, error) { return s.store.List(ctx) }

type MemoryStore struct {
	mu    sync.RWMutex
	plans map[string]Plan
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{plans: map[string]Plan{}} }
func (s *MemoryStore) Put(_ context.Context, plan Plan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plans[plan.ID] = clonePlan(plan)
	return nil
}
func (s *MemoryStore) Get(_ context.Context, id string) (Plan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	plan, ok := s.plans[id]
	if !ok {
		return Plan{}, ErrNotFound
	}
	return clonePlan(plan), nil
}
func (s *MemoryStore) List(context.Context) ([]Plan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Plan, 0, len(s.plans))
	for _, plan := range s.plans {
		items = append(items, clonePlan(plan))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func validateBudget(budget Budget) error {
	if budget.MaxSteps <= 0 || budget.MaxSteps > 100 || budget.MaxTokens <= 0 || budget.MaxTokens > 2_000_000 || budget.MaxCost < 0 || budget.DeadlineSecs <= 0 || budget.DeadlineSecs > 86_400 {
		return fmt.Errorf("invalid multi-agent budget")
	}
	return nil
}
func subtaskStatus(tasks []Subtask, id string) string {
	for _, task := range tasks {
		if task.ID == id {
			return task.Status
		}
	}
	return ""
}
func clonePlan(plan Plan) Plan {
	plan.PrincipalPermissions = slices.Clone(plan.PrincipalPermissions)
	plan.MergedOutputRefs = slices.Clone(plan.MergedOutputRefs)
	plan.Subtasks = slices.Clone(plan.Subtasks)
	for i := range plan.Subtasks {
		plan.Subtasks[i].DependsOn = slices.Clone(plan.Subtasks[i].DependsOn)
		plan.Subtasks[i].PermissionKeys = slices.Clone(plan.Subtasks[i].PermissionKeys)
	}
	return plan
}
