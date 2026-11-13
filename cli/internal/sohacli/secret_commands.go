package sohacli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

var (
	secretAliasPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	secretURIPattern   = regexp.MustCompile(`^soha://secrets/[A-Za-z0-9._-]+(?:/versions/[1-9][0-9]*)?$`)
)

func runSecret(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("secret requires a subcommand: list, get, create, update, disable, versions, references, generate-ssh-key, rotate, or revoke-version")
	}
	switch args[0] {
	case "list":
		return runSecretList(ctx, args[1:], rt)
	case "get":
		return runSecretGet(ctx, args[1:], rt)
	case "create":
		return runSecretCreate(ctx, args[1:], rt)
	case "generate-ssh-key":
		return runSecretCreateAction(ctx, args[1:], rt, true)
	case "update":
		return runSecretUpdate(ctx, args[1:], rt)
	case "disable":
		return runSecretDisable(ctx, args[1:], rt)
	case "versions":
		return runSecretVersions(ctx, args[1:], rt)
	case "references":
		return runSecretReferences(ctx, args[1:], rt)
	case "rotate":
		return runSecretRotate(ctx, args[1:], rt)
	case "revoke-version":
		return runSecretRevokeVersion(ctx, args[1:], rt)
	default:
		return fmt.Errorf("unknown secret command %q", args[0])
	}
}

