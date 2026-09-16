package docker

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"sigs.k8s.io/yaml"
)

// DeliveryProjectAccess reads the frozen deployment, never the mutable current
// Compose configuration. Ports are candidates until an explicit HTTP probe passes.
func (s *Service) DeliveryProjectAccess(ctx context.Context, principal domainidentity.Principal, snapshot sohaapi.DockerDeliverySnapshot, ciphertext string) ([]domaindelivery.AccessCandidate, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerHostsView); err != nil {
		return nil, err
	}
	project, err := s.GetProject(ctx, principal, snapshot.ProjectID)
	if err != nil {
		return nil, err
	}
	if project.HostID != snapshot.HostID {
		return nil, apperrors.ErrConflict
	}
	if err := checkProjectScope(ctx, project); err != nil {
		return nil, err
	}
	frozen, err := s.openDeliveryProject(ciphertext)
	if err != nil {
		return nil, err
	}
	digest, err := dockerDeliveryDigest(struct{ Compose, Env string }{frozen.Project.ComposeContent, frozen.Project.EnvContent})
	if err != nil || digest != snapshot.RenderedDigest || frozen.Project.ID != snapshot.ProjectID || frozen.Project.HostID != snapshot.HostID || frozen.ProjectDigest != snapshot.ProjectDigest {
		return nil, apperrors.ErrConflict
	}
	host, err := s.repo.GetHost(ctx, snapshot.HostID)
	if err != nil {
		return nil, err
	}
	address, err := netip.ParseAddr(host.IPAddress)
	if err != nil {
		return nil, nil
	}
	return composeAccessCandidates(frozen.Project.ComposeContent, address), nil
}

func composeAccessCandidates(content string, address netip.Addr) []domaindelivery.AccessCandidate {
	var compose struct {
		Services map[string]struct {
			Ports []any `json:"ports"`
		} `json:"services"`
	}
	if yaml.Unmarshal([]byte(content), &compose) != nil {
		return nil
	}
	urls := []string{}
	for _, service := range compose.Services {
		for _, entry := range service.Ports {
			port, bind := composePublishedPort(entry)
			if port == 0 {
				continue
			}
			if bind != "" {
				ip, err := netip.ParseAddr(strings.Trim(bind, "[]"))
				if err != nil || !ip.IsUnspecified() && ip.Unmap() != address.Unmap() {
					continue
				}
			}
			// Compose does not identify application protocols; HTTP is only a
			// candidate, and a failed probe never marks the application healthy.
			urls = append(urls, "http://"+net.JoinHostPort(address.String(), strconv.Itoa(port))+"/")
		}
	}
	slices.Sort(urls)
	result := []domaindelivery.AccessCandidate{}
	for _, url := range slices.Compact(urls) {
		result = append(result, domaindelivery.AccessCandidate{URL: url, DialAddress: address.String(), ProtocolUnknown: true})
	}
	return result
}

func composePublishedPort(entry any) (int, string) {
	var published, bind string
	switch value := entry.(type) {
	case string:
		portSpec, protocol, hasProtocol := strings.Cut(value, "/")
		if hasProtocol && protocol != "tcp" {
			return 0, ""
		}
		before, _, ok := strings.Cut(portSpec, ":")
		if !ok || before == "" {
			return 0, ""
		}
		// The final component is the container port. Split the remaining
		// host:published pair with net, preserving bracketed IPv6 addresses.
		prefix := portSpec[:strings.LastIndex(portSpec, ":")]
		if strings.Contains(prefix, ":") {
			var err error
			bind, published, err = net.SplitHostPort(prefix)
			if err != nil {
				return 0, ""
			}
		} else {
			published = prefix
		}
	case map[string]any:
		if protocol, ok := value["protocol"]; ok && protocol != "tcp" {
			return 0, ""
		}
		published, bind = fmt.Sprint(value["published"]), fmt.Sprint(value["host_ip"])
		if value["host_ip"] == nil {
			bind = ""
		}
	default:
		return 0, ""
	}
	port, err := strconv.Atoi(published)
	if err != nil || port < 1 || port > 65535 {
		return 0, ""
	}
	return port, bind
}
