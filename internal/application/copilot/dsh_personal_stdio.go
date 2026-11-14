package copilot

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	contractenv "github.com/opensoha/soha/contracts/agent/environment"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DSHPersonalStdioPeer interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(int, []byte) error
	SetReadLimit(int64)
	SetWriteDeadline(time.Time) error
	Close() error
}

type DSHPersonalStdioBinding struct {
	Deadline         time.Time
	Open             func(context.Context) (DSHPersonalStdioPeer, error)
	Check            func(context.Context, []byte) error
	ContainerStopped func(context.Context) error
}

func (s *Service) PrepareDSHPersonalStdio(ctx context.Context, grant string, id domain.DSHIdentity, assetID, version string) (DSHPersonalStdioBinding, error) {
	deps := s.personalEnvironment
	if s.nativeRuns == nil || deps == nil || deps.OpenMCPStdio == nil {
		return DSHPersonalStdioBinding{}, apperrors.ErrServiceUnavailable
	}
	ref := domain.RuntimeAssetRef{Kind: "mcp", ID: assetID, Version: version}
	_, cp, principal, _, err := s.authorizeDSHStdio(ctx, grant, id, ref)
	if err != nil {
		return DSHPersonalStdioBinding{}, err
	}
	_, catalog, err := s.runtimeAssetCatalog(ctx)
	if err != nil {
		return DSHPersonalStdioBinding{}, err
	}
	if !slices.ContainsFunc(catalog, func(asset domain.RuntimeAssetDescriptor) bool {
		return asset.Kind == ref.Kind && asset.ID == ref.ID && asset.Version == ref.Version &&
			asset.Transport == "stdio" && asset.ExecutionPosition == "personal-docker"
	}) {
		return DSHPersonalStdioBinding{}, apperrors.ErrAccessDenied
	}
	assistant, err := s.EnsurePersonalAssistant(ctx, principal, "en-US")
	if err != nil {
		return DSHPersonalStdioBinding{}, err
	}
	item, err := deps.Store.GetPersonalEnvironment(ctx, principal.UserID, assistant.ID)
	if err != nil {
		return DSHPersonalStdioBinding{}, err
	}
	if item.AssistantID != assistant.ID || !eligibleDSHStdioEnvironment(item, principal.UserID) {
		return DSHPersonalStdioBinding{}, apperrors.ErrAccessDenied
	}
	request := contractenv.PersonalMCPStdioRequest{Lease: item.Lease, AssetID: assetID, Version: version}
	if err := request.Validate(); err != nil {
		return DSHPersonalStdioBinding{}, apperrors.ErrInvalidArgument
	}
	check := func(checkCtx context.Context, frame []byte) error {
		return s.checkDSHStdio(checkCtx, grant, id, ref, item, frame)
	}
	deadline := cp.Budget.Deadline
	if item.Lease.ExpiresAt.Before(deadline) {
		deadline = item.Lease.ExpiresAt
	}
	return DSHPersonalStdioBinding{
		Deadline: deadline,
		Check:    check,
		ContainerStopped: func(receiptCtx context.Context) error {
			return s.recordStoppedPersonalStdio(receiptCtx, principal, item)
		},
		Open: func(openCtx context.Context) (DSHPersonalStdioPeer, error) {
			if err := check(openCtx, nil); err != nil {
				return nil, err
			}
			return deps.OpenMCPStdio(openCtx, principal, item.AgentClusterID, request)
		},
	}, nil
}

