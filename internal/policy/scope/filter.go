package scope

import domainaccess "github.com/soha/soha/internal/domain/access"

func Build(decision domainaccess.Decision) *domainaccess.ResourceScope {
	return decision.ResourceScope
}
