package bootstrap

import (
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/keyring"
)

// The new format is opt-in only after every worker understands it. Old workers
// cannot safely recover an encrypted Native checkpoint during a rolling upgrade.
// Admission additionally requires an owned task and a recoverable capability.
func durableStorageKeys(cfg cfgpkg.Config) keyring.Ring {
	if cfg.Security.ProtectedDurableStorage {
		return cfg.Security.CredentialEncryptionKeys
	}
	return keyring.Ring{}
}
