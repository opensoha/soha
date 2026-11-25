package systemintegration

import (
	"errors"
	"strings"
	"testing"

	"github.com/opensoha/soha/contracts/plugins/storagedriver"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/stretchr/testify/require"
)

func TestManagedStorageDriverPublicationAndStaleHandles(t *testing.T) {
	const driverID = "example.blob"
	registry := NewStorageDriverRegistry(nil)
	owner := registry.ManagedDrivers()
	first, second := &captureDriver{}, &captureDriver{}
	schema := []byte(testStorageSchema)
	nextSchema := []byte(strings.Replace(testStorageSchema, "^[a-z]+$", "^[A-Z]+$", 1))
	commit := func() error { return nil }
	require.NoError(t, owner.Switch(driverID, "Blob v1", "1.0.0", schema, first, commit))
	old, exists := registry.lookup(driverID)
	require.True(t, exists)

	conflict := errors.New("candidate compare-and-swap failed")
	require.ErrorIs(t, owner.Switch(driverID, "Blob v2", "2.0.0", nextSchema, second, func() error {
		return conflict
	}), conflict)
	require.Equal(t, "1.0.0", owner.ActiveVersion(driverID))

	waiting, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- owner.Switch(driverID, "Blob v2", "2.0.0", nextSchema, second, func() error {
			close(waiting)
			<-release
			return nil
		})
	}()
	<-waiting
	require.NoError(t, old.driver.TestConnection(t.Context(), storagedriver.Binding{}))
	require.Equal(t, "Blob v1", registry.descriptors()[0].Name)
	require.Equal(t, "1.0.0", owner.ActiveVersion(driverID))
	close(release)
	require.NoError(t, <-done)

	current, exists := registry.lookup(driverID)
	require.True(t, exists)
	require.Equal(t, "Blob v2", current.name)
	require.Equal(t, "2.0.0", current.version)
	require.NotEqual(t, old.schema, current.schema)
	require.NoError(t, current.driver.TestConnection(t.Context(), storagedriver.Binding{}))
	require.ErrorIs(t, old.driver.TestConnection(t.Context(), storagedriver.Binding{}), storagedriver.RPCError{Code: storagedriver.CodeUnavailable})
	require.Equal(t, 1, first.calls)
	require.Equal(t, 1, second.calls)

	require.ErrorIs(t, registry.ManagedDrivers().Remove(driverID, commit), apperrors.ErrConflict)
	require.NoError(t, owner.Remove(driverID, commit))
	require.Empty(t, registry.descriptors())
	require.ErrorIs(t, current.driver.TestConnection(t.Context(), storagedriver.Binding{}), storagedriver.RPCError{Code: storagedriver.CodeUnavailable})

	require.NoError(t, registry.RegisterPlugin(driverID, "Static Blob", schema, first))
	require.ErrorIs(t, owner.Switch(driverID, "Blob v2", "2.0.0", schema, second, commit), apperrors.ErrConflict)
	require.ErrorIs(t, owner.Remove(driverID, commit), apperrors.ErrConflict)
}
