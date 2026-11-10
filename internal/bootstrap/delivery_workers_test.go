package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type deliveryWorkerProbe struct {
	validationError error
	started         int
	ctx             context.Context
}

func (p *deliveryWorkerProbe) Start(ctx context.Context)             { p.started++; p.ctx = ctx }
func (p *deliveryWorkerProbe) StartHelmDelivery(ctx context.Context) { p.Start(ctx) }
func (p *deliveryWorkerProbe) ValidateRuntimeDependencies() error    { return p.validationError }
func (p *deliveryWorkerProbe) ValidateDeliveryRuntime() error        { return p.validationError }

func TestDeliveryWorkersValidateEntireCohortBeforeStarting(t *testing.T) {
	for _, test := range []struct {
		name       string
		invalidate func(*deliveryWorkers, []*deliveryWorkerProbe)
		wantError  string
	}{
		{"missing late worker", func(w *deliveryWorkers, _ []*deliveryWorkerProbe) { w.updates = nil }, "template updates"},
		{"typed nil late worker", func(w *deliveryWorkers, _ []*deliveryWorkerProbe) { w.updates = (*deliveryWorkerProbe)(nil) }, "template updates"},
		{"missing runtime binding", func(_ *deliveryWorkers, p []*deliveryWorkerProbe) {
			p[1].validationError = errors.New("missing template updates binding")
		}, "missing template updates binding"},
		{"missing workflow binding", func(_ *deliveryWorkers, p []*deliveryWorkerProbe) {
			p[2].validationError = errors.New("missing delivery runtime")
		}, "missing delivery runtime"},
	} {
		t.Run(test.name, func(t *testing.T) {
			probes := []*deliveryWorkerProbe{{}, {}, {}, {}, {}, {}}
			workers := deliveryWorkers{execution: probes[5], manifests: probes[0], helms: probes[1], workflows: probes[2], triggers: probes[3], updates: probes[4]}
			test.invalidate(&workers, probes)
			if err := workers.start(context.Background()); err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("start error = %v, want %q", err, test.wantError)
			}
			for i, probe := range probes {
				if probe.started != 0 {
					t.Fatalf("worker %d started before the cohort was ready", i)
				}
			}
		})
	}
}

func TestDeliveryWorkersStartWithSharedLifecycleAfterBinding(t *testing.T) {
	probes := []*deliveryWorkerProbe{{}, {}, {}, {}, {}, {}}
	workers := deliveryWorkers{execution: probes[5], manifests: probes[0], helms: probes[1], workflows: probes[2], triggers: probes[3], updates: probes[4]}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := workers.start(ctx); err != nil {
		t.Fatal(err)
	}
	for i, probe := range probes {
		if probe.started != 1 || probe.ctx != ctx {
			t.Fatalf("worker %d did not start exactly once with shared lifecycle", i)
		}
	}
	cancel()
	for i, probe := range probes {
		if probe.ctx.Err() != context.Canceled {
			t.Fatalf("worker %d lost lifecycle cancellation", i)
		}
	}
}
