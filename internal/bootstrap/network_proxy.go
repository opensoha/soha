package bootstrap

import (
	"fmt"

	apiHandlers "github.com/opensoha/soha/internal/api/handlers"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appaudit "github.com/opensoha/soha/internal/application/audit"
	appnetworkproxy "github.com/opensoha/soha/internal/application/networkproxy"
	appoperation "github.com/opensoha/soha/internal/application/operation"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

func newNetworkProxyServices(cfg cfgpkg.Config, repos *repositories, permissions *appaccess.PermissionResolver, scope *appaccess.Service, audit *appaudit.Service, operations *appoperation.Service) (apiHandlers.NetworkTelemetryService, *appnetworkproxy.Management, error) {
	query, err := buildNetworkIngestQuery(cfg)
	if err != nil {
		return nil, nil, err
	}
	var traffic appnetworkproxy.TrafficReader
	if reader, ok := query.(appnetworkproxy.TrafficReader); ok {
		traffic = reader
	}
	management, err := appnetworkproxy.NewManagement(repos.networkProxyRepository, traffic, permissions, scope, audit, operations, cfg.Security.CredentialEncryptionKeys)
	if err != nil {
		return nil, nil, fmt.Errorf("build proxy management service: %w", err)
	}
	return query, management, nil
}
