package networkruntime

import (
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	networkaccessrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
	"slices"
)

func mihomoProtectionCurrent(tx *gorm.DB, configuration *networkprotocol.MihomoConfiguration, snapshot domainnetworkruntime.PolicySnapshot) (bool, error) {
	if configuration == nil {
		return true, nil
	}
	if configuration.Protection == nil || configuration.Protection.PolicyVersion != snapshot.PolicyVersion {
		return false, nil
	}
	expected, err := compileMihomoProtection(tx, snapshot)
	if err != nil {
		return false, err
	}
	return slices.Equal(configuration.Protection.CIDRs, expected.CIDRs) && slices.Equal(configuration.Protection.Hosts, expected.Hosts), nil
}

func compileMihomoProtection(tx *gorm.DB, snapshot domainnetworkruntime.PolicySnapshot) (*networkprotocol.MihomoProtection, error) {
	protection, err := networkaccessrepo.New(tx).CompileProtectedDestinations(tx.Statement.Context, snapshot.PolicyVersion, snapshot.ProtectedResourceIDs)
	if err != nil {
		return nil, err
	}
	return &networkprotocol.MihomoProtection{PolicyVersion: protection.PolicyVersion, CIDRs: protection.CIDRs, Hosts: protection.Hosts}, nil
}

func withMihomoProtection(tx *gorm.DB, desired networkprotocol.ConfigurationDesired, snapshot domainnetworkruntime.PolicySnapshot) (networkprotocol.ConfigurationDesired, error) {
	if desired.Mihomo == nil {
		return desired, nil
	}
	protection, err := compileMihomoProtection(tx, snapshot)
	if err != nil {
		return desired, err
	}
	desired.Mihomo.Protection = protection
	return desired, nil
}
