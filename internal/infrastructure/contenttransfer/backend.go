package contenttransfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"

	sdk "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	app "github.com/opensoha/soha/internal/application/contenttransfer"
	d "github.com/opensoha/soha/internal/domain/contenttransfer"
	"github.com/opensoha/soha/internal/domain/identity"
	secret "github.com/opensoha/soha/internal/domain/secret"
	storage "github.com/opensoha/soha/internal/infrastructure/softwareobjectstore"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Secrets interface {
	PinReferences(context.Context, identity.Principal, map[string]string, secret.Target) ([]secret.Reference, error)
	ResolvePinnedReferences(context.Context, identity.Principal, []secret.Reference, secret.Target) (map[string]string, error)
}
type ConnectionViews interface {
	Get(context.Context, identity.Principal, string) (sdk.SystemIntegration, error)
}
type Factory struct {
	Connections ConnectionViews
	Storage     *storage.Provider
	Secrets     Secrets
}

func (f *Factory) Prepare(ctx context.Context, p identity.Principal, dest d.Destination) (app.Backend, error) {
	if f == nil || f.Storage == nil || f.Secrets == nil || dest.Published == nil {
		return nil, apperrors.ErrServiceUnavailable
	}
	if err := app.ValidateConfig(*dest.Published); err != nil {
		return nil, err
	}
	if dest.Published.Kind == "s3" {
		store, err := f.Storage.PrepareManaged(ctx, dest.Published.StorageIntegrationID)
		if err != nil {
			return nil, apperrors.ErrServiceUnavailable
		}
		return &objectBackend{store: store, destination: dest.ID}, nil
	}
	return f.webdav(ctx, p, dest)
}

type objectBackend struct {
	store       *storage.ManagedSnapshot
	destination string
}

func (b *objectBackend) Identity() string         { return b.store.ManagedIdentity() }
func (b *objectBackend) SnapshotIdentity() string { return b.store.SnapshotIdentity() }
func validObjectKey(key, destination string) bool {
	parts := strings.Split(key, "/")
	if len(parts) != 4 || parts[0] != "content-transfers" || parts[1] != destination || len(parts[2]) != 64 || len(parts[3]) != 36 {
		return false
	}
	_, err := hex.DecodeString(parts[2])
	return err == nil && !strings.ContainsAny(parts[3], ".\\%?#")
}
func (b *objectBackend) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if !validObjectKey(key, b.destination) {
		return nil, apperrors.ErrInvalidArgument
	}
	return b.store.Open(ctx, key)
}
func (b *objectBackend) Put(ctx context.Context, key string, content []byte) error {
	if !validObjectKey(key, b.destination) {
		return apperrors.ErrInvalidArgument
	}
	return b.store.PutManaged(ctx, key, content)
}
func (b *objectBackend) Delete(ctx context.Context, key string) error {
	if !validObjectKey(key, b.destination) {
		return apperrors.ErrInvalidArgument
	}
	return b.store.Delete(ctx, key)
}
func backendIdentity(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (f *Factory) Validate(ctx context.Context, p identity.Principal, dest d.Destination) error {
	if f == nil || f.Connections == nil || dest.Published == nil {
		return apperrors.ErrServiceUnavailable
	}
	if dest.Published.Kind == "s3" {
		item, err := f.Connections.Get(ctx, p, dest.Published.StorageIntegrationID)
		if err != nil {
			return err
		}
		if item.ProviderType != "s3" || item.Category != "storage" || !item.Enabled {
			return apperrors.ErrAccessDenied
		}
	}
	return nil
}
