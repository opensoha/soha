package bootstrap

import (
	appaccess "github.com/opensoha/soha/internal/application/access"
	appdlp "github.com/opensoha/soha/internal/application/contentdlp"
	dlprepo "github.com/opensoha/soha/internal/repository/contentdlp"
	"gorm.io/gorm"
)

func newContentDLPService(db *gorm.DB, projects appdlp.Projects, scope appdlp.Scope, permissions *appaccess.PermissionResolver, link appdlp.NetworkLinkage, stepUp appdlp.StepUpVerifier) (*appdlp.Service, error) {
	store, err := dlprepo.New(db)
	if err != nil {
		return nil, err
	}
	return appdlp.New(store, projects, scope, permissions, appdlp.WithNetworkLinkage(link), appdlp.WithStepUp(stepUp))
}
