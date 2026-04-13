package scope

import domainaccess "github.com/kubecrux/kubecrux/internal/domain/access"

func Build(decision domainaccess.Decision) *domainaccess.ResourceScope {
	return decision.ResourceScope
}
