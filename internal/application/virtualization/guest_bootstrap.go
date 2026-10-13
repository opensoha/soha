package virtualization

import (
	"context"
	"encoding/json"
	"fmt"
	"go.yaml.in/yaml/v3"
	"io"
	"path"
	"regexp"
	"strings"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var guestUsernamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

func (s *Service) prepareGuestBootstrap(ctx context.Context, principal domainidentity.Principal, prepared *preparedVMCreate) (string, []domainsecret.Reference, error) {
	access := prepared.input.GuestAccess
	if access == nil {
		return prepared.input.CloudInit, nil, nil
	}
	if !guestUsernamePattern.MatchString(access.Username) || access.Username == "root" || len(access.PublicKeyRefs) == 0 || len(access.PublicKeyRefs) > 16 {
		return "", nil, fmt.Errorf("%w: a non-root guest account and 1–16 public key references are required", apperrors.ErrInvalidArgument)
	}
	bootstrap, err := guestBootstrapFields(prepared.input.CloudInit)
	if err != nil {
		return "", nil, err
	}
	for _, key := range []string{"sshkeys", "ciuser", "cicustom", "cipassword"} {
		if prepared.input.ProviderParams[key] != nil {
			return "", nil, fmt.Errorf("%w: managed guest access owns native %s", apperrors.ErrInvalidArgument, key)
		}
	}
	if prepared.image.ID == "" || prepared.image.Config["cloudInitSupported"] != true || s.guestCredentials == nil {
		return "", nil, fmt.Errorf("%w: managed initialization requires a catalog image with verified cloud-init support and Secret Store", apperrors.ErrConflict)
	}
	if prepared.connection.Provider == ProviderPVE {
		adapter, connection, err := s.adapterForConnection(ctx, prepared.connection)
		if err != nil {
			return "", nil, err
		}
		proof, ok := adapter.(domain.VMGuestBootstrapProvider)
		if !ok {
			return "", nil, apperrors.ErrUnsupportedOperation
		}
		input := domain.AdapterCreateVMInput{Node: firstNonEmpty(prepared.input.Node, stringValue(prepared.image.Config, "node")), TemplateID: prepared.input.TemplateID, SourceRef: prepared.sourceRef, SourceMode: firstNonEmpty(prepared.input.SourceMode, sourceModeForProvider(ProviderPVE, prepared.input.TemplateID, prepared.image)), DiskSize: diskString(prepared.input.DiskGiB), ProviderParams: prepared.input.ProviderParams}
		prepared.guestSourceProof, err = proof.ValidateGuestBootstrap(ctx, connection, input)
		if err != nil {
			return "", nil, err
		}
	}
	refs := make(map[string]string, len(access.PublicKeyRefs))
	seen := map[string]bool{}
	for i, ref := range access.PublicKeyRefs {
		if seen[ref] {
			return "", nil, apperrors.ErrInvalidArgument
		}
		seen[ref] = true
		refs[fmt.Sprintf("SSH_KEY_%d", i)] = ref
	}
	pinned, keys, err := s.guestCredentials.PinSSHPublicKeys(ctx, principal, refs, domainsecret.Target{Type: "vm_create", Ref: prepared.connection.ID, Purpose: "vm_cloud_init"})
	if err != nil {
		return "", nil, err
	}
	public := make([]string, 0, len(keys))
	prepared.guestPublicKeys = keys
	for _, key := range keys {
		public = append(public, key.AuthorizedKey)
	}
	content, err := managedGuestCloudInit(access.Username, public, bootstrap)
	return content, pinned, err
}

func managedGuestCloudInit(username string, publicKeys []string, bootstrap map[string]any) (string, error) {
	// JSON is valid YAML; typed serialization prevents key text becoming configuration.
	if bootstrap == nil {
		bootstrap = map[string]any{}
	}
	bootstrap["users"] = []any{"default", map[string]any{"name": username, "lock_passwd": true, "shell": "/bin/bash", "ssh_authorized_keys": publicKeys}}
	bootstrap["ssh_pwauth"], bootstrap["disable_root"] = false, true
	content, err := json.Marshal(bootstrap)
	if err != nil {
		return "", err
	}
	return "#cloud-config\n" + string(content) + "\n", nil
}

func guestBootstrapFields(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	invalid := fmt.Errorf("%w: managed initialization requires one bounded cloud-config without account or SSH fields", apperrors.ErrInvalidArgument)
	if len(raw) > 1<<20 || !strings.HasPrefix(strings.TrimSpace(raw), "#cloud-config\n") {
		return nil, invalid
	}
	decoder := yaml.NewDecoder(strings.NewReader(raw))
	var node, trailing yaml.Node
	if decoder.Decode(&node) != nil || len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return nil, invalid
	}
	if decoder.Decode(&trailing) != io.EOF {
		return nil, invalid
	}
	remaining := 8192
	if !validGuestYAML(&node, 0, &remaining) {
		return nil, invalid
	}
	var fields map[string]any
	if node.Decode(&fields) != nil {
		return nil, invalid
	}
	for key := range fields {
		switch key {
		case "packages", "package_update", "package_upgrade", "timezone", "locale", "hostname", "fqdn", "manage_etc_hosts", "write_files", "runcmd", "bootcmd", "growpart", "resize_rootfs", "apt", "yum_repos":
		default:
			return nil, invalid
		}
	}
	if !validGuestWriteFiles(fields["write_files"]) {
		return nil, invalid
	}
	if _, err := json.Marshal(fields); err != nil {
		return nil, invalid
	}
	return fields, nil
}

