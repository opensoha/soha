package cluster

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"sigs.k8s.io/yaml"
)

const (
	agentReverseSessionTransport = "reverse_session"
	agentInstallTicketTTL        = 15 * time.Minute
	agentImage                   = "ghcr.io/opensoha/soha-agent:v0.1.6"
)

var ErrAgentInstallationExpired = errors.New("agent installation expired")

func (s *Service) CreateAgentInstallation(ctx context.Context, principal domainidentity.Principal, clusterID string) (domaincluster.AgentInstallation, error) {
	if s.repo == nil || s.accessURL == nil {
		return domaincluster.AgentInstallation{}, fmt.Errorf("%w: agent installation service is unavailable", apperrors.ErrInvalidArgument)
	}
	connection, err := s.repo.GetConnection(ctx, strings.TrimSpace(clusterID))
	if err != nil {
		return domaincluster.AgentInstallation{}, err
	}
	if err := s.authorize(ctx, principal, connection.Summary, domainaccess.ActionUpdate); err != nil {
		return domaincluster.AgentInstallation{}, err
	}
	if connection.Summary.ConnectionMode != domaincluster.ConnectionModeAgent {
		return domaincluster.AgentInstallation{}, fmt.Errorf("%w: cluster is not configured for Agent mode", apperrors.ErrConflict)
	}
	accessURL := strings.TrimSpace(s.accessURL.AccessURL())
	if accessURL == "" {
		return domaincluster.AgentInstallation{}, fmt.Errorf("%w: configure 访问地址 before generating the Agent installation", apperrors.ErrInvalidArgument)
	}
	if _, err := buildAgentManifestURL(accessURL, "validate"); err != nil {
		return domaincluster.AgentInstallation{}, fmt.Errorf("%w: invalid 访问地址", apperrors.ErrInvalidArgument)
	}

	connection.Metadata = cloneMetadata(connection.Metadata)
	if metadataString(connection.Metadata, "transport") != agentReverseSessionTransport {
		token, err := newAgentSecret()
		if err != nil {
			return domaincluster.AgentInstallation{}, err
		}
		connection.CredentialType = "bearer"
		connection.SourceType = "agent"
		connection.SourceRef = agentReverseSessionTransport
		connection.Metadata["transport"] = agentReverseSessionTransport
		connection.Metadata["endpoint"] = ""
		connection.Metadata["token"] = token
	}
	if strings.TrimSpace(metadataString(connection.Metadata, "token")) == "" {
		return domaincluster.AgentInstallation{}, fmt.Errorf("%w: cluster Agent token is unavailable", apperrors.ErrConflict)
	}

	ticketSecret, err := newAgentSecret()
	if err != nil {
		return domaincluster.AgentInstallation{}, err
	}
	ticket := base64.RawURLEncoding.EncodeToString([]byte(connection.Summary.ID)) + "." + ticketSecret
	expiresAt := time.Now().UTC().Add(agentInstallTicketTTL)
	connection.Metadata["install_ticket_hash"] = agentSecretHash(ticketSecret)
	connection.Metadata["install_ticket_expires_at"] = expiresAt.Format(time.RFC3339Nano)
	if err := s.repo.UpdateRegistration(ctx, connection); err != nil {
		return domaincluster.AgentInstallation{}, fmt.Errorf("persist Agent installation ticket: %w", err)
	}

	manifestURL, err := buildAgentManifestURL(accessURL, ticket)
	if err != nil {
		return domaincluster.AgentInstallation{}, fmt.Errorf("%w: invalid 访问地址", apperrors.ErrInvalidArgument)
	}
	installation := domaincluster.AgentInstallation{
		ClusterID: connection.Summary.ID, ManifestURL: manifestURL,
		Command: "kubectl apply -f " + manifestURL, ExpiresAt: expiresAt,
	}
	if err := s.recordAudit(ctx, principal, connection.Summary.ID, "Cluster", connection.Summary.Name, string(domainaccess.ActionUpdate), "success", "generated cluster Agent installation"); err != nil {
		return domaincluster.AgentInstallation{}, fmt.Errorf("record Agent installation audit: %w", err)
	}
	s.recordOperation(ctx, principal, "platform.cluster.agent.installation", connection.Summary.ID, connection.Summary.Name, "generated cluster Agent installation")
	return installation, nil
}

