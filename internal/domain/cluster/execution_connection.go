package cluster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

var ErrExecutionConnectionChanged = errors.New("execution cluster connection changed")

type executionConnectionKey struct{}
type executionConnection struct{ ID, Fingerprint string }

// RuntimeConnectionFingerprint excludes changing health/inventory projections.
// It binds the configured destination and credentials, without storing them.
func RuntimeConnectionFingerprint(connection Connection) (string, error) {
	mode := connection.Summary.ConnectionMode
	if mode == "" {
		mode = ConnectionModeDirectKubeconfig
	}
	raw, err := json.Marshal(struct {
		ID                                    string
		Mode                                  ConnectionMode
		CredentialType, SourceType, SourceRef string
		Metadata                              map[string]any
	}{connection.Summary.ID, mode, connection.CredentialType, connection.SourceType, connection.SourceRef, connection.Metadata})
	if err != nil || len(raw) > 8<<20 {
		return "", ErrExecutionConnectionChanged
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func WithExpectedExecutionConnection(ctx context.Context, id, fingerprint string) context.Context {
	return context.WithValue(ctx, executionConnectionKey{}, executionConnection{id, fingerprint})
}

func HasExpectedExecutionConnection(ctx context.Context) bool {
	_, ok := ctx.Value(executionConnectionKey{}).(executionConnection)
	return ok
}

// Verify at the resolution that supplies the actual transport configuration.
// A separate earlier authorization lookup alone is not a destination fence.
func VerifyExecutionConnection(ctx context.Context, connection Connection) error {
	expected, ok := ctx.Value(executionConnectionKey{}).(executionConnection)
	if !ok {
		return nil
	}
	fingerprint, err := RuntimeConnectionFingerprint(connection)
	if err != nil || expected.ID != connection.Summary.ID || expected.Fingerprint == "" || expected.Fingerprint != fingerprint {
		return ErrExecutionConnectionChanged
	}
	return nil
}
