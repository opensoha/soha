package bootstrap

import (
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/keyring"
	"testing"
	"time"
)

func TestPrivateDurableStorageRequiresExplicitFormatActivation(t *testing.T) {
	key, err := keyring.NewKey("storage-test", "task115-storage-key", time.Now().UTC(), nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	var cfg cfgpkg.Config
	cfg.Security.CredentialEncryptionKeys = keys
	if durableStorageKeys(cfg).Active().ID() != "" {
		t.Fatal("new format activated during an ordinary rolling upgrade")
	}
	cfg.Security.ProtectedDurableStorage = true
	if durableStorageKeys(cfg).Active().ID() != key.ID() {
		t.Fatal("explicit activation did not use persistent keyring")
	}
	cfg.Security.CredentialEncryptionKeys = keyring.Ring{}
	if durableStorageKeys(cfg).Active().ID() != "" {
		t.Fatal("missing key received a fallback")
	}
}
