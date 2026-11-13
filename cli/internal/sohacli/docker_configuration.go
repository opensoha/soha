package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

var dockerConfigurationKey = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

type dockerConfigurationCommand struct {
	method, path, resource, id, action, key string
	body                                    any
	conditions                              *sohaapi.DockerConfigurationPreconditions
}

func runDockerConfiguration(ctx context.Context, resource, action string, args []string, rt Runtime) error {
	leading, rest := extractLeadingPositionals(args, 1)
	fs := newRuntimeFlagSet("docker "+resource+" "+action, rest, rt)
	profile := fs.String("profile", "", "profile name")
	output := fs.String("output", "json", "output format: json or yaml")
	file := fs.String("input", "", "reviewed typed JSON file, or - for stdin")
	key := fs.String("idempotency-key", "", "stable original mutation key; must match the input when present")
	yes := fs.Bool("yes", false, "confirm the frozen target, UID, revision and input")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	ids := append(leading, fs.Args()...)
	req, err := prepareDockerConfiguration(resource, action, ids)
	if err != nil {
		return err
	}
	allowed := []string{}
	if req.body != nil {
		allowed = append(allowed, "input", "idempotency-key", "yes")
		if err := readDockerConfigurationJSON(rt, *file, req.body); err != nil {
			return err
		}
		if err := bindDockerConfigurationKey(&req, *key); err != nil {
			return err
		}
	}
	if err := checkWorkbenchFlags(fs, allowed...); err != nil {
		return err
	}
	format, client, err := computeOutputClient(ctx, rt, *profile, *output)
	if err != nil {
		return err
	}
	client.Client = client.httpClient()
	client.Client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if req.body != nil {
		prompt := "Submit Docker " + terminalLabel(resource) + " " + terminalLabel(action) + " " + terminalLabel(req.id) + " with original key " + req.key
		if req.conditions != nil {
			prompt += " UID " + req.conditions.ExpectedUID.String() + " revision " + req.conditions.ExpectedConfigurationRevision.String()
		}
		if err := confirmWorkbenchAction(rt, *yes, prompt+" using the reviewed input?"); err != nil {
			return err
		}
	}
	var result sohaapi.DockerConfigurationReceiptEnvelope
	if err := client.doJSON(ctx, req.method, req.path, client.Token, nil, req.body, &result); err != nil {
		if req.body != nil {
			return fmt.Errorf("%w; query docker configuration-requests get %s before explicitly retrying the same input and key; no automatic replay", err, req.key)
		}
		return err
	}
	if err := validateDockerConfigurationReceipt(req, result.Data); err != nil {
		return err
	}
	return writeWorkbenchOutput(rt, client, format, result)
}

func prepareDockerConfiguration(resource, action string, ids []string) (dockerConfigurationCommand, error) {
	req := dockerConfigurationCommand{method: http.MethodPost, resource: resource, action: action}
	if resource == "configuration-requests" && action == "get" {
		if len(ids) != 1 || !dockerConfigurationKey.MatchString(ids[0]) {
			return req, usageError{message: "configuration receipt lookup requires one original request key"}
		}
		req.method, req.key, req.path = http.MethodGet, ids[0], "/api/v1/docker/configuration-requests/"+url.PathEscape(ids[0])
		return req, nil
	}
	if resource == "hosts" && action == "register" {
		if len(ids) != 0 {
			return req, usageError{message: "host registration does not accept a caller resource ID"}
		}
		req.body, req.path = &sohaapi.DockerHostRegistrationInput{}, "/api/v1/docker/hosts/registrations"
		return req, nil
	}
	if (resource != "hosts" && resource != "projects") || (action != "update" && action != "delete") || len(ids) != 1 || !kubernetesSegment(ids[0]) {
		return req, usageError{message: "conditional Docker update/delete requires hosts or projects and one resource ID"}
	}
	req.id, req.path = ids[0], "/api/v1/docker/"+resource+"/"+url.PathEscape(ids[0])+"/configuration"
	if action == "update" {
		req.method, req.body = http.MethodPatch, &sohaapi.DockerConfigurationPatchInput{}
	} else {
		req.path += "-deletion"
		req.body = &sohaapi.DockerConfigurationDeleteInput{}
	}
	return req, nil
}

