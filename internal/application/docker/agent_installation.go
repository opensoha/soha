package docker

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"sigs.k8s.io/yaml"
)

const (
	hostAgentInstallTicketTTL = 15 * time.Minute
	hostAgentEnrollmentTTL    = 15 * time.Minute
	hostAgentReleaseVersion   = "v0.1.5"
	hostRuntimeIPPlaceholder  = "__SOHA_HOST_IP__"
	hostAgentAuthPath         = "/etc/soha-agent/agent.token"
	hostRuntimeAuthPath       = "/etc/soha-agent/runtime.token"
)

var (
	ErrHostAgentInstallationExpired = domaindocker.ErrHostAgentInstallationExpired
	ErrHostAgentEnrollmentConsumed  = domaindocker.ErrHostAgentEnrollmentConsumed
)

type AccessURLResolver interface {
	AccessURL() string
}

func (s *Service) CreateHostAgentInstallation(ctx context.Context, principal domainidentity.Principal, hostID string) (_ domaindocker.HostAgentInstallation, retErr error) {
	defer func() {
		s.recordMutationFailure(ctx, principal, "docker.host.agent.installation", hostID, hostID, retErr, nil)
	}()
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermDockerHostsManage, "update")); err != nil {
		return domaindocker.HostAgentInstallation{}, err
	}
	if s.repo == nil || s.accessURL == nil || s.credentialEncryptionKeys.Active().ID() == "" {
		return domaindocker.HostAgentInstallation{}, fmt.Errorf("%w: Docker Agent installation is unavailable", apperrors.ErrInvalidArgument)
	}
	host, err := s.repo.GetHost(ctx, strings.TrimSpace(hostID))
	if err != nil {
		return domaindocker.HostAgentInstallation{}, err
	}
	accessURL := strings.TrimSpace(s.accessURL.AccessURL())
	if accessURL == "" {
		return domaindocker.HostAgentInstallation{}, fmt.Errorf("%w: configure 访问地址 before generating the Agent installation", apperrors.ErrInvalidArgument)
	}
	if _, err := buildHostAgentInstallerURL(accessURL, "validate"); err != nil {
		return domaindocker.HostAgentInstallation{}, fmt.Errorf("%w: invalid 访问地址", apperrors.ErrInvalidArgument)
	}

	secret, err := newDockerCallbackToken()
	if err != nil {
		return domaindocker.HostAgentInstallation{}, err
	}
	expiresAt := time.Now().UTC().Add(hostAgentInstallTicketTTL)
	now := time.Now().UTC()
	operation, err := s.repo.CreateHostAgentInstallation(ctx, domaindocker.OperationInput{
		HostID: host.ID, OperationKind: OperationKindHostSync, Status: OperationStatusQueued,
		RequestedBy: firstNonEmpty(principal.UserID, principal.UserName), MaxRetries: defaultOperationMaxRetries,
		TimeoutSeconds: defaultOperationTimeout,
	}, domaindocker.HostAgentInstallationState{
		DownloadTokenHash: hostAgentInstallTicketHash(secret), DownloadExpiresAt: expiresAt,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return domaindocker.HostAgentInstallation{}, err
	}
	_ = s.repo.CreateOperationLog(ctx, domaindocker.OperationLog{
		ID: uuid.NewString(), OperationID: operation.ID, LogLevel: "info", Message: "operation queued by control plane",
		Payload: map[string]any{"kind": OperationKindHostSync}, CreatedAt: now,
	})
	ticket := base64.RawURLEncoding.EncodeToString([]byte(operation.ID)) + "." + secret
	scriptURL, err := buildHostAgentInstallerURL(accessURL, ticket)
	if err != nil {
		return domaindocker.HostAgentInstallation{}, fmt.Errorf("build Docker Agent installation URL: %w", err)
	}
	installation := domaindocker.HostAgentInstallation{
		HostID: host.ID, OperationID: operation.ID, ScriptURL: scriptURL,
		Command: "curl -fsSL " + shellSingleQuote(scriptURL) + " | sudo sh", ExpiresAt: expiresAt,
	}
	s.recordOperation(ctx, principal, "docker.host.agent.installation", host.ID, host.Name, "success", "generated Docker Host Agent installation", map[string]any{"operationId": operation.ID})
	return installation, nil
}

