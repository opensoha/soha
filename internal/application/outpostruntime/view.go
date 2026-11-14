package outpostruntime

import (
	"context"
	"time"

	"github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/outpostruntime"
)

// View contains only management-facing fields. Executor identity, ownership
// fences and installation policy do not cross this application boundary.
type View struct {
	OutpostID, SlotID, Owner, Namespace, WorkloadName, WorkloadUID string
	Generation, ObservedGeneration                                 int64
	DesiredReplicas                                                int32
	ActualReplicas, ReadyReplicas                                  *int32
	Phase, Reason, LatestTaskID                                    string
	ObservedAt                                                     *time.Time
	SupportsScale, SupportsDrain                                   bool
}

func (s *Service) View(ctx context.Context, p identity.Principal, id string) (View, error) {
	state, err := s.Get(ctx, p, id)
	if err != nil {
		return View{}, err
	}
	return s.DescribeState(ctx, state)
}

func (s *Service) DescribeState(ctx context.Context, state domain.State) (View, error) {
	view := View{OutpostID: state.OutpostID, SlotID: state.SlotID, Owner: state.Owner, WorkloadUID: state.UID, Generation: state.Generation, ObservedGeneration: state.ObservedGeneration, DesiredReplicas: state.DesiredReplicas, ActualReplicas: state.ActualReplicas, ReadyReplicas: state.ReadyReplicas, Phase: domain.PublicPhase(state), Reason: state.Reason, LatestTaskID: state.LatestTaskID, ObservedAt: state.ObservedAt}
	slot, ok := s.slots[state.SlotID]
	if !ok || slot.OutpostID != state.OutpostID || slot.UID != state.UID {
		return view, nil
	}
	view.Namespace, view.WorkloadName = slot.Namespace, slot.Name
	view.SupportsScale = state.Owner == "soha" && s.executors[slot.ExecutorID]
	if !view.SupportsScale {
		return view, nil
	}
	status, err := s.repo.DrainStatus(ctx, state.OutpostID, state.Generation, time.Now().UTC())
	if err != nil {
		return View{}, err
	}
	// Fresh protocol observations prove drain-control support; configuration or
	// an Outpost mode alone is not evidence that the node understands the gate.
	view.SupportsDrain = status.Instances > 0
	return view, nil
}
