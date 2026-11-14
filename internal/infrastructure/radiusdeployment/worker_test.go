package radiusdeployment

import (
	"context"
	"errors"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"strings"
	"testing"
	"time"
)

func testSlot() domain.Slot {
	return domain.Slot{ID: "slot", NASID: "nas", NASName: "NAS", SiteID: "site", NASBindingID: "binding", AccessMedium: "wired", ExecutorID: "edge", Namespace: "soha", Name: "radius", UID: "uid", WorkloadKind: "Deployment", ServiceAccount: "radius", TemplateDigest: "sha256:" + strings.Repeat("a", 64), MaxReplicas: 1, Members: []domain.Member{{MemberID: "member", RuntimeID: "radius-member:a", Endpoint: "radius.example.test:1812", CredentialRef: "secretref:member/a"}}, Configurations: []domain.Configuration{{Material: api.NetworkRADIUSConfiguration{Reference: "approved", TreeHash: "sha256:" + strings.Repeat("b", 64), NasSharedSecretRef: "secretref:nas/a", EapServerCertificateRef: "secretref:eap/cert", EapServerKeyRef: "secretref:eap/key", EapCARef: "secretref:eap/ca", HooksAttested: true}}}} // #nosec G101 -- Installer fixture contains secret references and credential IDs, never credential values.
}

type fakeExecution struct {
	task      delivery.ExecutionTask
	drain     domain.DrainStatus
	late      bool
	callbacks []delivery.ExecutionCallbackInput
}

func (f *fakeExecution) Claim(context.Context) (delivery.ExecutionTask, error) { return f.task, nil }
func (f *fakeExecution) Status(context.Context, string) (delivery.ExecutionTask, error) {
	if f.late {
		return delivery.ExecutionTask{}, apperrors.ErrConflict
	}
	v := f.task
	v.Result = map[string]any{"radiusDeploymentDrain": f.drain}
	return v, nil
}
func (f *fakeExecution) Callback(_ context.Context, i delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error) {
	f.callbacks = append(f.callbacks, i)
	return f.task, nil
}

type fakeScale struct {
	observation   domain.Observation
	writes, reads int
	unknown       bool
	failReadback  bool
}

func (f *fakeScale) Observe(context.Context, domain.Slot) (domain.Observation, error) {
	f.reads++
	if f.failReadback && f.writes > 0 {
		return domain.Observation{}, errors.New("unknown")
	}
	return f.observation, nil
}
func (f *fakeScale) Scale(_ context.Context, _ domain.Slot, _ domain.Observation, n int32) error {
	f.writes++
	if f.unknown {
		return errors.New("transport lost")
	}
	f.observation.DesiredReplicas = n
	f.observation.Replicas = n
	f.observation.ReadyReplicas = n
	return nil
}
func TestWorkerUnknownDrainAndLateFenceNeverScale(t *testing.T) {
	for _, late := range []bool{false, true} {
		slot := testSlot()
		c := domain.Command{PoolID: "pool", Slot: slot, SlotHash: slot.Hash(), Revision: 1, OwnerEpoch: 1, Replicas: 0, Action: "stop", Deadline: time.Now().Add(time.Minute)}
		f := &fakeExecution{late: late, task: delivery.ExecutionTask{ID: "task", TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), Status: "running", ClaimedByAgentID: "edge", CallbackToken: "callback", Payload: map[string]any{"radiusDeployment": c}}}
		scale := &fakeScale{observation: domain.Observation{UID: slot.UID, TemplateDigest: slot.TemplateDigest, ResourceVersion: "1", Replicas: 1, DesiredReplicas: 1}}
		w, e := NewWorker(f, scale, "edge", []domain.Slot{slot})
		if e != nil {
			t.Fatal(e)
		}
		w.wait = func(context.Context, time.Duration) bool { return false }
		w.Execute(context.Background(), f.task)
		if scale.writes != 0 {
			t.Fatalf("late=%v wrote scale", late)
		}
	}
}
func TestWorkerScaleKnownZeroAndUnknownWriteReadback(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		slot := testSlot()
		c := domain.Command{PoolID: "pool", Slot: slot, SlotHash: slot.Hash(), Revision: 1, OwnerEpoch: 1, Replicas: 0, Action: "stop", Deadline: time.Now().Add(time.Minute)}
		zero := int64(0)
		f := &fakeExecution{drain: domain.DrainStatus{CountKnown: true, ActiveStates: &zero, ExpectedMembers: 1, ConfirmedMembers: 1, DrainProven: true}, task: delivery.ExecutionTask{ID: "task", TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), Status: "running", ClaimedByAgentID: "edge", CallbackToken: "callback", Payload: map[string]any{"radiusDeployment": c}}}
		scale := &fakeScale{unknown: unknown, observation: domain.Observation{UID: slot.UID, TemplateDigest: slot.TemplateDigest, ResourceVersion: "1", Replicas: 1, DesiredReplicas: 1}}
		w, e := NewWorker(f, scale, "edge", []domain.Slot{slot})
		if e != nil {
			t.Fatal(e)
		}
		w.wait = func(context.Context, time.Duration) bool { return false }
		w.Execute(context.Background(), f.task)
		if scale.writes != 1 || scale.reads < 3 {
			t.Fatalf("write/readback %d/%d", scale.writes, scale.reads)
		}
		last := f.callbacks[len(f.callbacks)-1]
		if unknown && last.Status != "failed" {
			t.Fatal("unknown scale result passed")
		}
		if !unknown && last.Status != "completed" {
			t.Fatal("known0 scale failed")
		}
	}
}