func (s *Service) RenderHostAgentInstallation(ctx context.Context, ticket string) ([]byte, error) {
	if s.repo == nil || s.accessURL == nil {
		return nil, fmt.Errorf("%w: Docker Agent installation is unavailable", apperrors.ErrInvalidArgument)
	}
	operationID, downloadSecret, err := parseHostAgentInstallTicket(ticket)
	if err != nil {
		return nil, err
	}
	enrollmentSecret, err := newDockerCallbackToken()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	state, err := s.repo.ConsumeHostAgentInstallTicket(
		ctx, operationID, hostAgentInstallTicketHash(downloadSecret), hostAgentInstallTicketHash(enrollmentSecret),
		now.Add(hostAgentEnrollmentTTL), now,
	)
	if err != nil {
		return nil, err
	}
	return renderHostAgentInstaller(state.HostID, state.OperationID, strings.TrimSpace(s.accessURL.AccessURL()), enrollmentSecret)
}

func parseHostAgentInstallTicket(ticket string) (string, string, error) {
	encodedOperationID, secret, ok := strings.Cut(strings.TrimSpace(ticket), ".")
	if !ok || encodedOperationID == "" || secret == "" {
		return "", "", apperrors.ErrNotFound
	}
	operationID, err := base64.RawURLEncoding.DecodeString(encodedOperationID)
	if err != nil || len(operationID) == 0 {
		return "", "", apperrors.ErrNotFound
	}
	return string(operationID), secret, nil
}

func (s *Service) ExchangeHostAgentEnrollment(ctx context.Context, operationID string, request sohaapi.DockerHostAgentEnrollmentRequest) (sohaapi.DockerHostAgentCredentials, error) {
	operationID = strings.TrimSpace(operationID)
	agentID := strings.TrimSpace(request.AgentID)
	enrollmentToken := strings.TrimSpace(request.EnrollmentToken)
	if operationID == "" || agentID == "" || len(agentID) > 200 || len(enrollmentToken) < 32 || len(enrollmentToken) > 512 {
		return sohaapi.DockerHostAgentCredentials{}, fmt.Errorf("%w: invalid Docker host Agent enrollment", apperrors.ErrInvalidArgument)
	}
	agentToken, err := newDockerCallbackToken()
	if err != nil {
		return sohaapi.DockerHostAgentCredentials{}, err
	}
	runtimeToken, err := newDockerCallbackToken()
	if err != nil {
		return sohaapi.DockerHostAgentCredentials{}, err
	}
	agentTokenCiphertext, err := secretcrypto.EncryptStringWithKeyring(s.credentialEncryptionKeys, agentToken)
	if err != nil {
		return sohaapi.DockerHostAgentCredentials{}, fmt.Errorf("encrypt Docker host Agent credential: %w", err)
	}
	now := time.Now().UTC()
	state, err := s.repo.ExchangeHostAgentEnrollment(ctx, domaindocker.HostAgentEnrollmentExchange{
		OperationID: operationID, AgentID: agentID, EnrollmentTokenHash: hostAgentInstallTicketHash(enrollmentToken),
		AgentTokenCiphertext: agentTokenCiphertext, RuntimeTokenHash: hostAgentInstallTicketHash(runtimeToken), EnrolledAt: now,
	})
	if err != nil {
		return sohaapi.DockerHostAgentCredentials{}, err
	}
	return sohaapi.DockerHostAgentCredentials{
		HostID: state.HostID, OperationID: state.OperationID, AgentID: agentID,
		AgentBearerToken: agentToken, RuntimeBearerToken: runtimeToken, IssuedAt: now,
	}, nil
}

func (s *Service) AuthenticateHostAgent(ctx context.Context, token string) (domaindocker.RunnerAuthorization, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return domaindocker.RunnerAuthorization{}, apperrors.ErrUnauthorized
	}
	state, err := s.repo.GetHostAgentInstallationByRuntimeTokenHash(ctx, hostAgentInstallTicketHash(token))
	if errors.Is(err, apperrors.ErrNotFound) {
		return domaindocker.RunnerAuthorization{}, apperrors.ErrUnauthorized
	}
	if err != nil {
		return domaindocker.RunnerAuthorization{}, err
	}
	return domaindocker.RunnerAuthorization{HostID: state.HostID, AgentID: state.AgentID}, nil
}