func validGuestWriteFiles(raw any) bool {
	if raw == nil {
		return true
	}
	files, ok := raw.([]any)
	if !ok {
		return false
	}
	for _, item := range files {
		file, ok := item.(map[string]any)
		if !ok {
			return false
		}
		name, ok := file["path"].(string)
		if !ok || !path.IsAbs(name) {
			return false
		}
		name = path.Clean(name)
		for _, protected := range []string{"/etc/ssh", "/etc/cloud", "/var/lib/cloud", "/etc/passwd", "/etc/shadow", "/etc/sudoers", "/etc/sudoers.d"} {
			if name == protected || strings.HasPrefix(name, protected+"/") {
				return false
			}
		}
		if strings.Contains(name+"/", "/.ssh/") {
			return false
		}
	}
	return true
}

func validGuestYAML(node *yaml.Node, depth int, remaining *int) bool {
	*remaining -= 1
	if depth > 32 || *remaining < 0 || node.Kind == yaml.AliasNode || node.Anchor != "" {
		return false
	}
	switch node.Tag {
	case "", "!!map", "!!seq", "!!str", "!!int", "!!float", "!!bool", "!!null":
	default:
		return false
	}
	for i, child := range node.Content {
		if node.Kind == yaml.MappingNode && i%2 == 0 && child.Tag != "!!str" {
			return false
		}
		if !validGuestYAML(child, depth+1, remaining) {
			return false
		}
	}
	return true
}

func (s *Service) validateQueuedGuestReferences(ctx context.Context, principal domainidentity.Principal, task domain.Task) error {
	if task.Payload["guestAccess"] == nil {
		return nil
	}
	if s.guestCredentials == nil {
		return apperrors.ErrAccessDenied
	}
	var refs []domainsecret.Reference
	decodePayloadValue(task.Payload["guestPublicKeyRefs"], &refs)
	if len(refs) == 0 {
		return apperrors.ErrAccessDenied
	}
	pinned := make(map[string]string, len(refs))
	for _, ref := range refs {
		if ref.Version < 1 {
			return apperrors.ErrAccessDenied
		}
		pinned[ref.Alias] = ref.URI
	}
	_, _, err := s.guestCredentials.PinSSHPublicKeys(ctx, principal, pinned, domainsecret.Target{Type: "vm_create", Ref: task.ConnectionID, Purpose: "vm_cloud_init"})
	if err != nil {
		return err
	}
	if task.Provider == ProviderPVE {
		var frozen domain.VMIdentity
		decodePayloadValue(task.Payload["guestSourceProof"], &frozen)
		if frozen.Generation == "" {
			return apperrors.ErrAccessDenied
		}
		connection, err := s.connections.GetConnection(ctx, task.ConnectionID)
		if err != nil {
			return err
		}
		adapter, providerConnection, err := s.adapterForConnection(ctx, connection)
		if err != nil {
			return err
		}
		proof, ok := adapter.(domain.VMGuestBootstrapProvider)
		if !ok {
			return apperrors.ErrUnsupportedOperation
		}
		input := adapterCreateVMInput(task.Payload)
		input.Node, input.SourceRef = frozen.Node, frozen.ProviderVMID
		observed, err := proof.ValidateGuestBootstrap(ctx, providerConnection, input)
		if err != nil {
			return err
		}
		if observed.Generation != frozen.Generation || observed.Name != frozen.Name {
			return fmt.Errorf("%w: cloud-init source generation changed", apperrors.ErrConflict)
		}
	}
	return nil
}
