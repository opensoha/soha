package integration

import (
	"context"

	domainmcp "github.com/kubecrux/kubecrux/internal/domain/mcp"
)

type Registry interface {
	ListCapabilities() []domainmcp.Capability
}

type Service struct {
	registry Registry
}

func New(registry Registry) *Service {
	return &Service{registry: registry}
}

func (s *Service) ListCapabilities(_ context.Context) ([]domainmcp.Capability, error) {
	if s.registry == nil {
		return []domainmcp.Capability{}, nil
	}
	return s.registry.ListCapabilities(), nil
}
