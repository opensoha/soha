package dto

type UpsertApplicationRequest struct {
	ID                  string               `json:"id"`
	Name                string               `json:"name"`
	Key                 string               `json:"key"`
	Group               string               `json:"group"`
	BusinessLineID      string               `json:"businessLineId"`
	Language            string               `json:"language"`
	Description         string               `json:"description"`
	OwnerTeam           string               `json:"ownerTeam"`
	RepositoryProvider  string               `json:"repositoryProvider"`
	RepositoryIDs       []string             `json:"repositoryIds"`
	RepositoryProjectID string               `json:"repositoryProjectId"`
	RepositoryPath      string               `json:"repositoryPath"`
	DefaultBranch       string               `json:"defaultBranch"`
	DefaultTag          string               `json:"defaultTag"`
	BuildImage          string               `json:"buildImage"`
	BuildContextDir     string               `json:"buildContextDir"`
	DockerfilePath      string               `json:"dockerfilePath"`
	Enabled             bool                 `json:"enabled"`
	Metadata            map[string]any       `json:"metadata"`
	BuildSources        []BuildSourceRequest `json:"buildSources"`
}

type BuildSourceRequest struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Enabled    bool           `json:"enabled"`
	IsDefault  bool           `json:"isDefault"`
	BuildImage string         `json:"buildImage"`
	DefaultTag string         `json:"defaultTag"`
	Config     map[string]any `json:"config"`
}

type UpsertApplicationServiceRequest struct {
	ID                  string                    `json:"id"`
	Key                 string                    `json:"key"`
	Name                string                    `json:"name"`
	Description         string                    `json:"description"`
	ServiceKind         string                    `json:"serviceKind"`
	OwnerTeam           string                    `json:"ownerTeam"`
	RepositoryProvider  string                    `json:"repositoryProvider"`
	RepositoryID        string                    `json:"repositoryId"`
	RepositoryProjectID string                    `json:"repositoryProjectId"`
	RepositoryPath      string                    `json:"repositoryPath"`
	DefaultBranch       string                    `json:"defaultBranch"`
	BuildSourceID       string                    `json:"buildSourceId"`
	Enabled             bool                      `json:"enabled"`
	Metadata            map[string]any            `json:"metadata"`
	Containers          []ApplicationContainerReq `json:"containers"`
}

type UpsertSourceRepositoryRequest struct {
	Name            string   `json:"name"`
	Provider        string   `json:"provider"`
	URL             string   `json:"url"`
	Protocol        string   `json:"protocol"`
	GitLabProjectID string   `json:"gitlabProjectId"`
	Path            string   `json:"path"`
	CredentialRef   string   `json:"credentialRef"`
	DefaultBranch   string   `json:"defaultBranch"`
	ApplicationIDs  []string `json:"applicationIds"`
}

type ApplicationContainerReq struct {
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	ImageRepository    string         `json:"imageRepository"`
	DefaultTagTemplate string         `json:"defaultTagTemplate"`
	DockerfilePath     string         `json:"dockerfilePath"`
	BuildContextDir    string         `json:"buildContextDir"`
	RuntimePorts       []int          `json:"runtimePorts"`
	EnvSchema          map[string]any `json:"envSchema"`
	ResourceProfile    map[string]any `json:"resourceProfile"`
	HealthCheck        map[string]any `json:"healthCheck"`
	Metadata           map[string]any `json:"metadata"`
}
