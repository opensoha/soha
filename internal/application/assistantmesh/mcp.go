package assistantmesh

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	appdlp "github.com/opensoha/soha/internal/application/contentdlp"
	gateway "github.com/opensoha/soha/internal/domain/aigateway"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	audit "github.com/opensoha/soha/internal/domain/audit"
	identity "github.com/opensoha/soha/internal/domain/identity"
	knowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type CoreMCPGateway interface {
	InvokeTool(context.Context, identity.Principal, gateway.ToolInvocationRequest) (gateway.ToolInvocationResult, error)
}

// MCPService shares a fixed Core capability. Endpoint and credential mode are
// owned by AI Gateway and cannot be supplied by an employee or model.
type MCPService struct {
	store       Store
	settings    Settings
	permissions *appaccess.PermissionResolver
	gateway     CoreMCPGateway
	audit       Audit
	users       Users
}

func NewMCPService(store Store, settings Settings, permissions *appaccess.PermissionResolver, gateway CoreMCPGateway, audit Audit, users Users) *MCPService {
	return &MCPService{store: store, settings: settings, permissions: permissions, gateway: gateway, audit: audit, users: users}
}

type mcpProposal struct {
	Input    domain.AssetInput     `json:"input"`
	Audience knowledge.AccessScope `json:"audience"`
	Status   string                `json:"status"`
	Reason   string                `json:"reason,omitempty"`
	Approved time.Time             `json:"approved,omitempty"`
	Revision int                   `json:"revision,omitempty"`
}

func (s *MCPService) authorize(ctx context.Context, principal identity.Principal) error {
	if principal.UserID == "" {
		return apperrors.ErrUnauthorized
	}
	user, err := s.users.GetByID(ctx, principal.UserID)
	if err != nil {
		return err
	}
	if user.ID != principal.UserID || user.Status != "active" {
		return apperrors.ErrUnauthorized
	}
	config, err := s.settings.ResolveAssistantMeshSettings(ctx)
	if err != nil {
		return err
	}
	if !config.Enabled {
		return apperrors.ErrAccessDenied
	}
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermObserveAIChatUse)
}

func (s *MCPService) isAdmin(ctx context.Context, p identity.Principal) bool {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, p, appaccess.ManagedActionPermission(appaccess.PermSettingsAIManage, "update")) == nil
}

func decodeMCP(rec domain.Record) (mcpProposal, error) {
	if rec.Kind != "mcp" {
		return mcpProposal{}, apperrors.ErrNotFound
	}
	var item mcpProposal
	if err := json.Unmarshal(rec.Payload, &item); err != nil {
		return mcpProposal{}, err
	}
	return item, nil
}

func inspectMCPProposal(ctx context.Context, in domain.AssetInput) []string {
	decision := appdlp.Inspect(ctx, meshDLPPolicy, []byte(in.Title+"\n"+in.Body))
	if !decision.Allowed && len(decision.MatchedRuleIDs) == 0 {
		return []string{"inspection_unavailable"}
	}
	return decision.MatchedRuleIDs
}

func (s *MCPService) visible(ctx context.Context, rec domain.Record, item mcpProposal, p identity.Principal) bool {
	if item.Status != "approved" || !domain.AudienceAllows(rec.OwnerID, item.Audience, p) {
		return false
	}
	author, err := s.users.GetByID(ctx, rec.OwnerID)
	return err == nil && author.ID == rec.OwnerID && author.Status == "active"
}

func mcpView(rec domain.Record, p mcpProposal, private bool) map[string]any {
	out := map[string]any{"id": rec.ID, "ownerUserId": rec.OwnerID, "kind": "mcp"}
	version := map[string]any{"input": p.Input, "digest": p.candidateDigest(), "version": p.Revision, "audience": p.Audience}
	if p.Status == "approved" {
		out["published"] = map[string]any{"input": p.Input, "digest": p.approvalDigest(), "version": p.Revision, "audience": p.Audience, "publishedAt": p.Approved}
	}
	if private {
		out["draft"], out["draftDigest"] = p.Input, p.Input.Digest()
		if p.Status != "draft" {
			out["review"] = map[string]any{"status": p.Status, "reason": p.Reason, "candidate": version}
		}
	}
	return out
}

func (p mcpProposal) approvalDigest() string {
	return publicationDigest([]any{p.candidateDigest(), p.Approved})
}