func runSecretList(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("secret list", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	scopeType := fs.String("scope-type", "", "scope type: workspace, project, or environment")
	scopeID := fs.String("scope-id", "", "scope id")
	output := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if value := strings.TrimSpace(*scopeType); value != "" && value != "workspace" && value != "project" && value != "environment" {
		return fmt.Errorf("invalid --scope-type %q", value)
	}
	format, err := normalizeOutputFormat(*output, "json", "yaml")
	if err != nil {
		return err
	}
	client, err := loadSecretClient(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	items, err := client.ListSecrets(ctx, *scopeType, *scopeID)
	if err != nil {
		return err
	}
	return writeStructuredOutput(rt.Out, format, sanitizeCLIValue(items))
}

func runSecretGet(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("secret get", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	output := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := strings.TrimSpace(fs.Arg(0))
	if id == "" {
		return fmt.Errorf("secret get requires a secret id")
	}
	format, err := normalizeOutputFormat(*output, "json", "yaml")
	if err != nil {
		return err
	}
	client, err := loadSecretClient(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	item, err := client.GetSecret(ctx, id)
	if err != nil {
		return err
	}
	return writeStructuredOutput(rt.Out, format, sanitizeCLIValue(item))
}

func runSecretCreate(ctx context.Context, args []string, rt Runtime) error {
	return runSecretCreateAction(ctx, args, rt, false)
}

func runSecretCreateAction(ctx context.Context, args []string, rt Runtime, generate bool) error {
	fs := newRuntimeFlagSet("secret create", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	name := fs.String("name", "", "secret name")
	description := fs.String("description", "", "secret description")
	scopeType := fs.String("scope-type", "workspace", "scope type: workspace, project, or environment")
	scopeID := fs.String("scope-id", "default", "scope id")
	vaultMount := fs.String("vault-mount", "", "Vault KV v2 mount")
	vaultPath := fs.String("vault-path", "", "Vault KV v2 secret path")
	vaultKey := fs.String("vault-key", "", "Vault KV v2 data key")
	vaultVersion := fs.Int("vault-version", 0, "pinned Vault KV v2 version")
	kind := fs.String("kind", "opaque", "secret kind: opaque, ssh_keypair, ssh_public_key, ssh_password")
	input := fs.String("input", "", "write-only typed JSON request file or - for stdin")
	bindings := repeatableFlag{}
	fs.Var(&bindings, "binding", "binding as type=target or vm:vm_auth|vm_exec=ID or vm_create:vm_cloud_init=ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *input != "" {
		if err := exclusiveSecretJSONFlags(fs); err != nil {
			return err
		}
		if generate {
			return fmt.Errorf("generate-ssh-key uses metadata flags only")
		}
		var request SecretCreateRequest
		if err := readTypedSecretInput(rt, *input, &request); err != nil {
			return err
		}
		client, err := loadSecretClient(ctx, rt, *profileFlag)
		if err != nil {
			return err
		}
		item, err := client.CreateSecret(ctx, request)
		if err != nil {
			return err
		}
		return writePrettyJSON(rt.Out, sanitizeCLIValue(item))
	}
	if strings.TrimSpace(*name) == "" {
		return fmt.Errorf("--name is required")
	}
	if value := strings.TrimSpace(*scopeType); value != "workspace" && value != "project" && value != "environment" {
		return fmt.Errorf("invalid --scope-type %q", value)
	}
	if strings.TrimSpace(*scopeID) == "" {
		return fmt.Errorf("--scope-id is required")
	}
	parsedBindings, err := parseSecretBindings(bindings)
	if err != nil {
		return err
	}
	if generate {
		if *vaultMount != "" || *vaultPath != "" || *vaultKey != "" || *vaultVersion != 0 || *kind != "opaque" {
			return fmt.Errorf("generated SSH keys use the encrypted local store")
		}
		client, err := loadSecretClient(ctx, rt, *profileFlag)
		if err != nil {
			return err
		}
		var item sohaapi.SecretMetadataEnvelope
		err = client.doJSON(ctx, "POST", "/api/v1/secrets/generate-ssh-key", client.Token, nil, sohaapi.SecretGenerateSSHKeyRequest{Name: strings.TrimSpace(*name), Description: *description, ScopeType: sohaapi.SecretScopeType(*scopeType), ScopeID: *scopeID, Bindings: parsedBindings}, &item)
		if err != nil {
			return err
		}
		return writePrettyJSON(rt.Out, sanitizeCLIValue(item.Data))
	}
	if !sohaapi.SecretKind(*kind).Valid() {
		return fmt.Errorf("invalid --kind")
	}
	if (*kind == "ssh_keypair" || *kind == "ssh_public_key") && *vaultMount == "" {
		return fmt.Errorf("SSH key import requires --input file|- with a typed JSON request")
	}
	value, vaultKV2, err := readSecretInput(rt, *vaultMount, *vaultPath, *vaultKey, *vaultVersion)
	if err != nil {
		return err
	}
	client, err := loadSecretClient(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	item, err := client.CreateSecret(ctx, SecretCreateRequest{
		Kind: sohaapi.SecretKind(*kind),
		Name: strings.TrimSpace(*name), Description: strings.TrimSpace(*description), Value: value, VaultKv2: vaultKV2,
		ScopeType: sohaapi.SecretScopeType(strings.TrimSpace(*scopeType)), ScopeID: strings.TrimSpace(*scopeID), Bindings: parsedBindings,
	})
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCLIValue(item))
}

func runSecretUpdate(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("secret update", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	name := fs.String("name", "", "new secret name")
	description := fs.String("description", "", "new secret description")
	status := fs.String("status", "", "new status: active or disabled")
	bindings := repeatableFlag{}
	fs.Var(&bindings, "binding", "binding as capability|project|connection=target, repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := strings.TrimSpace(fs.Arg(0))
	if id == "" {
		return fmt.Errorf("secret update requires a secret id")
	}
	if strings.TrimSpace(*name) == "" && strings.TrimSpace(*description) == "" && strings.TrimSpace(*status) == "" && len(bindings) == 0 {
		return fmt.Errorf("secret update requires at least one changed field")
	}
	if value := strings.TrimSpace(*status); value != "" && value != "active" && value != "disabled" {
		return fmt.Errorf("invalid --status %q", value)
	}
	parsedBindings, err := parseSecretBindings(bindings)
	if err != nil {
		return err
	}
	client, err := loadSecretClient(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	item, err := client.UpdateSecret(ctx, id, SecretUpdateRequest{
		Name: strings.TrimSpace(*name), Description: strings.TrimSpace(*description), Status: sohaapi.SecretStatus(strings.TrimSpace(*status)), Bindings: parsedBindings,
	})
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCLIValue(item))
}

func runSecretDisable(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("secret disable", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := strings.TrimSpace(fs.Arg(0))
	if id == "" {
		return fmt.Errorf("secret disable requires a secret id")
	}
	client, err := loadSecretClient(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	if err := client.DisableSecret(ctx, id); err != nil {
		return err
	}
	_, err = fmt.Fprintf(rt.Out, "Disabled secret %s\n", id)
	return err
}

func runSecretVersions(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("secret versions", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	output := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := strings.TrimSpace(fs.Arg(0))
	if id == "" {
		return fmt.Errorf("secret versions requires a secret id")
	}
	format, err := normalizeOutputFormat(*output, "json", "yaml")
	if err != nil {
		return err
	}
	client, err := loadSecretClient(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	items, err := client.ListSecretVersions(ctx, id)
	if err != nil {
		return err
	}
	return writeStructuredOutput(rt.Out, format, sanitizeCLIValue(items))
}

func runSecretReferences(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("secret references", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	output := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := strings.TrimSpace(fs.Arg(0))
	if id == "" {
		return fmt.Errorf("secret references requires a secret id")
	}
	format, err := normalizeOutputFormat(*output, "json", "yaml")
	if err != nil {
		return err
	}
	client, err := loadSecretClient(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	var items sohaapi.SecretReferenceUsageListEnvelope
	err = client.doJSON(ctx, "GET", "/api/v1/secrets/"+url.PathEscape(id)+"/references", client.Token, nil, nil, &items)
	if err != nil {
		return err
	}
	return writeStructuredOutput(rt.Out, format, sanitizeCLIValue(items))
}

func runSecretRotate(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("secret rotate", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	vaultMount := fs.String("vault-mount", "", "Vault KV v2 mount")
	vaultPath := fs.String("vault-path", "", "Vault KV v2 secret path")
	vaultKey := fs.String("vault-key", "", "Vault KV v2 data key")
	vaultVersion := fs.Int("vault-version", 0, "pinned Vault KV v2 version")
	input := fs.String("input", "", "write-only typed rotation JSON file or - for stdin")
	id := ""
	flagArgs := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if strings.HasPrefix(arg, "-") {
			flagArgs = append(flagArgs, arg)
			if !strings.Contains(arg, "=") && !isHelpArg(arg) && index+1 < len(args) {
				index++
				flagArgs = append(flagArgs, args[index])
			}
			continue
		}
		if id != "" {
			return fmt.Errorf("secret rotate requires exactly one secret id")
		}
		id = strings.TrimSpace(arg)
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("secret rotate requires a secret id")
	}
	request := SecretRotateRequest{}
	if *input != "" {
		if err := exclusiveSecretJSONFlags(fs); err != nil {
			return err
		}
		if err := readTypedSecretInput(rt, *input, &request); err != nil {
			return err
		}
	} else {
		value, vaultKV2, err := readSecretInput(rt, *vaultMount, *vaultPath, *vaultKey, *vaultVersion)
		if err != nil {
			return err
		}
		request.Value, request.VaultKv2 = value, vaultKV2
	}
	client, err := loadSecretClient(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	version, err := client.RotateSecret(ctx, id, request)
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCLIValue(version))
}

func runSecretRevokeVersion(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("secret revoke-version", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := strings.TrimSpace(fs.Arg(0))
	version, err := strconv.Atoi(strings.TrimSpace(fs.Arg(1)))
	if id == "" || err != nil || version < 1 {
		return fmt.Errorf("secret revoke-version requires a secret id and positive version")
	}
	client, err := loadSecretClient(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	item, err := client.RevokeSecretVersion(ctx, id, version)
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCLIValue(item))
}

func loadSecretClient(ctx context.Context, rt Runtime, profileName string) (APIClient, error) {
	_, _, profile, err := loadRuntimeProfile(ctx, rt, profileName)
	if err != nil {
		return APIClient{}, err
	}
	return gatewayClient(rt, profile), nil
}

func readSecretValue(rt Runtime) (string, error) {
	if _, err := fmt.Fprint(rt.Err, "Secret value: "); err != nil {
		return "", err
	}
	value, err := readHiddenLine(rt, false)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("secret value must not be empty")
	}
	return value, nil
}

func readSecretInput(rt Runtime, mount, path, key string, version int) (string, *SecretVaultKV2Reference, error) {
	if mount == "" && path == "" && key == "" && version == 0 {
		value, err := readSecretValue(rt)
		return value, nil, err
	}
	if mount == "" || path == "" || strings.TrimSpace(key) == "" || version < 1 {
		return "", nil, fmt.Errorf("--vault-mount, --vault-path, --vault-key, and a positive --vault-version must be provided together")
	}
	return "", &SecretVaultKV2Reference{Mount: mount, Path: path, Key: key, Version: version}, nil
}

func parseSecretBindings(values []string) ([]SecretBinding, error) {
	bindings := make([]SecretBinding, 0, len(values))
	for _, value := range values {
		targetType, targetRef, ok := strings.Cut(value, "=")
		targetType, targetRef = strings.TrimSpace(targetType), strings.TrimSpace(targetRef)
		targetType, purpose, _ := strings.Cut(targetType, ":")
		valid := sohaapi.SecretBindingTargetType(targetType).Valid()
		switch targetType {
		case "vm":
			valid = purpose == "vm_auth" || purpose == "vm_exec"
		case "vm_create":
			valid = purpose == "vm_cloud_init"
		default:
			valid = valid && purpose == ""
		}
		if !ok || targetRef == "" || !valid {
			return nil, fmt.Errorf("invalid --binding; use capability|project|connection=target, vm:vm_auth|vm_exec=ID, or vm_create:vm_cloud_init=ID")
		}
		bindings = append(bindings, SecretBinding{TargetType: sohaapi.SecretBindingTargetType(targetType), TargetRef: targetRef, Purpose: sohaapi.SecretPurpose(purpose)})
	}
	return bindings, nil
}

func readTypedSecretInput(rt Runtime, file string, out any) error {
	value, err := readJSONInput(rt, file, "")
	if err != nil {
		return fmt.Errorf("cannot read secret JSON input")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("invalid secret JSON input")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		// Decoder errors may quote a value or an unknown key containing secret material.
		return fmt.Errorf("invalid typed secret JSON request")
	}
	return nil
}

func exclusiveSecretJSONFlags(fs *flag.FlagSet) error {
	var err error
	fs.Visit(func(value *flag.Flag) {
		if value.Name != "input" && value.Name != "profile" {
			err = fmt.Errorf("--input cannot be combined with secret request field flags")
		}
	})
	return err
}

func parseSecretRefFlags(values []string) (map[string]string, error) {
	refs := make(map[string]string, len(values))
	for _, value := range values {
		alias, uri, ok := strings.Cut(value, "=")
		alias, uri = strings.TrimSpace(alias), strings.TrimSpace(uri)
		if !ok || !secretAliasPattern.MatchString(alias) || !secretURIPattern.MatchString(uri) {
			return nil, fmt.Errorf("invalid --secret-ref %q; use ALIAS=soha://secrets/ID[/versions/N]", value)
		}
		if _, duplicate := refs[alias]; duplicate {
			return nil, fmt.Errorf("duplicate secret alias %q", alias)
		}
		refs[alias] = uri
	}
	return refs, nil
}

func validateSecretRefs(refs map[string]string) error {
	if len(refs) > 64 {
		return fmt.Errorf("secretRefs must contain at most 64 entries")
	}
	for alias, uri := range refs {
		if !secretAliasPattern.MatchString(alias) || !secretURIPattern.MatchString(strings.TrimSpace(uri)) {
			return fmt.Errorf("invalid secretRef %q", alias)
		}
	}
	return nil
}
