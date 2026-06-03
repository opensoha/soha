package dto

type UpsertRoleRequest struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Scope          string   `json:"scope"`
	Capabilities   []string `json:"capabilities"`
	PermissionKeys []string `json:"permissionKeys"`
}

type UpsertTeamRequest struct {
	ID         string         `json:"id"`
	ParentID   string         `json:"parentId"`
	Name       string         `json:"name"`
	Slug       string         `json:"slug"`
	Path       string         `json:"path"`
	Source     string         `json:"source"`
	ExternalID string         `json:"externalId"`
	Metadata   map[string]any `json:"metadata"`
}

type SubjectMatcherRequest struct {
	Roles    []string `json:"roles"`
	Teams    []string `json:"teams"`
	Projects []string `json:"projects"`
	Users    []string `json:"users"`
	Tags     []string `json:"tags"`
}

type ClusterMatcherRequest struct {
	IDs          []string            `json:"ids"`
	Regions      []string            `json:"regions"`
	Environments []string            `json:"environments"`
	Labels       map[string][]string `json:"labels"`
}

type NamespaceMatcherRequest struct {
	Names      []string            `json:"names"`
	OwnerTeams []string            `json:"ownerTeams"`
	Labels     map[string][]string `json:"labels"`
}

type ResourceMatcherRequest struct {
	Kinds  []string            `json:"kinds"`
	Names  []string            `json:"names"`
	Labels map[string][]string `json:"labels"`
}

type ConditionsRequest struct {
	Sources        []string `json:"sources"`
	ApprovalStates []string `json:"approvalStates"`
}

type UpsertPolicyRequest struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name"`
	Effect     string                  `json:"effect"`
	Priority   int                     `json:"priority"`
	Subjects   SubjectMatcherRequest   `json:"subjects"`
	Clusters   ClusterMatcherRequest   `json:"clusters"`
	Namespaces NamespaceMatcherRequest `json:"namespaces"`
	Resources  ResourceMatcherRequest  `json:"resources"`
	Actions    []string                `json:"actions"`
	Conditions ConditionsRequest       `json:"conditions"`
	Reason     string                  `json:"reason"`
}

type ReplaceUserRolesRequest struct {
	RoleIDs []string `json:"roleIds"`
}

type ReplaceUserTeamsRequest struct {
	TeamIDs []string `json:"teamIds"`
}

type UpsertUserRequest struct {
	ID          string         `json:"id"`
	Username    string         `json:"username"`
	Email       string         `json:"email"`
	DisplayName string         `json:"displayName"`
	Status      string         `json:"status"`
	Tags        []string       `json:"tags"`
	RoleIDs     []string       `json:"roleIds"`
	TeamIDs     []string       `json:"teamIds"`
	Preferences map[string]any `json:"preferences"`
	Password    string         `json:"password"`
}