func (p mcpProposal) candidateDigest() string {
	return publicationDigest([]any{p.Input.Digest(), p.Audience, p.Revision})
}

func (s *MCPService) Create(ctx context.Context, p identity.Principal, input domain.AssetInput) (map[string]any, error) {
	if err := s.authorize(ctx, p); err != nil {
		return nil, err
	}
	if input.Kind != "mcp" || len(input.Files)+len(input.Dependencies)+len(input.Parameters) != 0 || input.Applicability != "" ||
		strings.TrimSpace(input.Title) == "" || utf8.RuneCountInString(input.Title) > 256 || strings.TrimSpace(input.Body) == "" || len(input.Body) > 16<<10 {
		return nil, apperrors.ErrInvalidArgument
	}
	if hits := inspectMCPProposal(ctx, input); len(hits) != 0 {
		return nil, apperrors.ErrInvalidArgument
	}
	proposal := mcpProposal{Input: input, Status: "draft", Audience: knowledge.AccessScope{Visibility: "private"}}
	raw, err := json.Marshal(proposal)
	if err != nil {
		return nil, err
	}
	rec := domain.Record{ID: uuid.NewString(), OwnerID: p.UserID, Kind: "mcp", Payload: raw}
	if err := s.store.Create(ctx, rec); err != nil {
		return nil, err
	}
	return mcpView(rec, proposal, true), nil
}

func (s *MCPService) List(ctx context.Context, p identity.Principal, view string, offset int) (map[string]any, error) {
	if err := s.authorize(ctx, p); err != nil {
		return nil, err
	}
	owner := ""
	switch view {
	case "mine":
		owner = p.UserID
	case "review":
		if !s.isAdmin(ctx, p) {
			return nil, apperrors.ErrAccessDenied
		}
	case "shared":
	default:
		return nil, apperrors.ErrInvalidArgument
	}
	const pageLimit = 50
	rows, err := s.store.List(ctx, "mcp", owner, pageLimit, offset)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item, err := decodeMCP(row)
		if err != nil {
			return nil, err
		}
		if view == "mine" || view == "review" && item.Status == "pending" || view == "shared" && s.visible(ctx, row, item, p) {
			items = append(items, mcpView(row, item, view != "shared"))
		}
	}
	result := map[string]any{"items": items}
	if len(rows) == pageLimit {
		result["nextOffset"] = offset + pageLimit
	}
	return result, nil
}

func (s *MCPService) Get(ctx context.Context, p identity.Principal, id string) (map[string]any, error) {
	if err := s.authorize(ctx, p); err != nil {
		return nil, err
	}
	rec, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	item, err := decodeMCP(rec)
	if err != nil {
		return nil, err
	}
	private := rec.OwnerID == p.UserID || item.Status == "pending" && s.isAdmin(ctx, p)
	if !private && !s.visible(ctx, rec, item, p) {
		return nil, apperrors.ErrNotFound
	}
	return mcpView(rec, item, private), nil
}

func (s *MCPService) Act(ctx context.Context, p identity.Principal, id, action string, req ActionRequest) (map[string]any, error) {
	if err := s.authorize(ctx, p); err != nil {
		return nil, err
	}
	if action == "invoke" {
		return s.invoke(ctx, p, id, req)
	}
	var result map[string]any
	admin := s.isAdmin(ctx, p)
	err := s.store.Update(ctx, id, func(tx context.Context, rec domain.Record) (json.RawMessage, error) {
		item, err := decodeMCP(rec)
		if err != nil {
			return nil, err
		}
		owner := rec.OwnerID == p.UserID
		if !owner && !admin {
			return nil, apperrors.ErrNotFound
		}
		digest := item.Input.Digest()
		switch action {
		case "approve", "reject", "withdraw":
			digest = item.candidateDigest()
		case "revoke":
			digest = item.approvalDigest()
		}
		if req.ExpectedDigest != digest {
			return nil, apperrors.ErrConflict
		}
		if action == "approve" {
			publisher, err := s.users.GetByID(tx, rec.OwnerID)
			if err != nil || publisher.ID != rec.OwnerID || publisher.Status != "active" {
				return nil, apperrors.ErrAccessDenied
			}
		}
		if err := s.transition(tx, p, &item, owner, admin, action, req); err != nil {
			return nil, err
		}
		if err := s.audit.Record(tx, audit.Entry{ActorID: p.UserID, ResourceKind: "AssistantMeshMCP", ResourceName: id,
			Action: "assistant_mesh.mcp." + action, Result: "success",
			Metadata: map[string]any{"digest": req.ExpectedDigest, "state": item.Status}}); err != nil {
			return nil, err
		}
		result = map[string]any{"asset": mcpView(rec, item, true)}
		return json.Marshal(item)
	})
	return result, err
}

