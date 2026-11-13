package environment

import (
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestPersonalDockerDataDeletionReal(t *testing.T) {
	if os.Getenv("SOHA_TEST_PERSONAL_DOCKER") != "1" {
		t.Skip("requires owned disposable Docker validation")
	}
	executor, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
	if err != nil {
		t.Fatal(err)
	}
	request := personalDockerRequest("task48-delete-"+uuid.NewString(), "delete-"+uuid.NewString())
	handle, err := executor.Start(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupPersonalDockerFixtures(t, executor, []LocalRequest{request}, []Handle{handle}) })
	personalDockerCommand(t, executor, handle, "/bin/sh", "-ec", "printf retained > /data/note")
	t.Run("in_use_data_untouched", func(t *testing.T) {
		if err := executor.DeletePersonalData(t.Context(), request.Lease); err == nil {
			t.Fatal("deleted attached personal data")
		}
		if got := personalDockerCommand(t, executor, handle, "/bin/cat", "/data/note"); got != "retained" {
			t.Fatal("rejected deletion changed personal data")
		}
	})
	t.Run("foreign_owner_untouched", func(t *testing.T) {
		foreign := request.Lease
		foreign.OwnerID = "foreign-" + uuid.NewString()
		if err := executor.DeletePersonalData(t.Context(), foreign); err == nil {
			t.Fatal("borrowed another owner's data")
		}
		if got := personalDockerCommand(t, executor, handle, "/bin/cat", "/data/note"); got != "retained" {
			t.Fatal("foreign deletion affected original data")
		}
	})
	if err := executor.Stop(t.Context(), handle); err != nil {
		t.Fatal(err)
	}
	t.Run("partial_deletion_not_confirmed", func(t *testing.T) {
		if _, err := executor.quotaHelper(t.Context(), dockerPersonalVolume(request.Lease.OwnerID), "/bin/sh", "-ec", "touch /probe/.soha-personal-retired; test -e /probe/data/note"); err != nil {
			t.Fatal(err)
		}
		if retired, err := executor.personalDataRetired(t.Context(), request.Lease); err == nil || retired {
			t.Fatal("tombstone alone claimed physical file deletion")
		}
	})
	t.Run("delete_and_idempotent_tombstone", func(t *testing.T) {
		for range 2 {
			if err := executor.DeletePersonalData(t.Context(), request.Lease); err != nil {
				t.Fatal(err)
			}
		}
		retired, err := executor.personalDataRetired(t.Context(), request.Lease)
		if err != nil || !retired {
			t.Fatalf("deletion lost durable tombstone: %v", err)
		}
		if _, err := executor.quotaHelper(t.Context(), dockerPersonalVolume(request.Lease.OwnerID), "/bin/sh", "-ec", "test -f /probe/.soha-personal-retired; test ! -e /probe/data"); err != nil {
			t.Fatal("personal files were retained after deletion")
		}
	})
	t.Run("restart_and_delayed_creation_rejected", func(t *testing.T) {
		restarted, err := NewDockerExecutor(testPersonalImage, testQuotaImage)
		if err != nil {
			t.Fatal(err)
		}
		for _, existing := range []bool{false, true} {
			request.Spec.RequireExistingData = existing
			request.Lease.ID = "new-" + uuid.NewString()
			if _, err := restarted.Start(t.Context(), request); err == nil {
				t.Fatal("retired data was silently recreated")
			}
		}
	})
}