func (s *Service) hostAgentBearerToken(ctx context.Context, hostID string) (string, error) {
	state, err := s.repo.GetActiveHostAgentInstallation(ctx, hostID)
	if errors.Is(err, apperrors.ErrNotFound) {
		return s.runtimeBearerToken, nil
	}
	if err != nil {
		return "", err
	}
	token, err := secretcrypto.DecryptStringWithKeyring(s.credentialEncryptionKeys, state.AgentTokenCiphertext)
	if err != nil || strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("%w: Docker host Agent credential is unavailable", apperrors.ErrClusterUnready)
	}
	return token, nil
}

func hostAgentInstallTicketHash(secret string) string {
	digest := sha256.Sum256([]byte(secret))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func buildHostAgentInstallerURL(accessURL, ticket string) (string, error) {
	return buildHostAgentAPIURL(accessURL, ticket, "install.sh")
}

func buildHostAgentEnrollmentURL(accessURL, operationID string) (string, error) {
	return buildHostAgentAPIURL(accessURL, operationID, "enroll")
}

func buildHostAgentAPIURL(accessURL string, segments ...string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(accessURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", fmt.Errorf("access URL must be an absolute http or https URL")
	}
	basePath := strings.TrimSuffix(strings.TrimRight(parsed.Path, "/"), "/api/v1")
	parsed.Path = path.Join(append([]string{basePath, "api/v1/docker/agent-installations"}, segments...)...)
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func renderHostAgentInstaller(hostID, operationID, accessURL, enrollmentToken string) ([]byte, error) {
	enrollmentURL, err := buildHostAgentEnrollmentURL(accessURL, operationID)
	if err != nil {
		return nil, fmt.Errorf("build Docker Agent enrollment URL: %w", err)
	}
	enrollmentRequest, err := json.Marshal(sohaapi.DockerHostAgentEnrollmentRequest{AgentID: hostID, EnrollmentToken: enrollmentToken})
	if err != nil {
		return nil, fmt.Errorf("render Docker Agent enrollment request: %w", err)
	}
	config, err := renderHostAgentConfig(hostID, accessURL)
	if err != nil {
		return nil, err
	}
	encodedConfig := base64.StdEncoding.EncodeToString(config)
	encodedEnrollmentRequest := base64.StdEncoding.EncodeToString(enrollmentRequest)
	script := fmt.Sprintf(`#!/bin/sh
set -eu
umask 077

test "$(id -u)" -eq 0 || { echo "run this installer as root" >&2; exit 1; }
test "$(uname -s)" = "Linux" || { echo "only Linux hosts are supported" >&2; exit 1; }
for command in curl tar sha256sum install systemctl docker ip awk sed base64 jq mktemp mv; do
  command -v "$command" >/dev/null 2>&1 || { echo "$command is required" >&2; exit 1; }
done

case "$(uname -m)" in
  x86_64|amd64) agent_arch=amd64 ;;
  aarch64|arm64) agent_arch=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

agent_version=%s
archive="soha-agent_${agent_version}_linux_${agent_arch}.tar.gz"
download="https://github.com/opensoha/soha-agent/releases/download/${agent_version}/${archive}"
work_dir="$(mktemp -d)"
agent_token_tmp=""
runtime_token_tmp=""
config_tmp=""
cleanup() {
  rm -rf "$work_dir"
  test -z "$agent_token_tmp" || rm -f "$agent_token_tmp"
  test -z "$runtime_token_tmp" || rm -f "$runtime_token_tmp"
  test -z "$config_tmp" || rm -f "$config_tmp"
}
trap cleanup EXIT
curl -fsSL "$download" -o "$work_dir/$archive"
curl -fsSL "$download.sha256" -o "$work_dir/$archive.sha256"
(cd "$work_dir" && sha256sum -c "$archive.sha256")
tar -xzf "$work_dir/$archive" -C "$work_dir"
install -m 0755 "$work_dir/soha-agent" /usr/local/bin/soha-agent

host_ip="$(ip -4 route get 1.1.1.1 | awk '{ for (i = 1; i <= NF; i++) if ($i == "src") { print $(i + 1); exit } }')"
test -n "$host_ip" || { echo "unable to determine the host IPv4 address" >&2; exit 1; }
printf '%%s' %s | base64 -d > "$work_dir/enrollment.json"
curl -fsS -X POST -H 'Content-Type: application/json' --data-binary @"$work_dir/enrollment.json" %s -o "$work_dir/credentials.json"
jq -e --arg host %s --arg operation %s '.data.hostId == $host and .data.operationId == $operation' "$work_dir/credentials.json" >/dev/null

install -d -m 0700 /etc/soha-agent
install -d -m 0755 /var/lib/soha-agent/docker /var/log/soha-agent
agent_token_tmp="$(mktemp /etc/soha-agent/.agent.token.XXXXXX)"
runtime_token_tmp="$(mktemp /etc/soha-agent/.runtime.token.XXXXXX)"
config_tmp="$(mktemp /etc/soha-agent/.config.yaml.XXXXXX)"
umask 077
jq -er '.data.agentBearerToken | select(type == "string" and length >= 32)' "$work_dir/credentials.json" > "$agent_token_tmp"
jq -er '.data.runtimeBearerToken | select(type == "string" and length >= 32)' "$work_dir/credentials.json" > "$runtime_token_tmp"
printf '%%s' %s | base64 -d |
  sed -e "s#%s#${host_ip}#g" > "$config_tmp"
chmod 0600 "$agent_token_tmp" "$runtime_token_tmp" "$config_tmp"
mv -f "$agent_token_tmp" %s
mv -f "$runtime_token_tmp" %s
mv -f "$config_tmp" /etc/soha-agent/config.yaml

cat > /etc/systemd/system/soha-agent.service <<'UNIT'
[Unit]
Description=Soha Agent
Wants=network-online.target docker.service
After=network-online.target docker.service

[Service]
Environment=SOHA_AGENT_CONFIG_FILE=/etc/soha-agent/config.yaml
ExecStart=/usr/local/bin/soha-agent
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now soha-agent
echo "Soha Agent installed; waiting for the host to report to Soha."
`, hostAgentReleaseVersion,
		shellSingleQuote(encodedEnrollmentRequest), shellSingleQuote(enrollmentURL), shellSingleQuote(hostID), shellSingleQuote(operationID),
		shellSingleQuote(encodedConfig), hostRuntimeIPPlaceholder, shellSingleQuote(hostAgentAuthPath), shellSingleQuote(hostRuntimeAuthPath))
	return []byte(script), nil
}

func renderHostAgentConfig(hostID, accessURL string) ([]byte, error) {
	config, err := yaml.Marshal(map[string]any{
		"app":      map[string]any{"name": "soha-agent", "env": "production"},
		"http":     map[string]any{"addr": ":18080", "base_path": "/api/v1", "read_timeout": "15s", "write_timeout": "15s", "allowed_origins": []string{}},
		"logger":   map[string]any{"level": "info", "format": "json"},
		"auth":     map[string]any{"bearer_token_file": hostAgentAuthPath},
		"security": map[string]any{"allowed_actions": []string{}},
		"audit":    map[string]any{"file_path": "/var/log/soha-agent/actions.jsonl"},
		"control_plane": map[string]any{
			"enabled": true, "base_url": strings.TrimRight(accessURL, "/"), "bearer_token_file": hostRuntimeAuthPath,
			"agent_id": hostID, "runtime_endpoint": "http://" + hostRuntimeIPPlaceholder + ":18080",
			"poll_interval": "5s", "max_concurrency": 1, "default_timeout": "30m",
			"callback_retry": map[string]any{"max_attempts": 3, "backoff": "500ms"},
			"provider_kinds": []string{}, "workspace_root": "/var/lib/soha-agent",
			"docker": map[string]any{
				"enabled": true, "worker_id": hostID, "host_ids": []string{hostID},
				"operation_kinds": []string{OperationKindContainerStart, OperationKindProjectDeploy, OperationKindServiceAction, OperationKindPortReserve, OperationKindHostSync},
				"compose_root":    "/var/lib/soha-agent/docker", "poll_interval": "5s",
			},
		},
		"kubernetes": map[string]any{"enabled": false},
	})
	if err != nil {
		return nil, fmt.Errorf("render Docker Agent config: %w", err)
	}
	return config, nil
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
