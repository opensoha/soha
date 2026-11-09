package networkproxy

import (
	"context"
	"errors"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"testing"
	"time"
)

type publicationRuntimeStore struct {
	RuntimeStore
	item         domain.Instance
	prepared     bool
	grantedUntil time.Time
	grantErr     error
}

func (s *publicationRuntimeStore) Get(context.Context, string) (domain.Instance, error) {
	return s.item, nil
}
func (s *publicationRuntimeStore) Touch(context.Context, string, time.Time) error { return nil }
func (s *publicationRuntimeStore) PrepareConfiguration(context.Context, string, time.Time) (domain.Instance, error) {
	s.prepared = true
	return s.item, nil
}

func (s *publicationRuntimeStore) GrantConfigurationLease(_ context.Context, _ string, _ int64, _ string, until, _ time.Time) error {
	s.grantedUntil = until
	return s.grantErr
}

type publicationCredentialStore struct{ credential runtime.Credential }

func (s publicationCredentialStore) ActiveCredential(context.Context, string, time.Time) (runtime.Credential, error) {
	return s.credential, nil
}

type testPublicationReader struct {
	publication api.NetworkProxyServerPublication
	err         error
	called      bool
	leaseUntil  time.Time
}

func (s *testPublicationReader) ServerPublication(_ context.Context, instance, bundle string, revision int64, leaseUntil time.Time) (api.NetworkProxyServerPublication, error) {
	s.called = true
	s.leaseUntil = leaseUntil
	if instance != "proxy-1" || bundle != "bundle-1" || revision != 1 {
		return api.NetworkProxyServerPublication{}, errors.New("wrong publication request")
	}
	return s.publication, s.err
}
func TestRuntimePublicationIsAuthenticatedBoundAndLeaseLimited(t *testing.T) {
	now := time.Now().UTC()
	key, err := keyring.NewKey("test", "publication-test-key", now, nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := secretcrypto.EncryptStringWithKeyring(keys, "content")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"legacy", "valid", "missing reader", "reader failure", "wrong bundle", "wrong revision", "wrong hash", "expired", "wrong identity", "grant CAS conflict"} {
		t.Run(name, func(t *testing.T) {
			item := domain.Instance{ID: "proxy-1", BundleID: "bundle-1", BundleRevision: 1, DesiredRevision: 4, DesiredHash: "hash-1", DesiredContentEncrypted: encrypted}
			if name == "legacy" {
				item.BundleID = ""
			}
			store := &publicationRuntimeStore{item: item}
			if name == "grant CAS conflict" {
				store.grantErr = apperrors.ErrConflict
			}
			credentials := publicationCredentialStore{runtime.Credential{RuntimeID: "proxy-1", RuntimeKind: "proxy", CertificateFingerprint: "fp-1", Capabilities: []string{"proxy-policy-v1", "proxy-guarded-dns-v1"}, ExpiresAt: now.Add(time.Minute)}}
			service, err := NewRuntime(store, credentials, keys)
			if err != nil {
				t.Fatal(err)
			}
			service.now = func() time.Time { return now }
			reader := &testPublicationReader{publication: api.NetworkProxyServerPublication{BundleID: "bundle-1", Revision: 1, ServerHash: "hash-1", ValidUntil: now.Add(3 * time.Minute)}}
			switch name {
			case "reader failure":
				reader.err = errors.New("unavailable")
			case "wrong bundle":
				reader.publication.BundleID = "other"
			case "wrong revision":
				reader.publication.Revision = 2
			case "wrong hash":
				reader.publication.ServerHash = "other"
			case "expired":
				reader.publication.ValidUntil = now
			}
			if name != "missing reader" && name != "legacy" {
				service.SetPublicationReader(reader)
			}
			id := networkidentity.Identity{Scope: networkidentity.ScopeNetworkControl, Kind: "proxy", ID: "proxy-1", CertificateFingerprint: "fp-1"}
			if name == "wrong identity" {
				id.ID = "foreign"
			}
			config, err := service.Configuration(context.Background(), id)
			switch name {
			case "legacy":
				if err != nil || config == nil || config.Publication != nil {
					t.Fatalf("legacy config=%+v err=%v", config, err)
				}
			case "valid":
				if err != nil || config.Publication == nil || !config.ValidUntil.Equal(now.Add(time.Minute)) || !config.Publication.ValidUntil.Equal(config.ValidUntil) || !store.prepared || !reader.leaseUntil.Equal(now.Add(time.Minute)) || !store.grantedUntil.Equal(config.ValidUntil) {
					t.Fatalf("config=%+v err=%v", config, err)
				}
			case "grant CAS conflict":
				if !errors.Is(err, apperrors.ErrConflict) || config != nil {
					t.Fatalf("grant conflict config=%+v err=%v", config, err)
				}
			case "wrong identity":
				if !errors.Is(err, apperrors.ErrUnauthorized) || reader.called {
					t.Fatalf("auth error=%v reader called=%v", err, reader.called)
				}
			default:
				if !errors.Is(err, apperrors.ErrServiceUnavailable) || config != nil {
					t.Fatalf("config=%+v err=%v", config, err)
				}
			}
		})
	}
}
