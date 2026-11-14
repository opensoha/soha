package environment

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPersonalDockerDataFencingReal(t *testing.T) {
	if os.Getenv("SOHA_TEST_PERSONAL_DOCKER") != "1" {
		t.Skip("requires owned disposable Docker validation")
	}
	t.Run("cross_Agent_flock", testPersonalDataFencing)
	t.Run("late_container_start", testPersonalDataLateContainerStart)
	t.Run("expires_during_start", testPersonalDataExpiryDuringStart)
}

func testPersonalDataExpiryDuringStart(t *testing.T) {
	executor, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
	if err != nil {
		t.Fatal(err)
	}
	request := personalDockerRequest("task48-start-expiry-"+uuid.NewString(), "lease-"+uuid.NewString())
	request.Lease.ExpiresAt = time.Now().Add(5 * time.Second).UTC()
	var handle Handle
	t.Cleanup(func() { cleanupPersonalDockerFixtures(t, executor, []LocalRequest{request}, []Handle{handle}) })
	run := executor.run
	executor.run = func(ctx context.Context, args ...string) ([]byte, error) {
		if args[0] == "run" && slices.Contains(args, "--detach") {
			timer := time.NewTimer(time.Until(request.Lease.ExpiresAt.Add(time.Second)))
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return run(ctx, args...)
	}
	_, startErr := executor.Start(t.Context(), request)
	executor.run = run
	var found bool
	handle, found, err = executor.personalHandle(t.Context(), request.Lease)
	if err != nil || !found {
		t.Fatalf("original expired container receipt unavailable: %v", err)
	}
	if startErr == nil {
		t.Fatal("startup delay extended the original lease")
	}
	state, err := executor.run(t.Context(), "container", "inspect", "--format", "{{.State.Running}}", handle.ID)
	if err != nil || strings.TrimSpace(string(state)) != "false" {
		t.Fatal("expired supervisor retained its physical lock")
	}
}

func testPersonalDataLateContainerStart(t *testing.T) {
	for _, deletionFirst := range []bool{false, true} {
		executor, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
		if err != nil {
			t.Fatal(err)
		}
		request := personalDockerRequest("task48-start-delete-"+uuid.NewString(), "lease-"+uuid.NewString())
		handle, err := executor.Start(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cleanupPersonalDockerFixtures(t, executor, []LocalRequest{request}, []Handle{handle}) })
		personalDockerCommand(t, executor, handle, "/bin/sh", "-ec", "printf retained > /data/note")
		if err := executor.Stop(t.Context(), handle); err != nil {
			t.Fatal(err)
		}
		deleter, _ := NewDockerExecutor(testPersonalImage, testQuotaImage)
		starter, _ := NewDockerExecutor(testPersonalImage, testQuotaImage)
		ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
		defer cancel()
		request.Lease.ID = "late-" + uuid.NewString()
		request.Spec.RequireExistingData = true
		if deletionFirst {
			ready, proceed := make(chan struct{}), make(chan struct{})
			run := starter.run
			starter.run = func(ctx context.Context, args ...string) ([]byte, error) {
				if args[0] == "run" && slices.Contains(args, "--detach") {
					close(ready)
					select {
					case <-proceed:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				return run(ctx, args...)
			}
			started := make(chan error, 1)
			go func() { _, err := starter.Start(ctx, request); started <- err }()
			<-ready
			err := deleter.DeletePersonalData(ctx, request.Lease)
			close(proceed)
			if err != nil || <-started == nil {
				t.Fatal("late container recreated deleted data")
			}
			late, found, err := starter.personalHandle(ctx, request.Lease)
			if err != nil {
				t.Fatal(err)
			}
			if found {
				if err := starter.Stop(ctx, late); err != nil {
					t.Fatal(err)
				}
			}
		} else {
			ready, proceed := make(chan struct{}), make(chan struct{})
			deferPersonalDataDeletion(deleter, ready, proceed)
			deleted := make(chan error, 1)
			go func() { deleted <- deleter.DeletePersonalData(ctx, request.Lease) }()
			<-ready
			late, err := starter.Start(ctx, request)
			close(proceed)
			if err != nil {
				t.Fatal(err)
			}
			if err := <-deleted; err == nil {
				t.Fatal("running container was reported deleted")
			}
			personalDockerCommand(t, starter, late, "/bin/sh", "-ec", "test \"$(cat /data/note)\" = retained")
			if _, err := executor.quotaHelper(ctx, dockerPersonalVolume(request.Lease.OwnerID), "/bin/sh", "-ec", "test ! -e /probe/.soha-personal-retired; test -e /probe/data/note"); err != nil {
				t.Fatal("late container start did not prevent data deletion")
			}
			if err := starter.Stop(ctx, late); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func testPersonalDataFencing(t *testing.T) {
	t.Helper()
	executor, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
	if err != nil {
		t.Fatal(err)
	}
	request := personalDockerRequest("task48-data-fence-"+uuid.NewString(), "fence-"+uuid.NewString())
	handle, err := executor.Start(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupPersonalDockerFixtures(t, executor, []LocalRequest{request}, []Handle{handle}) })
	personalDockerCommand(t, executor, handle, "/bin/sh", "-ec", "printf retained > /data/note")
	if err := executor.Stop(t.Context(), handle); err != nil {
		t.Fatal(err)
	}
	deleter, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
	if err != nil {
		t.Fatal(err)
	}
	preparer, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	ready, proceed := make(chan struct{}), make(chan struct{})
	allowDelete := false
	defer func() {
		if !allowDelete {
			close(proceed)
		}
	}()
	deferPersonalDataDeletion(deleter, ready, proceed)
	deleted := make(chan error, 1)
	go func() { deleted <- deleter.DeletePersonalData(ctx, request.Lease) }()
	select {
	case <-ready: // Delete's initial no-container check has already passed.
	case <-ctx.Done():
		t.Fatal("deletion did not reach its helper")
	}
	pausePersonalDataPreparation(preparer)
	prepared := make(chan error, 1)
	volume := dockerPersonalVolume(request.Lease.OwnerID)
	go func() {
		_, err := preparer.prepareVolumeQuota(ctx, request.Lease, volume, "33554432", true)
		prepared <- err
	}()
	waitPersonalDataLock(t, executor, ctx, volume)
	close(proceed)
	allowDelete = true
	if err := <-deleted; err == nil {
		t.Fatal("deletion passed another Agent's in-flight data mutation")
	}
	deleter.run = runEnvironmentDocker
	if _, err := executor.quotaHelper(ctx, volume, "/bin/sh", "-ec", "touch /probe/.soha-lock-release"); err != nil {
		t.Fatal(err)
	}
	if err := <-prepared; err != nil {
		t.Fatal(err)
	}
	if _, err := executor.quotaHelper(ctx, volume, "/bin/sh", "-ec", "test ! -e /probe/.soha-personal-retired; test \"$(cat /probe/data/note)\" = retained"); err != nil {
		t.Fatal("lock conflict changed personal data")
	}
	if err := deleter.DeletePersonalData(ctx, request.Lease); err != nil {
		t.Fatal("explicit deletion failed after the other mutation ended")
	}
}

func deferPersonalDataDeletion(executor *DockerExecutor, ready chan<- struct{}, proceed <-chan struct{}) {
	run := executor.run
	executor.run = func(ctx context.Context, args ...string) ([]byte, error) {
		if args[0] == "run" && slices.ContainsFunc(args, func(arg string) bool { return strings.Contains(arg, "touch /probe/.soha-personal-retired") }) {
			close(ready)
			select {
			case <-proceed:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return run(ctx, args...)
	}
}

func pausePersonalDataPreparation(executor *DockerExecutor) {
	run := executor.run
	executor.run = func(ctx context.Context, args ...string) ([]byte, error) {
		args = slices.Clone(args)
		for i, arg := range args {
			if strings.Contains(arg, "btrfs qgroup limit") {
				args[i] = strings.Replace(arg, `flock "$lock_mode" -n 9 || exit 1`, `flock "$lock_mode" -n 9 || exit 1
touch /probe/.soha-lock-ready
while [ ! -e /probe/.soha-lock-release ]; do sleep 0.1; done`, 1)
			}
		}
		return run(ctx, args...)
	}
}

func waitPersonalDataLock(t *testing.T, executor *DockerExecutor, ctx context.Context, volume string) {
	t.Helper()
	for ctx.Err() == nil {
		data, err := executor.quotaHelper(ctx, volume, "/bin/sh", "-ec", "if [ -e /probe/.soha-lock-ready ]; then printf ready; fi")
		if err != nil {
			t.Fatal(err)
		}
		if string(data) == "ready" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("another Agent did not acquire the real data lock")
}
