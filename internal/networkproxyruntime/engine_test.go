package networkproxyruntime

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/platform/securefile"
)

func TestApplyRestoresFileAndStopsOldProcessOnFailure(t *testing.T) {
	for _, scenario := range []struct{ name, reason string }{
		{"start failure", "start_failed"}, {"readback changed", "readback_failed"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			config, probes := lifecycleFixture(t, scenario.name)
			engine := newEngine(config, "a-secret-for-tests")
			defer engine.stop()
			if result := engine.apply(context.Background(), lifecycleDesired(1, 7890)); result.Status != "applied" {
				t.Fatalf("initial guarded configuration failed: %+v", result)
			}
			old, err := securefile.Read(config.ConfigFile, 2<<20, true)
			if err != nil {
				t.Fatal(err)
			}
			result := engine.apply(context.Background(), lifecycleDesired(2, 7891))
			if result.Status != "rolled-back" || result.ReasonCode != scenario.reason {
				t.Fatalf("apply result = %+v", result)
			}
			if probes.Load() == 0 {
				t.Fatal("replacement was never probed")
			}
			got, err := securefile.Read(config.ConfigFile, 2<<20, true)
			if err != nil || string(got) != string(old) {
				t.Fatalf("restored file = %q, %v", got, err)
			}
			if engine.process != nil || engine.egress != nil {
				t.Fatal("rejected replacement resumed an old process or guard")
			}
			if err := engine.health(context.Background()); err == nil {
				t.Fatal("stopped predecessor reported healthy")
			}
		})
	}
}

func TestRunnerSuspendsExitedEngineUntilFreshConfiguration(t *testing.T) {
	config, _ := lifecycleFixture(t, "")
	runner := &Runner{config: config, engine: newEngine(config, "a-secret-for-tests")}
	defer runner.engine.stop()
	runner.applyConfiguration(context.Background(), lifecycleDesired(1, 7890))
	if runner.engine.process == nil || !runner.enabled {
		t.Fatal("verified engine did not start")
	}
	firstPID := runner.engine.process.Process.Pid
	if err := runner.engine.process.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-runner.engine.wait:
		runner.engine.wait <- err
	case <-time.After(5 * time.Second):
		t.Fatal("engine did not exit")
	}
	runner.ensureRunning(context.Background())
	if runner.enabled || !runner.suspended || runner.engine.process != nil || runner.reasonCode != "engine_state_drift" {
		t.Fatal("crashed engine restarted without configuration revalidation")
	}
	runner.applyConfiguration(context.Background(), lifecycleDesired(2, 7890))
	if !runner.enabled || runner.suspended || runner.engine.process == nil || runner.engine.process.Process.Pid == firstPID {
		t.Fatal("fresh validated configuration did not recover the engine")
	}
}

func TestEngineCannotStartWithoutActiveEgressGuard(t *testing.T) {
	config, _ := lifecycleFixture(t, "")
	engine := newEngine(config, "a-secret-for-tests")
	defer engine.stop()
	if err := engine.start(context.Background()); err == nil || engine.process != nil {
		t.Fatal("unguarded engine started", err)
	}
	if _, err := os.Stat(config.ConfigFile); !os.IsNotExist(err) {
		t.Fatal("unguarded start wrote a configuration")
	}
}
