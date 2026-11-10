package bootstrap

import (
	"context"

	appcluster "github.com/opensoha/soha/internal/application/cluster"
	appresource "github.com/opensoha/soha/internal/application/resource"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func currentClusterConfigResolver(connections appresource.ConnectionResolver) func(context.Context, string) (cfgpkg.ClusterConfig, error) {
	return func(ctx context.Context, id string) (cfgpkg.ClusterConfig, error) {
		connection, err := connections.GetConnection(ctx, id)
		if err != nil {
			return cfgpkg.ClusterConfig{}, err
		}
		if domaincluster.VerifyExecutionConnection(ctx, connection) != nil {
			return cfgpkg.ClusterConfig{}, apperrors.ErrConflict
		}
		config, err := appcluster.RuntimeClusterConfig(connection)
		if err != nil {
			return cfgpkg.ClusterConfig{}, err
		}
		return *config, nil
	}
}
