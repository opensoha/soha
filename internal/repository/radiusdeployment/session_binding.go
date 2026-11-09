package radiusdeployment

import (
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repoaccess "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
	"strings"
	"time"
)

// SessionNASBinding resolves a restriction command's current route. A managed
// route must still have a fresh, credential-bound native member receipt. The
// original enrollment/authorization never substitutes for current route proof.
func SessionNASBinding(tx *gorm.DB, runtime, nas, site string, now time.Time) (access.NASBinding, error) {
	var binding access.NASBinding
	var err error
	switch {
	case strings.HasPrefix(runtime, domain.PoolPrefix):
		return binding, apperrors.ErrUnauthorized
	case strings.HasPrefix(runtime, domain.MemberPrefix):
		binding, err = managedSessionNASBinding(tx, runtime, nas, now)
	default:
		binding, err = repoaccess.New(tx).FindActiveNASBinding(tx.Statement.Context, runtime, nas)
	}
	if err != nil {
		return binding, err
	}
	if binding.SiteID != site || binding.Status != access.StatusActive || binding.NASID != nas {
		return access.NASBinding{}, apperrors.ErrUnauthorized
	}
	return binding, nil
}
func managedSessionNASBinding(tx *gorm.DB, runtime, nas string, now time.Time) (access.NASBinding, error) {
	state, record, err := loadMember(tx, runtime)
	if err != nil {
		return access.NASBinding{}, err
	}
	if !record.Ready(state, now) {
		return access.NASBinding{}, apperrors.ErrUnauthorized
	}
	c := record.Control
	var fingerprint string
	if err := tx.Raw(`SELECT certificate_fingerprint FROM network_runtime_credentials WHERE id=? AND runtime_id=?`, c.CredentialID, runtime).Row().Scan(&fingerprint); err != nil {
		return access.NASBinding{}, err
	}
	proof := networkprotocol.ManagedNASProof{PoolID: c.PoolID, MemberID: c.MemberID, CredentialID: c.CredentialID, ProcessEpoch: c.ProcessEpoch, OwnerEpoch: int64(c.OwnerEpoch), ControlRevision: int64(c.ControlRevision), ConfigurationRevision: int64(c.ConfigurationRevision), ConfigurationHash: c.ConfigurationHash}
	return validateManagedNAS(tx, runtime, fingerprint, nas, &proof, state.SlotHash, true, now)
}