func (s *Service) RenderAgentInstallation(ctx context.Context, ticket string) ([]byte, error) {
	connection, err := s.connectionForInstallTicket(ctx, ticket)
	if err != nil {
		return nil, err
	}
	return renderAgentManifest(connection, strings.TrimSpace(s.accessURL.AccessURL()))
}

func (s *Service) AuthenticateAgentSession(ctx context.Context, clusterID, token string) error {
	if s.repo == nil {
		return fmt.Errorf("%w: cluster repository is required", apperrors.ErrInvalidArgument)
	}
	connection, err := s.repo.GetConnection(ctx, strings.TrimSpace(clusterID))
	if err != nil {
		return err
	}
	if connection.Summary.ConnectionMode != domaincluster.ConnectionModeAgent || metadataString(connection.Metadata, "transport") != agentReverseSessionTransport {
		return fmt.Errorf("%w: cluster does not accept reverse Agent sessions", apperrors.ErrConflict)
	}
	expected := strings.TrimSpace(metadataString(connection.Metadata, "token"))
	provided := strings.TrimSpace(token)
	if expected == "" || provided == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) != 1 {
		return apperrors.ErrUnauthorized
	}
	if _, ok := connection.Metadata["install_ticket_hash"]; ok {
		connection.Metadata = cloneMetadata(connection.Metadata)
		delete(connection.Metadata, "install_ticket_hash")
		delete(connection.Metadata, "install_ticket_expires_at")
		if err := s.repo.UpdateRegistration(ctx, connection); err != nil {
			return fmt.Errorf("invalidate Agent installation ticket: %w", err)
		}
	}
	return nil
}

func (s *Service) RefreshAgentSession(ctx context.Context, clusterID string) error {
	return s.syncOne(ctx, strings.TrimSpace(clusterID))
}

func (s *Service) connectionForInstallTicket(ctx context.Context, ticket string) (domaincluster.Connection, error) {
	encodedClusterID, ticketSecret, ok := strings.Cut(strings.TrimSpace(ticket), ".")
	if !ok || encodedClusterID == "" || ticketSecret == "" {
		return domaincluster.Connection{}, apperrors.ErrNotFound
	}
	clusterIDBytes, err := base64.RawURLEncoding.DecodeString(encodedClusterID)
	if err != nil || len(clusterIDBytes) == 0 {
		return domaincluster.Connection{}, apperrors.ErrNotFound
	}
	connection, err := s.repo.GetConnection(ctx, string(clusterIDBytes))
	if err != nil {
		return domaincluster.Connection{}, apperrors.ErrNotFound
	}
	expectedHash := metadataString(connection.Metadata, "install_ticket_hash")
	providedHash := agentSecretHash(ticketSecret)
	if expectedHash == "" || subtle.ConstantTimeCompare([]byte(expectedHash), []byte(providedHash)) != 1 {
		return domaincluster.Connection{}, apperrors.ErrNotFound
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, metadataString(connection.Metadata, "install_ticket_expires_at"))
	if err != nil || !time.Now().UTC().Before(expiresAt) {
		return domaincluster.Connection{}, ErrAgentInstallationExpired
	}
	return connection, nil
}

func newAgentSecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate Agent secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func agentSecretHash(secret string) string {
	hash := sha256.Sum256([]byte(secret))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func cloneMetadata(source map[string]any) map[string]any {
	result := make(map[string]any, len(source)+2)
	for key, value := range source {
		result[key] = value
	}
	return result
}

func buildAgentManifestURL(accessURL, ticket string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(accessURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("access URL must be an absolute http or https URL")
	}
	basePath := strings.TrimSuffix(strings.TrimRight(parsed.Path, "/"), "/api/v1")
	parsed.Path = path.Join(basePath, "api/v1/kubernetes/agent-installations", ticket, "manifest.yaml")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func renderAgentManifest(connection domaincluster.Connection, accessURL string) ([]byte, error) {
	token := strings.TrimSpace(metadataString(connection.Metadata, "token"))
	if token == "" {
		return nil, fmt.Errorf("%w: cluster Agent token is unavailable", apperrors.ErrConflict)
	}
	configData, err := yaml.Marshal(map[string]any{
		"app":    map[string]any{"name": "soha-agent", "env": "production"},
		"http":   map[string]any{"addr": ":18080", "base_path": "/api/v1", "read_timeout": "15s", "write_timeout": "15s", "allowed_origins": []string{}},
		"logger": map[string]any{"level": "info", "format": "json"},
		"auth":   map[string]any{"bearer_token": ""},
		"security": map[string]any{"allowed_actions": []string{
			"platform.deployments.restart", "platform.deployments.scale", "platform.deployments.image", "platform.deployments.rollback",
			"platform.statefulsets.restart", "platform.statefulsets.scale", "platform.daemonsets.restart",
		}},
		"audit": map[string]any{"file_path": ""},
		"control_plane": map[string]any{
			"enabled": false, "base_url": strings.TrimRight(accessURL, "/"), "bearer_token": "", "agent_id": connection.Summary.ID,
			"runtime_endpoint": "http://127.0.0.1:18080",
			"session":          map[string]any{"enabled": true, "reconnect_min": "1s", "reconnect_max": "30s", "handshake_timeout": "15s", "max_streams": 64},
		},
		"kubernetes": map[string]any{
			"enabled": true, "id": connection.Summary.ID, "name": connection.Summary.Name, "kubeconfig": "",
			"region": connection.Summary.Region, "environment": connection.Summary.Environment, "labels": connection.Summary.Labels,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("render Agent config: %w", err)
	}

	readRules := []map[string]any{
		{"apiGroups": []string{""}, "resources": []string{"configmaps", "endpoints", "events", "limitranges", "namespaces", "nodes", "persistentvolumeclaims", "persistentvolumes", "pods", "pods/log", "resourcequotas", "secrets", "services"}, "verbs": []string{"get", "list", "watch"}},
		{"apiGroups": []string{"apps"}, "resources": []string{"daemonsets", "deployments", "deployments/scale", "replicasets", "statefulsets", "statefulsets/scale"}, "verbs": []string{"get", "list", "watch"}},
		{"apiGroups": []string{"batch"}, "resources": []string{"cronjobs", "jobs"}, "verbs": []string{"get", "list", "watch"}},
		{"apiGroups": []string{"networking.k8s.io"}, "resources": []string{"ingresses", "networkpolicies"}, "verbs": []string{"get", "list", "watch"}},
		{"apiGroups": []string{"autoscaling"}, "resources": []string{"horizontalpodautoscalers"}, "verbs": []string{"get", "list", "watch"}},
		{"apiGroups": []string{"policy"}, "resources": []string{"poddisruptionbudgets"}, "verbs": []string{"get", "list", "watch"}},
		{"apiGroups": []string{"rbac.authorization.k8s.io"}, "resources": []string{"clusterrolebindings", "clusterroles", "rolebindings", "roles"}, "verbs": []string{"get", "list", "watch"}},
		{"apiGroups": []string{"apiextensions.k8s.io"}, "resources": []string{"customresourcedefinitions"}, "verbs": []string{"get", "list", "watch"}},
		{"apiGroups": []string{"coordination.k8s.io"}, "resources": []string{"leases"}, "verbs": []string{"get", "list", "watch"}},
		{"apiGroups": []string{"gateway.networking.k8s.io"}, "resources": []string{"gateways", "grpcroutes", "httproutes", "referencegrants", "tcproutes", "tlsroutes", "udproutes"}, "verbs": []string{"get", "list", "watch"}},
		{"apiGroups": []string{"metrics.k8s.io"}, "resources": []string{"nodes", "pods"}, "verbs": []string{"get", "list", "watch"}},
		{"apiGroups": []string{"apps"}, "resources": []string{"daemonsets", "deployments", "deployments/scale", "statefulsets", "statefulsets/scale"}, "verbs": []string{"update", "patch"}},
	}
	objects := []map[string]any{
		{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": "soha-agent"}},
		{"apiVersion": "v1", "kind": "ServiceAccount", "metadata": map[string]any{"name": "soha-agent", "namespace": "soha-agent"}},
		{"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRole", "metadata": map[string]any{"name": "soha-agent"}, "rules": readRules},
		{"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRoleBinding", "metadata": map[string]any{"name": "soha-agent"}, "roleRef": map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "soha-agent"}, "subjects": []map[string]any{{"kind": "ServiceAccount", "name": "soha-agent", "namespace": "soha-agent"}}},
		{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "soha-agent-config", "namespace": "soha-agent"}, "data": map[string]any{"agent.config.yaml": string(configData)}},
		{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "soha-agent-secrets", "namespace": "soha-agent"}, "type": "Opaque", "stringData": map[string]any{"agent-bearer-token": token, "control-plane-bearer-token": token}},
		{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "soha-agent", "namespace": "soha-agent"}, "spec": map[string]any{
			"replicas": 1,
			"selector": map[string]any{"matchLabels": map[string]any{"app.kubernetes.io/name": "soha-agent"}},
			"template": map[string]any{
				"metadata": map[string]any{"labels": map[string]any{"app.kubernetes.io/name": "soha-agent", "app.kubernetes.io/part-of": "opensoha"}},
				"spec": map[string]any{"serviceAccountName": "soha-agent", "containers": []map[string]any{{
					"name": "soha-agent", "image": agentImage, "imagePullPolicy": "IfNotPresent",
					"ports": []map[string]any{{"name": "http", "containerPort": 18080}},
					"env": []map[string]any{
						{"name": "SOHA_AGENT_CONFIG_FILE", "value": "/etc/soha-agent/agent.config.yaml"},
						{"name": "SOHA_AGENT_AUTH_BEARER_TOKEN", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "soha-agent-secrets", "key": "agent-bearer-token"}}},
						{"name": "SOHA_AGENT_CONTROL_PLANE_BEARER_TOKEN", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "soha-agent-secrets", "key": "control-plane-bearer-token"}}},
					},
					"volumeMounts":   []map[string]any{{"name": "config", "mountPath": "/etc/soha-agent", "readOnly": true}},
					"readinessProbe": map[string]any{"httpGet": map[string]any{"path": "/api/v1/healthz", "port": "http"}, "initialDelaySeconds": 5, "periodSeconds": 10},
					"livenessProbe":  map[string]any{"httpGet": map[string]any{"path": "/healthz", "port": "http"}, "initialDelaySeconds": 15, "periodSeconds": 20},
					"resources":      map[string]any{"requests": map[string]any{"cpu": "100m", "memory": "128Mi"}, "limits": map[string]any{"cpu": "1", "memory": "512Mi"}},
				}}, "volumes": []map[string]any{{"name": "config", "configMap": map[string]any{"name": "soha-agent-config"}}}},
			},
		}},
	}
	documents := make([]string, 0, len(objects))
	for _, object := range objects {
		document, err := yaml.Marshal(object)
		if err != nil {
			return nil, fmt.Errorf("render Agent manifest: %w", err)
		}
		documents = append(documents, strings.TrimSpace(string(document)))
	}
	return []byte(strings.Join(documents, "\n---\n") + "\n"), nil
}
