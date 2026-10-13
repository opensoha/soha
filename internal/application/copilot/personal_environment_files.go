package copilot

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Base64 plus its line wrapping and envelope fits the Agent's 64 KiB output.
const personalFileLimit = 44 << 10

func nativePersonalEnvironmentBindings() []domain.AgentToolBinding {
	return []domain.AgentToolBinding{
		{ID: "personal.environment.inspect", ToolName: "personal.environment.inspect", ToolKind: "internal_api", AdapterID: "personal.environment.v1", PermissionKey: appaccess.PermObserveAIChatUse},
		{ID: "personal.environment.files.read", ToolName: "personal.environment.files.read", ToolKind: "internal_api", AdapterID: "personal.environment.v1", PermissionKey: appaccess.PermObserveAIChatUse},
	}
}

// The caller supplies a relative data path as a positional argument, never a
// shell fragment. Check the opened descriptor, then read from that same inode;
// replacing a symlink after the check cannot redirect the read. No file content
// is logged or persisted here. An extra byte detects oversized/growing files.
const personalFileReadScript = `[ -f "$1" ] || { printf 'missing\n'; exit 0; }
exec 3< "$1"
resolved=$(readlink -f "/proc/$$/fd/3") || { printf 'denied\n'; exit 0; }
case "$resolved" in /data/*) ;; *) printf 'denied\n'; exit 0 ;; esac
[ -f "/proc/$$/fd/3" ] || { printf 'denied\n'; exit 0; }
printf 'ok\n'
head -c 45057 <&3 | base64`

func personalFilePath(path string) (string, error) {
	if path == "" || len(path) > 1024 || !utf8.ValidString(path) || strings.ContainsAny(path, "\x00\r\n\\") {
		return "", apperrors.ErrInvalidArgument
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return "", apperrors.ErrInvalidArgument
		}
	}
	return "/data/" + path, nil
}

func personalFileToolArguments(input map[string]any) (map[string]any, error) {
	path, valid := input["path"].(string)
	if !valid || len(input) != 1 {
		return nil, apperrors.ErrInvalidArgument
	}
	if _, err := personalFilePath(path); err != nil {
		return nil, err
	}
	return map[string]any{"path": path}, nil
}

func (s *Service) ReadPersonalEnvironmentFile(ctx context.Context, principal domainidentity.Principal, path string) (domain.PersonalEnvironmentFile, error) {
	absolute, err := personalFilePath(path)
	if err != nil {
		return domain.PersonalEnvironmentFile{}, err
	}
	item, err := s.personalEnvironmentForChange(ctx, principal, "reconcile", "", "en-US")
	if err != nil {
		return domain.PersonalEnvironmentFile{}, err
	}
	if item.State != "ready" || !item.Lease.ExpiresAt.After(time.Now()) {
		return domain.PersonalEnvironmentFile{}, apperrors.ErrConflict
	}
	if item.Backend == "vm" {
		return s.readPersonalVMFile(ctx, principal, item, path)
	}
	request := contractenv.Request{SchemaVersion: contractenv.RequestVersion, Action: "execute", Lease: item.Lease, Argv: []string{"/bin/sh", "-ec", personalFileReadScript, "personal-file-read", absolute}}
	view, result, err := s.dispatchPersonalEnvironment(ctx, principal, "file_read", item, request)
	if err != nil {
		return domain.PersonalEnvironmentFile{}, err
	}
	content, err := personalFileContent(*result.Output)
	if err != nil {
		return domain.PersonalEnvironmentFile{}, err
	}
	return domain.PersonalEnvironmentFile{AssistantID: item.AssistantID, EnvironmentVersion: view.Version, Path: path, Content: content, ObservedAt: result.ObservedAt}, nil
}

func (s *Service) readPersonalVMFile(ctx context.Context, principal domainidentity.Principal, item domain.PersonalEnvironmentRecord, path string) (domain.PersonalEnvironmentFile, error) {
	if s.personalEnvironment.InvokeVM == nil {
		return domain.PersonalEnvironmentFile{}, apperrors.ErrServiceUnavailable
	}
	request := contractenv.Request{SchemaVersion: contractenv.RequestVersion, Action: "inspect", Lease: item.Lease}
	reserved, err := s.personalEnvironment.Store.ReservePersonalEnvironment(ctx, item, request.Lease, uuid.NewString(), false)
	if err != nil {
		return domain.PersonalEnvironmentFile{}, err
	}
	result, invokeErr := s.personalEnvironment.InvokeVM(ctx, principal, reserved, guest.RuntimeOperation{Kind: "file_read", Request: request, File: &guest.RuntimeFileInput{Path: path}})
	if errors.Is(invokeErr, domain.ErrPersonalEnvironmentNotDispatched) {
		_, err := s.abortPersonalEnvironment(ctx, principal, "file_read", reserved, item, invokeErr)
		return domain.PersonalEnvironmentFile{}, err
	}
	if invokeErr == nil && (result.File == nil || result.File.Path != path || result.Environment.ValidateFor(request) != nil) {
		invokeErr = apperrors.ErrConflict
	}
	applyPersonalEnvironmentReceipt(&reserved, result.Environment, invokeErr)
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	completed, err := s.personalEnvironment.Store.CompletePersonalEnvironment(commitCtx, reserved)
	if err != nil {
		return domain.PersonalEnvironmentFile{}, err
	}
	if invokeErr != nil {
		return domain.PersonalEnvironmentFile{}, invokeErr
	}
	data, err := base64.StdEncoding.DecodeString(result.File.ContentBase64)
	if err != nil || len(data) > personalFileLimit || !utf8.Valid(data) {
		return domain.PersonalEnvironmentFile{}, apperrors.ErrServiceUnavailable
	}
	return domain.PersonalEnvironmentFile{AssistantID: item.AssistantID, EnvironmentVersion: completed.Version, Path: path, Content: string(data), ObservedAt: result.Environment.ObservedAt}, nil
}

func personalFileContent(output string) (string, error) {
	status, content, found := strings.Cut(output, "\n")
	if !found {
		return "", apperrors.ErrServiceUnavailable
	}
	switch status {
	case "ok":
		data, err := base64.StdEncoding.DecodeString(content)
		if err == nil && len(data) <= personalFileLimit && utf8.Valid(data) {
			return string(data), nil
		}
		return "", apperrors.ErrInvalidArgument
	case "missing":
		return "", apperrors.ErrNotFound
	case "denied":
		return "", apperrors.ErrAccessDenied
	case "too_large":
		return "", apperrors.ErrInvalidArgument
	default:
		return "", apperrors.ErrServiceUnavailable
	}
}
