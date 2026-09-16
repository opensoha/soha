package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"sigs.k8s.io/yaml"
)

type frozenDeliveryProject struct {
	Project       domaindocker.Project `json:"project"`
	ProjectDigest string               `json:"projectDigest"`
}

func dockerDeliveryDigest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func projectConfigurationDigest(project domaindocker.Project) (string, error) {
	return dockerDeliveryDigest(struct {
		HostID, Slug, SourceKind, Compose, Env string
		Config                                 map[string]any
	}{project.HostID, project.Slug, project.SourceKind, project.ComposeContent, project.EnvContent, project.Config})
}

func (s *Service) FreezeDeliveryProject(ctx context.Context, principal domainidentity.Principal, hostID, projectID string) (domaindocker.PreparedDeliveryProject, error) {
	project, err := s.GetProject(ctx, principal, projectID)
	if err != nil {
		return domaindocker.PreparedDeliveryProject{}, err
	}
	if hostID == "" || project.HostID != hostID {
		return domaindocker.PreparedDeliveryProject{}, apperrors.ErrInvalidArgument
	}
	digest, err := projectConfigurationDigest(project)
	if err != nil {
		return domaindocker.PreparedDeliveryProject{}, err
	}
	frozen := frozenDeliveryProject{Project: project, ProjectDigest: digest}
	return s.sealDeliveryProject(frozen, nil)
}

func (s *Service) sealDeliveryProject(frozen frozenDeliveryProject, images map[string]string) (domaindocker.PreparedDeliveryProject, error) {
	encoded, err := json.Marshal(frozen)
	if err != nil {
		return domaindocker.PreparedDeliveryProject{}, err
	}
	ciphertext, err := secretcrypto.EncryptStringWithKeyring(s.credentialEncryptionKeys, string(encoded))
	if err != nil {
		return domaindocker.PreparedDeliveryProject{}, err
	}
	renderedDigest, err := dockerDeliveryDigest(struct{ Compose, Env string }{frozen.Project.ComposeContent, frozen.Project.EnvContent})
	return domaindocker.PreparedDeliveryProject{HostID: frozen.Project.HostID, ProjectID: frozen.Project.ID, ProjectDigest: frozen.ProjectDigest, RenderedDigest: renderedDigest, Ciphertext: ciphertext, ExpectedServices: composeServiceNames(frozen.Project.ComposeContent), Images: images}, err
}

func (s *Service) openDeliveryProject(ciphertext string) (frozenDeliveryProject, error) {
	var frozen frozenDeliveryProject
	plaintext, err := secretcrypto.DecryptStringWithKeyring(s.credentialEncryptionKeys, ciphertext)
	if err != nil {
		return frozen, apperrors.ErrConflict
	}
	if err := json.Unmarshal([]byte(plaintext), &frozen); err != nil {
		return frozen, apperrors.ErrConflict
	}
	return frozen, nil
}

func (s *Service) PrepareDeliveryProject(ctx context.Context, principal domainidentity.Principal, ciphertext string, mappings, artifacts map[string]string) (domaindocker.PreparedDeliveryProject, error) {
	frozen, err := s.openDeliveryProject(ciphertext)
	if err != nil {
		return domaindocker.PreparedDeliveryProject{}, err
	}
	if err := s.validateDeliveryProject(ctx, principal, frozen); err != nil {
		return domaindocker.PreparedDeliveryProject{}, err
	}
	if hasComposeInterpolation(frozen.Project.EnvContent) {
		return domaindocker.PreparedDeliveryProject{}, fmt.Errorf("%w: provide resolved Docker environment values before freezing delivery", apperrors.ErrInvalidArgument)
	}
	compose, images, err := mapDeliveryComposeImages(frozen.Project.ComposeContent, frozen.Project.Slug, mappings, artifacts)
	if err != nil {
		return domaindocker.PreparedDeliveryProject{}, err
	}
	frozen.Project.ComposeContent = compose
	return s.sealDeliveryProject(frozen, images)
}

func (s *Service) validateDeliveryProject(ctx context.Context, principal domainidentity.Principal, frozen frozenDeliveryProject) error {
	if err := s.authorize(ctx, principal, appaccess.PermDockerProjectsDeploy); err != nil {
		return err
	}
	current, err := s.GetProject(ctx, principal, frozen.Project.ID)
	if err != nil {
		return err
	}
	digest, err := projectConfigurationDigest(current)
	if err != nil {
		return err
	}
	if digest != frozen.ProjectDigest || current.HostID != frozen.Project.HostID {
		return fmt.Errorf("%w: Docker project configuration changed; create a new delivery batch", apperrors.ErrConflict)
	}
	host, err := s.GetHost(ctx, principal, current.HostID)
	if err != nil {
		return err
	}
	if host.AgentID == "" || host.DockerVersion == "" || host.ComposeVersion == "" || host.LastHeartbeatAt == nil || time.Since(*host.LastHeartbeatAt) > 2*time.Minute || host.LastHeartbeatAt.After(time.Now().Add(5*time.Second)) || !slices.Contains([]string{"online", "docker_ready"}, host.Status) {
		return fmt.Errorf("%w: Docker host requires a fresh ready Agent heartbeat", apperrors.ErrClusterUnready)
	}
	return nil
}