func bindDockerConfigurationKey(req *dockerConfigurationCommand, key string) error {
	var original *string
	switch body := req.body.(type) {
	case *sohaapi.DockerHostRegistrationInput:
		original = &body.IdempotencyKey
	case *sohaapi.DockerConfigurationPatchInput:
		original = &body.IdempotencyKey
		req.conditions = &body.Preconditions
	case *sohaapi.DockerConfigurationDeleteInput:
		original = &body.IdempotencyKey
		req.conditions = &body.Preconditions
	default:
		return usageError{message: "unsupported configuration request"}
	}
	if key != "" && *original != "" && key != *original {
		return usageError{message: "input and flag must use the same original idempotency key"}
	}
	if key != "" {
		*original = key
	}
	if !dockerConfigurationKey.MatchString(*original) {
		return usageError{message: "provide an original idempotency key of 8..128 ASCII letters, digits, dot, underscore, colon or hyphen"}
	}
	req.key = *original
	if req.conditions != nil && (req.conditions.ExpectedUID == [16]byte{} || req.conditions.ExpectedConfigurationRevision == [16]byte{}) {
		return usageError{message: "conditional mutation requires the reviewed nonempty UID and configuration revision; read the identity first"}
	}
	return nil
}

func validateDockerConfigurationReceipt(req dockerConfigurationCommand, item sohaapi.DockerConfigurationReceipt) error {
	valid := item.RequestKey == req.key && item.ID != "" && item.Status == "committed" && item.Resource.ID != "" && item.Resource.UID != [16]byte{} && item.Resource.ConfigurationRevision != [16]byte{}
	valid = valid && validDockerConfigurationResource(item.Resource)
	valid = valid && (item.Action == "register" || item.Action == "update" || item.Action == "delete") && item.Deleted == (item.Action == "delete")
	if req.body != nil {
		action := req.action
		if action == "register" {
			valid = valid && item.Resource.ResourceKind == "docker.host"
		} else {
			valid = valid && item.Resource.ID == req.id && string(item.Resource.ResourceKind) == "docker."+map[string]string{"hosts": "host", "projects": "project"}[req.resource]
		}
		valid = valid && string(item.Action) == action
	}
	if !valid {
		return fmt.Errorf("docker configuration receipt does not match the original request; no result identity was synthesized")
	}
	return nil
}

func validDockerConfigurationResource(item sohaapi.DockerResourceIdentity) bool {
	switch item.ResourceKind {
	case "docker.host":
		return item.HostID == ""
	case "docker.project":
		return item.HostID != ""
	default:
		return false
	}
}

func readDockerConfigurationJSON(rt Runtime, file string, out any) error {
	var raw json.RawMessage
	if err := readTypedWorkbenchInput(rt, file, "", &raw); err != nil {
		return err
	}
	invalid := usageError{message: "configuration input must be one bounded typed JSON object without duplicate keys or null values"}
	if len(raw) > 2<<20 {
		return invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if validateDockerConfigurationJSON(decoder, 0) != nil || decoder.Decode(new(any)) != io.EOF {
		return invalid
	}
	if validateDockerConfigurationTypedKeys(raw, reflect.TypeOf(out)) != nil {
		return invalid
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil || decoder.Decode(new(any)) != io.EOF {
		return invalid
	}
	return nil
}

func validateDockerConfigurationTypedKeys(raw json.RawMessage, target reflect.Type) error {
	invalid := usageError{message: "configuration keys must use the exact public field spelling"}
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	if reflect.PointerTo(target).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		return nil
	}
	switch target.Kind() {
	case reflect.Struct:
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil {
			return invalid
		}
		fields := map[string]reflect.Type{}
		for index := range target.NumField() {
			field := target.Field(index)
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if field.IsExported() && name != "-" && name != "" {
				fields[name] = field.Type
			}
		}
		for name, value := range object {
			field, known := fields[name]
			if !known || validateDockerConfigurationTypedKeys(value, field) != nil {
				return invalid
			}
		}
	case reflect.Slice:
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) != nil {
			return invalid
		}
		for _, value := range values {
			if validateDockerConfigurationTypedKeys(value, target.Elem()) != nil {
				return invalid
			}
		}
	}
	return nil
}

func validateDockerConfigurationJSON(decoder *json.Decoder, depth int) error {
	invalid := usageError{message: "invalid configuration JSON"}
	if depth > 32 {
		return invalid
	}
	token, err := decoder.Token()
	if err != nil || token == nil {
		return invalid
	}
	switch token {
	case json.Delim('{'):
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return invalid
			}
			seen[name] = true
			if validateDockerConfigurationJSON(decoder, depth+1) != nil {
				return invalid
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return invalid
		}
	case json.Delim('['):
		for decoder.More() {
			if validateDockerConfigurationJSON(decoder, depth+1) != nil {
				return invalid
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return invalid
		}
	}
	return nil
}
