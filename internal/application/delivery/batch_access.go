package delivery

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/netguard"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

func accessManifestDocuments(content string) ([]domainmanifest.RenderedDocument, error) {
	decoder := kubeyaml.NewYAMLOrJSONDecoder(strings.NewReader(content), 4096)
	documents := []domainmanifest.RenderedDocument{}
	for {
		var object map[string]any
		if err := decoder.Decode(&object); err == io.EOF {
			return documents, nil
		} else if err != nil {
			return nil, apperrors.ErrConflict
		}
		if object == nil {
			continue
		}
		kind, _ := object["kind"].(string)
		if kind != "Ingress" && kind != "Service" {
			continue
		}
		encoded, err := yaml.Marshal(object)
		if err != nil {
			return nil, err
		}
		documents = append(documents, domainmanifest.RenderedDocument{Kind: kind, Content: string(encoded)})
	}
}

func (s *Service) manifestAccessCandidates(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string, documents []domainmanifest.RenderedDocument) ([]domaindelivery.AccessCandidate, error) {
	if s.targets == nil {
		return nil, nil
	}
	services, err := s.targets.ListServices(ctx, principal, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	ingresses, err := s.targets.ListIngresses(ctx, principal, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	result := []domaindelivery.AccessCandidate{}
	for _, document := range documents {
		switch document.Kind {
		case "Service":
			var desired corev1.Service
			if yaml.Unmarshal([]byte(document.Content), &desired) != nil || desired.Namespace != "" && desired.Namespace != namespace {
				continue
			}
			result = append(result, serviceAccessCandidates(desired, namespace, services)...)
		case "Ingress":
			var desired networkingv1.Ingress
			if yaml.Unmarshal([]byte(document.Content), &desired) != nil || desired.Namespace != "" && desired.Namespace != namespace {
				continue
			}
			resolved := []domainresource.IngressView{}
			for _, live := range ingresses {
				if live.Name == desired.Name && live.Namespace == namespace {
					live.Address = strings.Join(resolveIngressAddresses(ctx, live.Address), ",")
					resolved = append(resolved, live)
				}
			}
			result = append(result, ingressAccessCandidates(desired, namespace, resolved)...)
		}
	}
	return result, nil
}

func resolveIngressAddresses(ctx context.Context, value string) []string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	result := []string{}
	for _, host := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' }) {
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			continue
		}
		for _, address := range addresses {
			address = address.Unmap()
			if address.IsLoopback() || !netguard.AllowedSourceAddress(address, []netip.Prefix{netip.PrefixFrom(address, address.BitLen())}) {
				continue
			}
			result = append(result, address.Unmap().String())
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func serviceAccessCandidates(desired corev1.Service, namespace string, services []domainresource.ServiceView) []domaindelivery.AccessCandidate {
	result := []domaindelivery.AccessCandidate{}
	for _, live := range services {
		if live.Namespace != namespace || live.Name != desired.Name {
			continue
		}
		address, err := netip.ParseAddr(live.ClusterIP)
		if err != nil || address.IsLoopback() {
			continue
		}
		for _, port := range desired.Spec.Ports {
			if port.Protocol != "" && port.Protocol != corev1.ProtocolTCP {
				continue
			}
			scheme := "http"
			if port.Name == "https" || port.AppProtocol != nil && *port.AppProtocol == "https" {
				scheme = "https"
			}
			result = append(result, domaindelivery.AccessCandidate{URL: scheme + "://" + net.JoinHostPort(address.String(), strconv.Itoa(int(port.Port))) + "/", DialAddress: address.String()})
		}
	}
	return result
}

func ingressAccessCandidates(desired networkingv1.Ingress, namespace string, ingresses []domainresource.IngressView) []domaindelivery.AccessCandidate {
	result := []domaindelivery.AccessCandidate{}
	for _, live := range ingresses {
		if live.Namespace != namespace || live.Name != desired.Name {
			continue
		}
		for _, rule := range desired.Spec.Rules {
			if rule.HTTP == nil || rule.Host == "" || strings.Contains(rule.Host, "*") || !slices.Contains(live.Hosts, rule.Host) {
				continue
			}
			scheme := "http"
			for _, tls := range desired.Spec.TLS {
				if slices.Contains(tls.Hosts, rule.Host) {
					scheme = "https"
				}
			}
			for _, route := range rule.HTTP.Paths {
				if route.Backend.Service == nil || !slices.Contains(live.BackendServices, route.Backend.Service.Name) {
					continue
				}
				for _, raw := range strings.FieldsFunc(live.Address, func(r rune) bool { return r == ',' || r == ' ' }) {
					address, err := netip.ParseAddr(raw)
					if err != nil || address.IsLoopback() {
						continue
					}
					result = append(result, domaindelivery.AccessCandidate{URL: (&url.URL{Scheme: scheme, Host: rule.Host, Path: route.Path}).String(), DialAddress: address.String()})
				}
			}
		}
	}
	return result
}

func assessmentURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, apperrors.ErrInvalidArgument
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	if parsed.Path != path.Clean(parsed.Path) && parsed.Path != path.Clean(parsed.Path)+"/" {
		return nil, apperrors.ErrInvalidArgument
	}
	return parsed, nil
}

func assessmentPort(value *url.URL) string {
	if value.Port() != "" {
		return value.Port()
	}
	if value.Scheme == "https" {
		return "443"
	}
	return "80"
}

func assessTargetAccess(ctx context.Context, input sohaapi.DeliveryBatchAssessmentInput, candidates []domaindelivery.AccessCandidate, result *sohaapi.DeliveryBatchAssessment) error {
	matched := false
	seen := map[string]bool{}
	for _, candidate := range candidates {
		parsed, err := assessmentURL(candidate.URL)
		if err != nil || seen[parsed.String()] {
			continue
		}
		seen[parsed.String()] = true
		entry := sohaapi.DeliveryAccessResult{URL: parsed.String(), ServiceID: result.ServiceID, ProbeLocation: "soha_control_plane", Reachability: "unverified", Authentication: "unknown", NetworkRequirement: "unknown", Summary: "configured entry candidate; HTTP availability is unverified"}
		address, err := netip.ParseAddr(candidate.DialAddress)
		if err == nil && (address.IsPrivate() || address.IsLoopback()) {
			entry.NetworkRequirement = "private_network"
		}
		if input.HTTP != nil {
			requested, err := assessmentURL(input.HTTP.URL)
			if err != nil {
				return err
			}
			if accessCandidateMatches(requested, parsed, candidate.ProtocolUnknown) {
				matched = true
				entry.URL = requested.String()
				if err := probeDeliveryAccess(ctx, requested, candidate.DialAddress, input, &entry); err != nil {
					return err
				}
				mergeAccessAssessment(entry, result)
			}
		}
		result.Access = append(result.Access, entry)
	}
	if input.HTTP != nil && !matched {
		return apperrors.ErrInvalidArgument
	}
	return nil
}

func accessCandidateMatches(requested, candidate *url.URL, protocolUnknown bool) bool {
	protocolMatches := requested.Scheme == candidate.Scheme || protocolUnknown && requested.Scheme == "https" && candidate.Scheme == "http"
	return protocolMatches && requested.Hostname() == candidate.Hostname() && assessmentPort(requested) == assessmentPort(candidate) && requested.Path == candidate.Path
}

func probeDeliveryAccess(ctx context.Context, entryURL *url.URL, dialAddress string, input sohaapi.DeliveryBatchAssessmentInput, result *sohaapi.DeliveryAccessResult) error {
	expected := input.HTTP.ExpectedStatus
	if expected == 0 {
		expected = http.StatusOK
	}
	healthPath := input.HTTP.HealthPath
	basePath := strings.TrimSuffix(entryURL.Path, "/")
	if expected < 200 || expected > 299 || len(healthPath) > 1024 || !strings.HasPrefix(healthPath, "/") || strings.HasPrefix(healthPath, "//") || strings.ContainsAny(healthPath, "?#\\%") || path.Clean(healthPath) != healthPath || basePath != "" && healthPath != basePath && !strings.HasPrefix(healthPath, basePath+"/") {
		return apperrors.ErrInvalidArgument
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result.Reachability, result.Summary = "inconclusive", "control-plane HTTP probe could not verify this entry"
	now := time.Now().UTC()
	validUntil := now.Add(time.Duration(input.MaxAgeSeconds) * time.Second)
	result.CheckedAt, result.ValidUntil = &now, &validUntil
	address, err := netip.ParseAddr(dialAddress)
	if err != nil {
		return nil
	}
	client, err := netguard.PinnedResourceHTTPClient(entryURL.Hostname(), assessmentPort(entryURL), address)
	if err != nil {
		return nil
	}
	defer client.CloseIdleConnections()
	probeURL := *entryURL
	probeURL.Path, probeURL.RawPath = healthPath, ""
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL.String(), nil)
	if err != nil {
		return apperrors.ErrInvalidArgument
	}
	response, err := client.Do(request)
	if err != nil {
		return nil
	}
	defer func() { _ = response.Body.Close() }()
	result.StatusCode, result.Reachability = response.StatusCode, "unsatisfied"
	result.Summary = "GET " + healthPath + " did not return the expected HTTP status"
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		result.Authentication = "required"
	}
	if response.StatusCode == expected {
		result.Reachability, result.Summary = "satisfied", "GET "+healthPath+" returned the expected HTTP status from the control plane"
	}
	return nil
}

func mergeAccessAssessment(entry sohaapi.DeliveryAccessResult, result *sohaapi.DeliveryBatchAssessment) {
	result.Evidence = append(result.Evidence, sohaapi.CapabilityEvidence{Kind: "http_probe", Source: "delivery.control_plane.http", ObservedAt: *entry.CheckedAt, DataThrough: entry.CheckedAt, Incomplete: entry.Reachability == "inconclusive", Summary: entry.Summary, Resource: &sohaapi.CapabilityResourceRef{Kind: "delivery.service", ID: result.ServiceID, Scope: map[string]string{"applicationId": result.ApplicationID}}})
	if entry.Reachability == "unsatisfied" {
		result.Verdict = "unsatisfied"
	}
	if entry.Reachability == "inconclusive" && result.Verdict != "unsatisfied" {
		result.Verdict = "inconclusive"
	}
}