func (s *Service) checkDSHStdio(checkCtx context.Context, grant string, id domain.DSHIdentity, ref domain.RuntimeAssetRef, item domain.PersonalEnvironmentRecord, frame []byte) error {
	if frame != nil {
		lock := s.dshLock(id.RunID)
		lock.Lock()
		defer lock.Unlock()
	}
	lease, current, actor, authCtx, err := s.authorizeDSHStdio(checkCtx, grant, id, ref)
	if err != nil || actor.UserID != item.CreatedBy {
		return apperrors.ErrAccessDenied
	}
	deps := s.personalEnvironment
	owned, err := deps.Store.GetPersonalEnvironment(checkCtx, actor.UserID, item.AssistantID)
	if err != nil || !eligibleDSHStdioEnvironment(owned, actor.UserID) ||
		owned.AssistantID != item.AssistantID || owned.Version != item.Version ||
		owned.ComputeRef != item.ComputeRef || owned.ComputeGeneration != item.ComputeGeneration ||
		owned.Lease.ID != item.Lease.ID || owned.Lease.ScopeHash != item.Lease.ScopeHash ||
		owned.AgentClusterID != item.AgentClusterID {
		return apperrors.ErrAccessDenied
	}
	if err := deps.Authorize(checkCtx, actor, owned.AgentClusterID); err != nil || frame == nil {
		return err
	}
	callID, err := authorizeDSHStdioFrame(frame, ref, current)
	if err != nil || callID == "" {
		return err
	}
	if current.PersonalStdioDispatches == nil {
		current.PersonalStdioDispatches = make(map[string]bool)
	}
	current.PersonalStdioDispatches[callID] = true
	_, err = s.saveDSH(authCtx, lease, current, lease.Phase, nil)
	return err
}

func eligibleDSHStdioEnvironment(item domain.PersonalEnvironmentRecord, owner string) bool {
	return item.CreatedBy == owner && item.AssistantID != "" && item.Lease.OwnerID == item.AssistantID &&
		item.Backend == "container" && item.Provider == "docker" && item.State == "ready" &&
		item.OperationID == "" && item.ActiveTaskID == "" && item.Lease.ExpiresAt.After(time.Now())
}

func (s *Service) authorizeDSHStdio(ctx context.Context, grant string, id domain.DSHIdentity, ref domain.RuntimeAssetRef) (domain.NativeLease, dshCheckpoint, identity.Principal, context.Context, error) {
	lease, cp, principal, authCtx, err := s.authorizeDSHCallback(ctx, grant, id)
	if err == nil && (!cp.Submitted || cp.Start.Profile != "heya" || cp.Start.Assembly == nil ||
		!slices.ContainsFunc(cp.Start.Assembly.Assets, func(asset domain.RuntimeAssetRef) bool {
			return asset.Kind == ref.Kind && asset.ID == ref.ID && asset.Version == ref.Version
		})) {
		err = apperrors.ErrAccessDenied
	}
	return lease, cp, principal, authCtx, err
}

var dshPersonalMCPToolName = regexp.MustCompile(`^[A-Za-z0-9_]{1,128}$`)

type dshStdioRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      jsontext.Value  `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// DSHPersonalStdioPingResponse terminates a server ping on the trusted relay.
func DSHPersonalStdioPingResponse(line []byte) []byte {
	var request dshStdioRequest
	if jsonv2.Unmarshal(line, &request, jsonv2.RejectUnknownMembers(true)) != nil ||
		request.JSONRPC != "2.0" || request.Method != "ping" ||
		(len(request.Params) != 0 && jsontext.Value(request.Params).Kind() != '{') {
		return nil
	}
	id := request.ID
	switch id.Kind() {
	case '"':
	case '0':
		if _, exponent, ok := dshStdioDecimal(string(id)); !ok || exponent < 0 {
			return nil
		}
	default:
		return nil
	}
	reply := append([]byte(`{"jsonrpc":"2.0","id":`), id...)
	return append(reply, ",\"result\":{}}\n"...)
}

func authorizeDSHStdioFrame(frame []byte, ref domain.RuntimeAssetRef, cp dshCheckpoint) (string, error) {
	// The MCP SDK sends one newline-delimited JSON-RPC request per frame.
	if len(frame) == 0 || frame[len(frame)-1] != '\n' || bytes.IndexByte(frame[:len(frame)-1], '\n') >= 0 {
		return "", apperrors.ErrInvalidArgument
	}
	var request dshStdioRequest
	if jsonv2.Unmarshal(frame, &request, jsonv2.RejectUnknownMembers(true)) != nil || request.JSONRPC != "2.0" {
		return "", apperrors.ErrInvalidArgument
	}
	method := request.Method
	switch method {
	case "initialize", "notifications/initialized", "notifications/cancelled", "ping", "tools/list", "resources/list":
		return "", nil
	case "tools/call", "resources/read":
	default:
		return "", apperrors.ErrAccessDenied
	}
	return authorizeDSHStdioCall(method, request.Params, ref, cp)
}

func authorizeDSHStdioCall(method string, raw json.RawMessage, ref domain.RuntimeAssetRef, cp dshCheckpoint) (string, error) {
	if !dshAssemblyConfirmed(cp) || len(cp.Pending) == 0 {
		return "", apperrors.ErrAccessDenied
	}
	var params struct {
		Name      string          `json:"name"`
		URI       string          `json:"uri"`
		Arguments json.RawMessage `json:"arguments"`
		Meta      json.RawMessage `json:"_meta"`
	}
	if jsonv2.Unmarshal(raw, &params, jsonv2.RejectUnknownMembers(true)) != nil {
		return "", apperrors.ErrInvalidArgument
	}
	var args json.RawMessage
	name := "read_resource"
	if method == "tools/call" {
		name = params.Name
		if !dshPersonalMCPToolName.MatchString(name) || name == "read_resource" || name == "list_resources" {
			return "", apperrors.ErrAccessDenied
		}
		args = params.Arguments
	} else {
		args, _ = json.Marshal(map[string]string{"uri": params.URI})
	}
	call := cp.Pending[0].Call
	if cp.PluginDispatches[call.ID] == "" || cp.PersonalStdioDispatches[call.ID] ||
		call.Name != "mcp_"+strings.ReplaceAll(ref.ID, "-", "_")+"_"+name {
		return "", apperrors.ErrAccessDenied
	}
	if !slices.ContainsFunc(dshNativeTools(cp), func(tool domain.DSHNativeTool) bool {
		return tool.Kind == ref.Kind && tool.AssetID == ref.ID && tool.Version == ref.Version && tool.Name == call.Name
	}) {
		return "", apperrors.ErrAccessDenied
	}
	if len(args) == 0 || bytes.Equal(bytes.TrimSpace(args), []byte("null")) {
		args = json.RawMessage(`{}`)
	}
	if !sameDSHStdioArguments(args, call.Arguments) {
		return "", apperrors.ErrAccessDenied
	}
	return call.ID, nil
}

func sameDSHStdioArguments(actual, approved json.RawMessage) bool {
	left, right := jsontext.Value(bytes.Clone(actual)), jsontext.Value(bytes.Clone(approved))
	opts := []jsontext.Options{jsontext.CanonicalizeRawInts(false), jsontext.CanonicalizeRawFloats(false)}
	if left.Kind() != '{' || left.Canonicalize(opts...) != nil || right.Canonicalize(opts...) != nil {
		return false
	}
	if bytes.Equal(left, right) {
		return true
	}
	a, b := jsontext.NewDecoder(bytes.NewReader(left)), jsontext.NewDecoder(bytes.NewReader(right))
	for {
		at, aerr := a.ReadToken()
		bt, berr := b.ReadToken()
		if aerr != nil || berr != nil {
			return aerr == io.EOF && berr == io.EOF
		}
		if at.Kind() != bt.Kind() {
			return false
		}
		if at.String() == bt.String() {
			continue
		}
		if at.Kind() != '0' {
			return false
		}
		ac, ae, aok := dshStdioDecimal(at.String())
		bc, be, bok := dshStdioDecimal(bt.String())
		if !aok || !bok || ac != bc || ae != be {
			return false
		}
	}
}

// Keep the coefficient and base-10 exponent separate to compare without rounding or expanding powers.
func dshStdioDecimal(number string) (coefficient string, exponent int64, ok bool) {
	mantissa, exponentText, _ := strings.Cut(strings.Replace(number, "E", "e", 1), "e")
	mantissa, negative := strings.CutPrefix(mantissa, "-")
	whole, fraction, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(whole+fraction, "0")
	if digits == "" {
		return "", 0, true
	}
	coefficient = strings.TrimRight(digits, "0")
	if exponentText != "" {
		var err error
		exponent, err = strconv.ParseInt(exponentText, 10, 64)
		// Values that can round-trip through JavaScript are well inside this bound.
		limit := int64(len(number)) + 324
		if err != nil || exponent < -limit || exponent > limit {
			return "", 0, false
		}
	}
	exponent += int64(len(digits) - len(coefficient) - len(fraction))
	if negative {
		coefficient = "-" + coefficient
	}
	return coefficient, exponent, true
}