func (s *MCPService) transition(ctx context.Context, p identity.Principal, item *mcpProposal, owner, admin bool, action string, req ActionRequest) error {
	switch action {
	case "publish":
		if !owner || !slices.Contains([]string{"draft", "rejected", "withdrawn"}, item.Status) {
			return apperrors.ErrAccessDenied
		}
		if err := domain.ValidateAudience(p, req.Audience); err != nil {
			return apperrors.ErrInvalidArgument
		}
		if len(inspectMCPProposal(ctx, item.Input)) != 0 {
			return apperrors.ErrInvalidArgument
		}
		item.Audience, item.Status, item.Reason = req.Audience, "pending", ""
		item.Revision++
	case "approve", "reject":
		if !admin || owner || item.Status != "pending" {
			return apperrors.ErrAccessDenied
		}
		if action == "approve" {
			item.Status, item.Approved = "approved", time.Now().UTC()
		} else {
			if strings.TrimSpace(req.Reason) == "" || len(req.Reason) > 512 {
				return apperrors.ErrInvalidArgument
			}
			item.Status, item.Reason = "rejected", strings.TrimSpace(req.Reason)
		}
	case "withdraw":
		if !owner || item.Status != "pending" {
			return apperrors.ErrAccessDenied
		}
		item.Status = "withdrawn"
	case "revoke":
		if item.Status != "approved" {
			return apperrors.ErrAccessDenied
		}
		item.Status, item.Approved = "withdrawn", time.Time{}
	default:
		return apperrors.ErrInvalidArgument
	}
	return nil
}

func (s *MCPService) AuthorizeMCP(ctx context.Context, p identity.Principal, id, digest string) error {
	if err := s.authorize(ctx, p); err != nil {
		return err
	}
	_, _, err := s.approved(ctx, p, id, digest)
	return err
}

func (s *MCPService) approved(ctx context.Context, p identity.Principal, id, digest string) (domain.Record, mcpProposal, error) {
	rec, err := s.store.Get(ctx, id)
	if err != nil {
		return rec, mcpProposal{}, err
	}
	item, err := decodeMCP(rec)
	if err != nil {
		return rec, item, err
	}
	if digest != item.approvalDigest() || !s.visible(ctx, rec, item, p) {
		return rec, item, apperrors.ErrNotFound
	}
	return rec, item, nil
}

func (s *MCPService) invoke(ctx context.Context, p identity.Principal, id string, req ActionRequest) (map[string]any, error) {
	rec, item, err := s.approved(ctx, p, id, req.ExpectedDigest)
	if err != nil {
		return nil, err
	}
	projectID, err := uuid.Parse(req.ResourceID)
	if err != nil || projectID.String() != req.ResourceID {
		return nil, apperrors.ErrInvalidArgument
	}
	if s.gateway == nil {
		return nil, apperrors.ErrServiceUnavailable
	}
	result, err := s.gateway.InvokeTool(appaigateway.WithAssistantMeshProjectGrant(ctx), p, gateway.ToolInvocationRequest{
		ToolName: "docker.projects.get", CapabilityVersion: "1",
		Input: map[string]any{"projectId": req.ResourceID}, RequestID: req.RequestID,
	})
	if err != nil {
		return nil, err
	}
	return mcpInvocationResult(rec, item, req.ResourceID, result)
}

func mcpInvocationResult(rec domain.Record, p mcpProposal, requested string, result gateway.ToolInvocationResult) (map[string]any, error) {
	if result.RequiresApproval || result.Output == nil {
		return nil, apperrors.ErrAccessDenied
	}
	data, err := json.Marshal(result.Output)
	if err != nil {
		return nil, err
	}
	var project struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if json.Unmarshal(data, &project) != nil || project.ID != requested {
		return nil, apperrors.ErrConflict
	}
	return map[string]any{"asset": mcpView(rec, p, false), "project": project}, nil
}
