package access

import (
	"context"
	"time"

	"github.com/kubecrux/kubecrux/internal/domain/identity"
)

type Action string

const (
	ActionView     Action = "view"
	ActionList     Action = "list"
	ActionWatch    Action = "watch"
	ActionCreate   Action = "create"
	ActionUpdate   Action = "update"
	ActionDelete   Action = "delete"
	ActionRestart  Action = "restart"
	ActionRollback Action = "rollback"
	ActionScale    Action = "scale"
	ActionTrigger  Action = "trigger"
	ActionLogs     Action = "logs"
	ActionExec     Action = "exec"
)

type PolicyEffect string

const (
	EffectAllow PolicyEffect = "allow"
	EffectDeny  PolicyEffect = "deny"
)

type SubjectAttributes struct {
	UserID   string
	Roles    []string
	Teams    []string
	Projects []string
	Tags     []string
}

type ClusterAttributes struct {
	ClusterID   string
	Region      string
	Environment string
	Labels      map[string]string
}

type NamespaceAttributes struct {
	Namespace string
	Labels    map[string]string
	OwnerTeam string
}

type ResourceAttributes struct {
	Kind        string
	Name        string
	Labels      map[string]string
	Annotations map[string]string
	Owner       string
}

type DeliveryAttributes struct {
	BusinessLineID string
	EnvironmentKey string
	ApplicationID  string
}

type ContextAttributes struct {
	Source        string
	ApprovalState string
	OccurredAt    time.Time
}

type Request struct {
	Principal identity.Principal
	Action    Action
	Subject   SubjectAttributes
	Cluster   ClusterAttributes
	Namespace NamespaceAttributes
	Resource  ResourceAttributes
	Delivery  DeliveryAttributes
	Context   ContextAttributes
}

type ResourceScope struct {
	Clusters      []string `json:"clusters,omitempty"`
	Namespaces    []string `json:"namespaces,omitempty"`
	LabelSelector string   `json:"labelSelector,omitempty"`
}

type Decision struct {
	Allowed        bool           `json:"allowed"`
	Reason         string         `json:"reason"`
	AllowedActions []Action       `json:"allowedActions,omitempty"`
	ResourceScope  *ResourceScope `json:"resourceScope,omitempty"`
}

type Matcher struct {
	Roles    []string            `json:"roles,omitempty"`
	Teams    []string            `json:"teams,omitempty"`
	Projects []string            `json:"projects,omitempty"`
	Users    []string            `json:"users,omitempty"`
	Tags     []string            `json:"tags,omitempty"`
	Labels   map[string][]string `json:"labels,omitempty"`
}

type ClusterMatcher struct {
	IDs          []string            `json:"ids,omitempty"`
	Regions      []string            `json:"regions,omitempty"`
	Environments []string            `json:"environments,omitempty"`
	Labels       map[string][]string `json:"labels,omitempty"`
}

type NamespaceMatcher struct {
	Names      []string            `json:"names,omitempty"`
	OwnerTeams []string            `json:"ownerTeams,omitempty"`
	Labels     map[string][]string `json:"labels,omitempty"`
}

type ResourceMatcher struct {
	Kinds  []string            `json:"kinds,omitempty"`
	Names  []string            `json:"names,omitempty"`
	Labels map[string][]string `json:"labels,omitempty"`
}

type Conditions struct {
	Sources        []string `json:"sources,omitempty"`
	ApprovalStates []string `json:"approvalStates,omitempty"`
}

type Policy struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Effect     PolicyEffect     `json:"effect"`
	Priority   int              `json:"priority"`
	Subjects   Matcher          `json:"subjects"`
	Clusters   ClusterMatcher   `json:"clusters"`
	Namespaces NamespaceMatcher `json:"namespaces"`
	Resources  ResourceMatcher  `json:"resources"`
	Actions    []Action         `json:"actions"`
	Conditions Conditions       `json:"conditions"`
	Reason     string           `json:"reason"`
}

type UserRecord struct {
	ID          string     `json:"id"`
	Username    string     `json:"username"`
	Email       string     `json:"email"`
	DisplayName string     `json:"displayName"`
	Status      string     `json:"status"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
	Tags        []string   `json:"tags"`
	Roles       []string   `json:"roles"`
	Teams       []string   `json:"teams"`
	Projects    []string   `json:"projects"`
}

type UserInput struct {
	ID          string         `json:"id"`
	Username    string         `json:"username"`
	Email       string         `json:"email"`
	DisplayName string         `json:"displayName"`
	Status      string         `json:"status"`
	Tags        []string       `json:"tags"`
	RoleIDs     []string       `json:"roleIds,omitempty"`
	TeamIDs     []string       `json:"teamIds,omitempty"`
	Preferences map[string]any `json:"preferences"`
	Password    string         `json:"password,omitempty"`
}

type RoleRecord struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Scope          string   `json:"scope"`
	Capabilities   []Action `json:"capabilities"`
	PermissionKeys []string `json:"permissionKeys"`
	UserCount      int      `json:"userCount"`
}

type TeamRecord struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Slug      string         `json:"slug"`
	Metadata  map[string]any `json:"metadata"`
	UserCount int            `json:"userCount"`
}

type RoleInput struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Scope          string   `json:"scope"`
	Capabilities   []Action `json:"capabilities"`
	PermissionKeys []string `json:"permissionKeys"`
}

type TeamInput struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Slug     string         `json:"slug"`
	Metadata map[string]any `json:"metadata"`
}

type PolicyInput struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Effect     PolicyEffect     `json:"effect"`
	Priority   int              `json:"priority"`
	Subjects   Matcher          `json:"subjects"`
	Clusters   ClusterMatcher   `json:"clusters"`
	Namespaces NamespaceMatcher `json:"namespaces"`
	Resources  ResourceMatcher  `json:"resources"`
	Actions    []Action         `json:"actions"`
	Conditions Conditions       `json:"conditions"`
	Reason     string           `json:"reason"`
}

type VisibleMenu struct {
	ID        string `json:"id"`
	ParentID  string `json:"parentId,omitempty"`
	Path      string `json:"path"`
	LabelZH   string `json:"labelZh"`
	LabelEN   string `json:"labelEn"`
	IconKey   string `json:"iconKey"`
	Section   string `json:"section"`
	SortOrder int    `json:"sortOrder"`
	Enabled   bool   `json:"enabled"`
}

type PermissionSnapshot struct {
	PermissionKeys []string      `json:"permissionKeys"`
	VisibleMenuIDs []string      `json:"visibleMenuIds"`
	VisibleMenus   []VisibleMenu `json:"visibleMenus"`
}

type Authorizer interface {
	Authorize(context.Context, Request) (Decision, error)
}

type PolicyEngine interface {
	Evaluate(context.Context, Request, []Policy) (Decision, error)
}
