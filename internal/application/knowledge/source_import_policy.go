package knowledge

import (
	"slices"
	"strings"

	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

func sourceImportCeiling(source domainknowledge.Source, ownerID string) (*domainknowledge.AccessScope, error) {
	switch source.Kind {
	case domainknowledge.SourceKindHTTP, domainknowledge.SourceKindGit, domainknowledge.SourceKindObject:
	default:
		return nil, nil
	}
	policy, err := domainknowledge.ParseSourceImportPolicy(source.Config)
	if err != nil {
		return nil, err
	}
	if policy != nil {
		return &policy.Scope, nil
	}
	scope := domainknowledge.AccessScope{Visibility: "private"}
	if ownerID = strings.TrimSpace(ownerID); ownerID != "" {
		scope.Users = []string{ownerID}
	}
	return &scope, nil
}

func governedImportedScope(remote domainknowledge.AccessScope, ownerID string, ceiling *domainknowledge.AccessScope) domainknowledge.AccessScope {
	if ceiling == nil {
		return normalizeImportedAccessScope(remote, ownerID)
	}
	if strings.TrimSpace(remote.Visibility) == "" && len(remote.Users)+len(remote.Roles)+len(remote.Teams)+len(remote.Projects) == 0 {
		return *ceiling
	}
	remote = normalizeImportedAccessScope(remote, ownerID)
	if ceiling.Visibility == "public" {
		return remote
	}
	if remote.Visibility == "public" {
		return *ceiling
	}
	visibility := "private"
	if ceiling.Visibility == "scoped" {
		visibility = "scoped"
	}
	return domainknowledge.AccessScope{
		Visibility: visibility,
		Users:      intersectImportGrants(remote.Users, ceiling.Users),
		Roles:      intersectImportGrants(remote.Roles, ceiling.Roles),
		Teams:      intersectImportGrants(remote.Teams, ceiling.Teams),
		Projects:   intersectImportGrants(remote.Projects, ceiling.Projects),
	}
}

func intersectImportGrants(remote, allowed []string) []string {
	return slices.DeleteFunc(slices.Clone(remote), func(id string) bool { return !slices.Contains(allowed, id) })
}
