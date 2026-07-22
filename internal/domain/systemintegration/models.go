package systemintegration

import (
	"context"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

const (
	CategorySourceControl = "source_control"
	ProviderGitLab        = "gitlab"
	HealthUnknown         = "unknown"
	HealthHealthy         = "healthy"
	HealthUnhealthy       = "unhealthy"
)

type Integration struct {
	ID             string
	Category       string
	ProviderType   string
	Name           string
	Description    string
	Enabled        bool
	Configuration  []sohaapi.SystemIntegrationConfigurationField
	CredentialKeys []string
	HealthStatus   string
	LastCheckedAt  *time.Time
	LastError      string
	Version        int64
	CreatedBy      string
	UpdatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Filter struct {
	Category     string
	ProviderType string
	Enabled      *bool
}

type UpdateInput struct {
	ExpectedVersion     int64
	Name                *string
	Description         *string
	Enabled             *bool
	Configuration       *[]sohaapi.SystemIntegrationConfigurationField
	Credentials         []sohaapi.SystemIntegrationCredentialInput
	ClearCredentialKeys []string
}

type Repository interface {
	List(context.Context, Filter) ([]Integration, error)
	Get(context.Context, string) (Integration, error)
	Create(context.Context, Integration, map[string]string) (Integration, error)
	Update(context.Context, Integration, int64, map[string]string, []string) (Integration, error)
	Delete(context.Context, string) error
	Credentials(context.Context, string) (map[string]string, error)
	UpdateHealth(context.Context, string, string, string, time.Time) error
}