// The domain queue owns execution. Deterministic IDs also allow a batch to
// recover or cancel an operation after losing its enqueue response.
func deliveryProjectClaim(principal domainidentity.Principal, snapshot sohaapi.DockerDeliverySnapshot, action string) (operationClaim, error) {
	return operationClaimFor("docker.delivery:"+snapshot.ProjectID, principal, "delivery:"+snapshot.DeliveryPlanID+":"+snapshot.TargetID+":"+action, struct{ PlanID, TargetID, Digest, Action string }{snapshot.DeliveryPlanID, snapshot.TargetID, snapshot.RenderedDigest, action})
}

func DeliveryProjectOperationID(principal domainidentity.Principal, snapshot sohaapi.DockerDeliverySnapshot, action string) (string, error) {
	claim, err := deliveryProjectClaim(principal, snapshot, action)
	return claim.id, err
}

func (s *Service) ValidateDeliveryProject(ctx context.Context, principal domainidentity.Principal, snapshot sohaapi.DockerDeliverySnapshot, ciphertext string) error {
	frozen, err := s.openDeliveryProject(ciphertext)
	if err != nil {
		return err
	}
	rendered, err := dockerDeliveryDigest(struct{ Compose, Env string }{frozen.Project.ComposeContent, frozen.Project.EnvContent})
	if err != nil {
		return err
	}
	if frozen.Project.ID != snapshot.ProjectID || frozen.Project.HostID != snapshot.HostID || frozen.ProjectDigest != snapshot.ProjectDigest || rendered != snapshot.RenderedDigest {
		return apperrors.ErrConflict
	}
	return s.validateDeliveryProject(ctx, principal, frozen)
}

func (s *Service) QueueDeliveryProject(ctx context.Context, principal domainidentity.Principal, snapshot sohaapi.DockerDeliverySnapshot, ciphertext, action string) (domaindocker.Operation, error) {
	if !slices.Contains([]string{"validate", "delivery_deploy"}, action) {
		return domaindocker.Operation{}, apperrors.ErrInvalidArgument
	}
	if err := s.ValidateDeliveryProject(ctx, principal, snapshot, ciphertext); err != nil {
		return domaindocker.Operation{}, err
	}
	claim, err := deliveryProjectClaim(principal, snapshot, action)
	if err != nil {
		return domaindocker.Operation{}, err
	}
	if operation, found, err := s.findClaimedOperation(ctx, claim); found || err != nil {
		return operation, err
	}
	node, ok := domainworkflow.NodeExecutionFrom(ctx)
	if !ok || node.Scope != domainworkflow.ScopeDeliveryBatch || action == "validate" && node.Stage != "plan" || action == "delivery_deploy" && node.Stage != "deploy" {
		return domaindocker.Operation{}, apperrors.ErrConflict
	}
	payload := node.Metadata(map[string]any{"action": action, "applicationId": snapshot.ApplicationID, "applicationEnvironmentId": snapshot.ApplicationEnvironmentID, "deliveryPlanId": snapshot.DeliveryPlanID, "releaseTargetId": snapshot.TargetID, "renderedDigest": snapshot.RenderedDigest, "deliveryCiphertext": ciphertext})
	return s.enqueueClaimedOperation(ctx, principal, OperationKindProjectDeploy, snapshot.HostID, snapshot.ProjectID, "", payload, claim)
}

func (s *Service) CancelDeliveryProject(ctx context.Context, operationID, planID, reason string) (domaindocker.Operation, error) {
	node, ok := domainworkflow.NodeExecutionFrom(ctx)
	if !ok || node.Scope != domainworkflow.ScopeDeliveryBatch {
		return domaindocker.Operation{}, apperrors.ErrConflict
	}
	item, err := s.repo.GetOperation(ctx, operationID)
	if err != nil {
		return item, err
	}
	if item.Payload["deliveryPlanId"] != planID || item.Payload["workflowRunId"] != node.RunID || item.Payload["workflowNodeId"] != node.NodeID {
		return item, apperrors.ErrConflict
	}
	if operationTerminal(item.Status) || item.Status == OperationStatusCanceling {
		return item, nil
	}
	return s.cancelOperation(ctx, domainidentity.Principal{UserID: item.RequestedBy}, item.ID, OperationMutationInput{Reason: reason})
}

func (s *Service) hydrateDeliveryOperation(item domaindocker.Operation) (domaindocker.Operation, error) {
	ciphertext, _ := item.Payload["deliveryCiphertext"].(string)
	if ciphertext == "" {
		return item, nil
	}
	frozen, err := s.openDeliveryProject(ciphertext)
	if err != nil {
		return item, err
	}
	if frozen.Project.ID != item.ProjectID || frozen.Project.HostID != item.HostID {
		return item, apperrors.ErrConflict
	}
	item.Payload = mergeMap(item.Payload, buildProjectDeployPayload(frozen.Project, stringValue(item.Payload, "action")))
	delete(item.Payload, "deliveryCiphertext")
	return item, nil
}

