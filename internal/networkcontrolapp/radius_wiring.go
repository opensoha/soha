package networkcontrolapp

import (
	"context"
	control "github.com/opensoha/soha/internal/application/networkcontrol"
	app "github.com/opensoha/soha/internal/application/radiusdeployment"
	"github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	runtimeRepo "github.com/opensoha/soha/internal/repository/networkruntime"
	repo "github.com/opensoha/soha/internal/repository/radiusdeployment"
	"gorm.io/gorm"
)

type radiusRuntimeOnlyConsole struct{}

func (radiusRuntimeOnlyConsole) AuthorizeRADIUS(context.Context, identity.Principal, string, string, string) error {
	return apperrors.ErrAccessDenied
}
func newRadiusControlServices(db *gorm.DB, c config.NetworkControlConfig) (*app.Service, error) {
	slots := []domain.Slot{}
	for _, s := range c.RadiusSlots {
		slots = append(slots, s.Slot())
	}
	return app.New(repo.New(db), radiusRuntimeOnlyConsole{}, slots)
}

func newRadiusNetworkService(db *gorm.DB, c config.NetworkControlConfig, repo *runtimeRepo.Repository, schemas *networkprotocol.Schemas, options control.Options) (*control.Service, *app.Service, error) {
	radius, err := newRadiusControlServices(db, c)
	if err != nil {
		return nil, nil, err
	}
	options.ManagedNAS = radius
	service, err := control.New(repo, schemas, options)
	return service, radius, err
}
