package providerportal

import (
	"context"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

const (
	ProviderTypeLink  = "link"
	ProviderTypeOIDC  = "oidc"
	ProviderTypeProxy = "proxy"

	ApplicationStatusDraft       = "draft"
	ApplicationStatusEnabled     = "enabled"
	ApplicationStatusDisabled    = "disabled"
	ApplicationStatusMaintenance = "maintenance"

	AssignmentSubjectUser = "user"
	AssignmentSubjectRole = "role"
	AssignmentSubjectTeam = "team"
	AssignmentSubjectTag  = "tag"

	AssignmentEffectAllow = "allow"
)

type Application struct {
	ID             string                  `json:"id"`
	Slug           string                  `json:"slug"`
	Name           string                  `json:"name"`
	Description    string                  `json:"description,omitempty"`
	IconURL        string                  `json:"iconUrl,omitempty"`
	Category       string                  `json:"category,omitempty"`
	Tags           []string                `json:"tags"`
	LaunchURL      string                  `json:"launchUrl,omitempty"`
	ProviderID     string                  `json:"providerId,omitempty"`
	ProviderType   string                  `json:"providerType"`
	PortalVisible  bool                    `json:"portalVisible"`
	Featured       bool                    `json:"featured"`
	SortOrder      int                     `json:"sortOrder"`
	Status         string                  `json:"status"`
	Metadata       map[string]any          `json:"metadata,omitempty"`
	Assignments    []ApplicationAssignment `json:"assignments,omitempty"`
	Favorite       bool                    `json:"favorite,omitempty"`
	LastLaunchedAt *time.Time              `json:"lastLaunchedAt,omitempty"`
	CreatedBy      string                  `json:"createdBy,omitempty"`
	UpdatedBy      string                  `json:"updatedBy,omitempty"`
	CreatedAt      time.Time               `json:"createdAt"`
	UpdatedAt      time.Time               `json:"updatedAt"`
}

type ApplicationInput struct {
	Slug          string                       `json:"slug"`
	Name          string                       `json:"name"`
	Description   string                       `json:"description"`
	IconURL       string                       `json:"iconUrl"`
	Category      string                       `json:"category"`
	Tags          []string                     `json:"tags"`
	LaunchURL     string                       `json:"launchUrl"`
	ProviderID    string                       `json:"providerId"`
	ProviderType  string                       `json:"providerType"`
	PortalVisible bool                         `json:"portalVisible"`
	Featured      bool                         `json:"featured"`
	SortOrder     int                          `json:"sortOrder"`
	Status        string                       `json:"status"`
	Metadata      map[string]any               `json:"metadata"`
	Assignments   []ApplicationAssignmentInput `json:"assignments"`
}

type ApplicationFilter struct {
	Query  string
	Status string
	Limit  int
	Offset int
}

type ApplicationAssignment struct {
	ID            string    `json:"id"`
	ApplicationID string    `json:"applicationId"`
	SubjectType   string    `json:"subjectType"`
	SubjectID     string    `json:"subjectId"`
	Effect        string    `json:"effect"`
	CreatedBy     string    `json:"createdBy,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

type ApplicationAssignmentInput struct {
	SubjectType string `json:"subjectType"`
	SubjectID   string `json:"subjectId"`
	Effect      string `json:"effect"`
}

type ApplicationPolicy struct {
	ApplicationID   string                  `json:"applicationId"`
	ApplicationSlug string                  `json:"applicationSlug"`
	ApplicationName string                  `json:"applicationName"`
	Category        string                  `json:"category,omitempty"`
	ProviderID      string                  `json:"providerId,omitempty"`
	ProviderType    string                  `json:"providerType"`
	PortalVisible   bool                    `json:"portalVisible"`
	Status          string                  `json:"status"`
	Assignments     []ApplicationAssignment `json:"assignments"`
	UpdatedAt       time.Time               `json:"updatedAt"`
}

type ApplicationPolicyInput struct {
	Assignments []ApplicationAssignmentInput `json:"assignments"`
}

type LaunchDecision struct {
	Application  Application `json:"application"`
	LaunchURL    string      `json:"launchUrl"`
	ProviderType string      `json:"providerType"`
	Decision     string      `json:"decision"`
}

type ApplicationLaunch struct {
	ID              string    `json:"id"`
	ApplicationID   string    `json:"applicationId"`
	ApplicationName string    `json:"applicationName,omitempty"`
	UserID          string    `json:"userId"`
	ProviderID      string    `json:"providerId,omitempty"`
	ProviderType    string    `json:"providerType"`
	Result          string    `json:"result"`
	Reason          string    `json:"reason,omitempty"`
	LaunchURL       string    `json:"launchUrl,omitempty"`
	SourceIP        string    `json:"sourceIp,omitempty"`
	UserAgent       string    `json:"userAgent,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
}

type PortalSecuritySummary struct {
	Principal     domainidentity.Principal `json:"principal"`
	MFAEnabled    bool                     `json:"mfaEnabled"`
	LinkedSources []string                 `json:"linkedSources"`
	ActiveSession int                      `json:"activeSession"`
	RecentLoginAt *time.Time               `json:"recentLoginAt,omitempty"`
}

type PortalBootstrap struct {
	Principal    domainidentity.Principal `json:"principal"`
	Applications []Application            `json:"applications"`
	Favorites    []Application            `json:"favorites"`
	Recent       []ApplicationLaunch      `json:"recent"`
	Categories   []string                 `json:"categories"`
	Security     PortalSecuritySummary    `json:"security"`
}

type ProviderCapability struct {
	Type        string   `json:"type"`
	Status      string   `json:"status"`
	Endpoints   []string `json:"endpoints"`
	Description string   `json:"description,omitempty"`
}

type Repository interface {
	ListApplications(context.Context, ApplicationFilter) ([]Application, error)
	ListPortalApplications(context.Context) ([]Application, error)
	GetApplication(context.Context, string) (Application, error)
	CreateApplication(context.Context, Application) (Application, error)
	UpdateApplication(context.Context, Application) (Application, error)
	DeleteApplication(context.Context, string) error
	ValidateProviderBinding(context.Context, string, string, string) error
	ReplaceAssignments(context.Context, string, []ApplicationAssignment) error
	ListAssignments(context.Context, []string) (map[string][]ApplicationAssignment, error)
	ListFavoriteApplicationIDs(context.Context, string) (map[string]bool, error)
	SetFavorite(context.Context, string, string) error
	DeleteFavorite(context.Context, string, string) error
	ListRecentLaunches(context.Context, string, int) ([]ApplicationLaunch, error)
	GetLastLaunches(context.Context, string) (map[string]time.Time, error)
	RecordLaunch(context.Context, ApplicationLaunch) error
}

func CanAccessApplication(principal domainidentity.Principal, item Application) bool {
	allows := 0
	for _, assignment := range item.Assignments {
		if assignment.Effect != AssignmentEffectAllow {
			continue
		}
		allows++
		switch assignment.SubjectType {
		case AssignmentSubjectUser:
			if assignment.SubjectID == principal.UserID {
				return true
			}
		case AssignmentSubjectRole:
			for _, role := range principal.Roles {
				if assignment.SubjectID == role {
					return true
				}
			}
		case AssignmentSubjectTeam:
			for _, team := range principal.Teams {
				if assignment.SubjectID == team {
					return true
				}
			}
		case AssignmentSubjectTag:
			for _, tag := range principal.Tags {
				if assignment.SubjectID == tag {
					return true
				}
			}
		}
	}
	return allows == 0
}