func mapDeliveryComposeImages(content, slug string, mappings, artifacts map[string]string) (string, map[string]string, error) {
	invalid := func(message string) (string, map[string]string, error) {
		return "", nil, fmt.Errorf("%w: %s", apperrors.ErrInvalidArgument, message)
	}
	encoded, err := yaml.YAMLToJSONStrict([]byte(content))
	if err != nil {
		return invalid("invalid Compose document")
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		return invalid("invalid Compose object")
	}
	if err := validateDeliveryComposeSources(document, string(encoded)); err != nil {
		return "", nil, err
	}
	if _, ok := document["include"]; ok {
		return invalid("delivery Compose must contain its complete configuration; include is unsupported")
	}
	services, ok := document["services"].(map[string]any)
	if !ok || len(services) == 0 || len(services) > 100 || len(mappings) == 0 {
		return invalid("delivery requires services and explicit image mappings")
	}
	for name := range mappings {
		if _, ok := services[name]; !ok {
			return invalid("image mapping refers to an unknown Compose service")
		}
	}
	images := map[string]string{}
	for name, raw := range services {
		service, ok := raw.(map[string]any)
		if !ok {
			return invalid("invalid Compose service")
		}
		for _, key := range []string{"build", "extends", "profiles"} {
			if _, ok := service[key]; ok {
				return invalid("delivery Compose does not support service " + key + "; materialize configuration and build through continuous delivery")
			}
		}
		image, _ := service["image"].(string)
		if container, mapped := mappings[name]; mapped {
			image = artifacts[container]
		}
		_, digest, pinned := strings.Cut(image, "@")
		immutable, err := domaindelivery.ImmutableImageReference(image, digest)
		if err != nil || !pinned {
			return invalid("every Compose service requires a verified or explicitly pinned immutable image")
		}
		service["image"] = immutable
		images[name] = immutable
	}
	// A fixed name keeps validation in an isolated directory and deployment in
	// the existing workspace bound to the same Docker Compose project identity.
	document["name"] = slug
	encoded, err = json.Marshal(document)
	if err != nil {
		return "", nil, err
	}
	return string(encoded), images, nil
}

func hasComposeInterpolation(value string) bool {
	for i := 0; i+1 < len(value); i++ {
		if value[i] != '$' {
			continue
		}
		next := value[i+1]
		if next == '$' {
			i++
			continue
		}
		if next == '{' || next == '_' || next >= 'a' && next <= 'z' || next >= 'A' && next <= 'Z' {
			return true
		}
	}
	return false
}

func validateDeliveryComposeSources(document map[string]any, encoded string) error {
	if hasComposeInterpolation(encoded) {
		return fmt.Errorf("%w: resolve Compose interpolation before freezing delivery; use $$ for container variables", apperrors.ErrInvalidArgument)
	}
	for _, kind := range []string{"configs", "secrets"} {
		entries, _ := document[kind].(map[string]any)
		for _, entry := range entries {
			value, _ := entry.(map[string]any)
			if value["file"] != nil || value["environment"] != nil {
				return fmt.Errorf("%w: Compose file/environment sources must be resolved before delivery", apperrors.ErrInvalidArgument)
			}
		}
	}
	services, _ := document["services"].(map[string]any)
	for _, entry := range services {
		service, _ := entry.(map[string]any)
		if err := validateFrozenComposeEnvironment(service); err != nil {
			return err
		}
		volumes, _ := service["volumes"].([]any)
		for _, volume := range volumes {
			if relativeComposeBind(volume) {
				return fmt.Errorf("%w: Compose bind mounts require absolute host paths", apperrors.ErrInvalidArgument)
			}
		}
	}
	return nil
}

func relativeComposeBind(volume any) bool {
	if text, ok := volume.(string); ok {
		source, _, found := strings.Cut(text, ":")
		return found && !strings.HasPrefix(source, "/") && (strings.Contains(source, "/") || strings.HasPrefix(source, ".") || strings.HasPrefix(source, "~"))
	}
	value, _ := volume.(map[string]any)
	if value["type"] != "bind" {
		return false
	}
	source, _ := value["source"].(string)
	return !strings.HasPrefix(source, "/")
}

func validateFrozenComposeEnvironment(service map[string]any) error {
	invalid := func() error {
		return fmt.Errorf("%w: Compose environment must be explicit; only the bundled .env file is supported", apperrors.ErrInvalidArgument)
	}
	switch env := service["environment"].(type) {
	case map[string]any:
		for _, value := range env {
			if value == nil {
				return invalid()
			}
		}
	case []any:
		for _, value := range env {
			text, ok := value.(string)
			if !ok || !strings.Contains(text, "=") {
				return invalid()
			}
		}
	}
	if file, ok := service["env_file"]; ok {
		if text, ok := file.(string); ok && text == ".env" {
			return nil
		}
		files, ok := file.([]any)
		if !ok || len(files) != 1 || files[0] != ".env" {
			return invalid()
		}
	}
	return nil
}
