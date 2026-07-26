package providerportal

import (
	"context"
	"net"
	"strings"
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
	AssignmentEffectDeny  = "deny"
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
	ApplicationID   string                      `json:"applicationId"`
	ApplicationSlug string                      `json:"applicationSlug"`
	ApplicationName string                      `json:"applicationName"`
	Category        string                      `json:"category,omitempty"`
	ProviderID      string                      `json:"providerId,omitempty"`
	ProviderType    string                      `json:"providerType"`
	PortalVisible   bool                        `json:"portalVisible"`
	Status          string                      `json:"status"`
	Assignments     []ApplicationAssignment     `json:"assignments"`
	Conditions      ApplicationPolicyConditions `json:"conditions"`
	UpdatedAt       time.Time                   `json:"updatedAt"`
}

type ApplicationPolicyInput struct {
	Assignments []ApplicationAssignmentInput `json:"assignments"`
	Conditions  ApplicationPolicyConditions  `json:"conditions"`
}

type ApplicationPolicyConditions struct {
	RequireMFA   bool     `json:"requireMfa"`
	AllowedCIDRs []string `json:"allowedCidrs"`
	StartTimeUTC string   `json:"startTimeUtc"`
	EndTimeUTC   string   `json:"endTimeUtc"`
}

type AccessPolicyContext struct {
	SourceIP         string
	MFAAuthenticated bool
	Now              time.Time
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
	CreateApplicationWithAssignments(context.Context, Application, []ApplicationAssignment) (Application, error)
	UpdateApplication(context.Context, Application) (Application, error)
	UpdateApplicationWithAssignments(context.Context, Application, []ApplicationAssignment) (Application, error)
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
	return CanAccessApplicationWithContext(principal, item, AccessPolicyContext{})
}

func CanAccessApplicationWithContext(principal domainidentity.Principal, item Application, access AccessPolicyContext) bool {
	allows, matchedAllow := 0, false
	for _, assignment := range item.Assignments {
		if assignment.Effect == AssignmentEffectAllow {
			allows++
		}
		if !assignmentMatches(principal, assignment) {
			continue
		}
		if assignment.Effect == AssignmentEffectDeny {
			return false
		}
		if assignment.Effect == AssignmentEffectAllow {
			matchedAllow = true
		}
	}
	if allows > 0 && !matchedAllow {
		return false
	}
	return policyConditionsMatch(ApplicationPolicyConditionsFromMetadata(item.Metadata), access)
}

func assignmentMatches(principal domainidentity.Principal, assignment ApplicationAssignment) bool {
	switch assignment.SubjectType {
	case AssignmentSubjectUser:
		return assignment.SubjectID == principal.UserID
	case AssignmentSubjectRole:
		return slicesContain(principal.Roles, assignment.SubjectID)
	case AssignmentSubjectTeam:
		return slicesContain(principal.Teams, assignment.SubjectID)
	case AssignmentSubjectTag:
		return slicesContain(principal.Tags, assignment.SubjectID)
	default:
		return false
	}
}

func slicesContain(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func ApplicationPolicyConditionsFromMetadata(metadata map[string]any) ApplicationPolicyConditions {
	raw, ok := metadata["accessPolicy"]
	if !ok {
		return ApplicationPolicyConditions{AllowedCIDRs: []string{}}
	}
	if conditions, ok := raw.(ApplicationPolicyConditions); ok {
		conditions.AllowedCIDRs = append([]string(nil), conditions.AllowedCIDRs...)
		return conditions
	}
	values, ok := raw.(map[string]any)
	if !ok {
		return ApplicationPolicyConditions{RequireMFA: true}
	}
	conditions := ApplicationPolicyConditions{
		RequireMFA:   boolValue(values["requireMfa"]),
		StartTimeUTC: stringValue(values["startTimeUtc"]),
		EndTimeUTC:   stringValue(values["endTimeUtc"]),
	}
	for _, value := range stringSliceValue(values["allowedCidrs"]) {
		conditions.AllowedCIDRs = append(conditions.AllowedCIDRs, value)
	}
	return conditions
}

func policyConditionsMatch(conditions ApplicationPolicyConditions, access AccessPolicyContext) bool {
	if conditions.RequireMFA && !access.MFAAuthenticated {
		return false
	}
	if len(conditions.AllowedCIDRs) > 0 {
		ip := net.ParseIP(strings.TrimSpace(access.SourceIP))
		if ip == nil {
			return false
		}
		matched := false
		for _, value := range conditions.AllowedCIDRs {
			_, network, err := net.ParseCIDR(value)
			if err == nil && network.Contains(ip) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if conditions.StartTimeUTC == "" && conditions.EndTimeUTC == "" {
		return true
	}
	start, startErr := time.Parse("15:04", conditions.StartTimeUTC)
	end, endErr := time.Parse("15:04", conditions.EndTimeUTC)
	if startErr != nil || endErr != nil || access.Now.IsZero() {
		return false
	}
	nowMinutes := access.Now.UTC().Hour()*60 + access.Now.UTC().Minute()
	startMinutes := start.Hour()*60 + start.Minute()
	endMinutes := end.Hour()*60 + end.Minute()
	if startMinutes <= endMinutes {
		return nowMinutes >= startMinutes && nowMinutes <= endMinutes
	}
	return nowMinutes >= startMinutes || nowMinutes <= endMinutes
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func stringValue(value any) string {
	result, _ := value.(string)
	return strings.TrimSpace(result)
}

func stringSliceValue(value any) []string {
	switch items := value.(type) {
	case []string:
		return items
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
		}
		return out
	default:
		return nil
	}
}
