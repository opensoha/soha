package dshruntime

import (
	"context"
	"errors"
	"net/http"

	appcopilot "github.com/opensoha/soha/internal/application/copilot"
	domain "github.com/opensoha/soha/internal/domain/copilot"
)

// Adapter binds the application-owned runtime port to the service wire client.
type Adapter struct{ client *Client }

var _ appcopilot.DSHRuntime = (*Adapter)(nil)

func NewAdapter(client *Client) (*Adapter, error) {
	if client == nil {
		return nil, errors.New("dsh runtime client is required")
	}
	return &Adapter{client: client}, nil
}
func wireIdentity(id domain.DSHIdentity) Identity {
	return Identity{RunID: id.RunID, Attempt: id.Attempt, Fence: id.Fence}
}
func domainIdentity(id Identity) domain.DSHIdentity {
	return domain.DSHIdentity{RunID: id.RunID, Attempt: id.Attempt, Fence: id.Fence}
}
func wireSubmission(s domain.DSHSubmission) Submission {
	return Submission{Identity: wireIdentity(s.DSHIdentity), InputHash: s.InputHash, SubmissionID: s.SubmissionID}
}
func domainSubmission(s Submission) domain.DSHSubmission {
	return domain.DSHSubmission{DSHIdentity: domainIdentity(s.Identity), InputHash: s.InputHash, SubmissionID: s.SubmissionID}
}
func (a *Adapter) Start(ctx context.Context, in domain.DSHStart) (domain.DSHReceipt, error) {
	request := StartRequest{Identity: wireIdentity(in.DSHIdentity), OwnerKey: in.OwnerKey, SessionID: in.SessionID, RequestID: in.RequestID, InputHash: in.InputHash, ContextRevision: in.ContextRevision, Profile: in.Profile, Model: in.Model, Prompt: in.Prompt, ToolCatalogRevision: in.ToolCatalogRevision, ToolGrant: in.ToolGrant, Deadline: in.Deadline, MaxOutputTokens: in.MaxOutputTokens}
	request.Assembly = in.Assembly
	for _, m := range in.Context {
		request.Context = append(request.Context, ContextMessage{Role: m.Role, Content: m.Content})
	}
	for _, t := range in.Tools {
		request.Tools = append(request.Tools, Tool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema, ReadOnly: t.ReadOnly})
	}
	r, err := a.client.Start(ctx, request)
	if err != nil {
		var httpErr *HTTPError
		if errors.Is(err, ErrInput) || (errors.As(err, &httpErr) && rejectedStartStatus(httpErr.StatusCode)) {
			return domain.DSHReceipt{}, errors.Join(appcopilot.ErrDSHStartRejected, err)
		}
		return domain.DSHReceipt{}, err
	}
	return domain.DSHReceipt{DSHSubmission: domainSubmission(r.Submission), Status: r.Status, Sequence: r.Sequence}, nil
}

// These statuses are emitted before the runtime creates a durable receipt.
// A conflicting request can still have an existing receipt; reconcile it.
func rejectedStartStatus(code int) bool {
	switch code {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestEntityTooLarge, http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}
func domainStatus(s Status, err error) (domain.DSHStatus, error) {
	if err != nil {
		return domain.DSHStatus{}, err
	}
	result := domain.DSHStatus{DSHSubmission: domainSubmission(s.Submission), Status: s.Status, StopReason: s.StopReason, AssistantText: s.AssistantText, Sequence: s.Sequence, UsageKnown: s.UsageKnown}
	if s.Usage != nil {
		result.Usage = &domain.DSHUsage{InputTokens: s.Usage.InputTokens, OutputTokens: s.Usage.OutputTokens, TotalTokens: s.Usage.TotalTokens}
	}
	return result, nil
}
func (a *Adapter) Lookup(ctx context.Context, id domain.DSHIdentity, hash string) (domain.DSHStatus, error) {
	return domainStatus(a.client.Lookup(ctx, wireIdentity(id), hash))
}
func (a *Adapter) Status(ctx context.Context, s domain.DSHSubmission) (domain.DSHStatus, error) {
	return domainStatus(a.client.Status(ctx, wireSubmission(s)))
}
func (a *Adapter) Cancel(ctx context.Context, s domain.DSHSubmission, reason string) (domain.DSHStatus, error) {
	return domainStatus(a.client.Cancel(ctx, wireSubmission(s), reason))
}
func (a *Adapter) Events(ctx context.Context, id domain.DSHIdentity, after int64) (domain.DSHEventPage, error) {
	p, err := a.client.Events(ctx, wireIdentity(id), after)
	if err != nil {
		return domain.DSHEventPage{}, err
	}
	out := domain.DSHEventPage{NextSequence: p.NextSequence, HasMore: p.HasMore, Items: []domain.DSHEvent{}}
	for _, e := range p.Items {
		out.Items = append(out.Items, domain.DSHEvent{DSHIdentity: domainIdentity(e.Identity), Sequence: e.Sequence, Type: e.Type, Delta: e.Data.Delta, AssistantText: e.Data.AssistantText, Status: e.Data.Status, StopReason: e.Data.StopReason, ErrorCode: e.Data.ErrorCode, CallID: e.Data.CallID, ToolName: e.Data.ToolName, Outcome: e.Data.Outcome})
	}
	return out, nil
}
