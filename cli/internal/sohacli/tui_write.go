package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	contractresource "github.com/opensoha/soha-contracts/resource"
)

// Plans retain a private, typed submission closure. Rendering never reparses a
// response or uses server text as an executable action.
type tuiWritePlan struct {
	scope         tuiScope
	target, key   string
	review        any
	submit        func(context.Context) (any, error)
	preconditions *contractresource.ResourceMutationPreconditions
}
type tuiWriteBackend interface {
	prepareWrite(context.Context, tuiScope, *tuiRow, string) (*tuiWritePlan, error)
}
type tuiWriteResult struct {
	epoch uint64
	plan  *tuiWritePlan
	err   error
}
type tuiWriteInput struct {
	Action   string          `json:"action"`
	Replicas *int32          `json:"replicas,omitempty"`
	Revision string          `json:"revision,omitempty"`
	Intent   string          `json:"intent,omitempty"`
	PlanID   string          `json:"planId,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
}

func decodeTUIWrite(raw string, out any) error {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("invalid typed action JSON; values are not echoed")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("provide exactly one action JSON object")
	}
	return nil
}

func tuiWriteTemplate(scope tuiScope, row *tuiRow) string {
	section := scope.section
	if section == "" {
		section = tuiSections(scope.tab)[0]
	}
	switch {
	case scope.tab == 0 && row != nil && (section == "deployments" || section == "statefulsets" || section == "daemonsets"):
		return `{"action":"restart"}`
	case scope.tab == 1 && section == "vms":
		return `{"action":"create","input":{"connectionId":"","name":"","cpu":1,"memoryMiB":512,"diskGiB":4,"requireCapacity":true,"startAfterCreate":false}}`
	case scope.tab == 1 && section == "hosts":
		return `{"action":"create","input":{"name":""}}`
	case scope.tab == 1 && section == "projects" && row != nil:
		return `{"action":"deploy","intent":"deploy"}`
	case scope.tab == 2:
		return `{"action":"confirm-plan","planId":""}`
	default:
		return ""
	}
}

func (m *tuiModel) beginWriteEdit() {
	if m.busy {
		return
	}
	if _, ok := m.backend.(tuiWriteBackend); !ok {
		m.message = "Write plans are unavailable for this backend."
		return
	}
	draft := tuiWriteTemplate(m.scope, m.row())
	if draft == "" {
		m.message = "Select a supported workload/project, or use a typed native command for this view."
		return
	}
	m.draft, m.composing, m.writeEditing = draft, true, true
	m.contextEditing = false
}

func (m *tuiModel) stageWrite() tea.Cmd {
	backend, ok := m.backend.(tuiWriteBackend)
	if !ok {
		return nil
	}
	scope, draft, row := m.scope, m.draft, m.row()
	ctx, epoch := m.begin()
	m.composing, m.writeEditing = false, false
	m.message = "Reading a real server plan; no execution request sent."
	return func() tea.Msg {
		plan, err := backend.prepareWrite(ctx, scope, row, draft)
		return tuiWriteResult{epoch: epoch, plan: plan, err: err}
	}
}

func (m *tuiModel) applyWriteResult(result tuiWriteResult) {
	if result.epoch != m.epoch {
		return
	}
	m.busy = false
	if result.err != nil {
		m.failed, m.message = true, tuiText(result.err.Error())
		return
	}
	plan := result.plan
	if plan == nil || plan.submit == nil || plan.target == "" {
		m.failed, m.message = true, "Invalid server plan; nothing submitted."
		return
	}
	review, err := json.MarshalIndent(map[string]any{"profile": m.profile, "server": m.origin, "subject": m.subject, "cluster": plan.scope.cluster, "namespace": plan.scope.namespace, "target": plan.target, "originalRequest": plan.key, "review": plan.review}, "", "  ")
	if err != nil {
		m.failed, m.message = true, "Cannot display the complete plan; nothing submitted."
		return
	}
	if len(review) > 128<<10 {
		m.failed, m.message = true, "Plan too large for a complete TUI review; use the typed native command."
		return
	}
	m.failed, m.message = false, "Review all lines before submitting. Server authorization and approval still apply."
	m.confirm = &tuiConfirmation{scope: plan.scope, row: tuiRow{id: plan.target}, action: "write", content: string(review), requestID: plan.key, plan: plan}
}

func (a tuiAPI) prepareWrite(ctx context.Context, scope tuiScope, row *tuiRow, raw string) (*tuiWritePlan, error) {
	var input tuiWriteInput
	if err := decodeTUIWrite(raw, &input); err != nil {
		return nil, err
	}
	if tuiWriteTemplate(scope, row) == "" {
		return nil, fmt.Errorf("write action unavailable for this view")
	}
	if scope.section == "" {
		scope.section = tuiSections(scope.tab)[0]
	}
	// Mutations never follow redirects to another endpoint or old compatibility route.
	a.client.Client = a.client.httpClient()
	a.client.Client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	switch scope.tab {
	case 0:
		return a.prepareWorkloadWrite(ctx, scope, *row, input)
	case 1:
		return a.prepareComputeWrite(ctx, scope, row, input)
	case 2:
		return a.prepareDeliveryWrite(ctx, scope, input)
	default:
		return nil, fmt.Errorf("unsupported write workbench")
	}
}

func (a tuiAPI) prepareWorkloadWrite(ctx context.Context, scope tuiScope, row tuiRow, input tuiWriteInput) (*tuiWritePlan, error) {
	if len(input.Input) != 0 || input.Intent != "" || input.PlanID != "" {
		return nil, fmt.Errorf("workload action only accepts action, replicas and revision")
	}
	if row.namespace != "" {
		scope.namespace = row.namespace
	}
	value, err := a.client.ReadKubernetesResource(ctx, scope.cluster, scope.namespace, scope.section, row.name, "identity")
	if err != nil {
		return nil, err
	}
	identity, ok := value.(*sohaapi.KubernetesResourceIdentityEnvelope)
	if !ok {
		return nil, fmt.Errorf("invalid workload identity response")
	}
	endpoint, err := kubernetesEndpoint(scope.section)
	if err != nil {
		return nil, err
	}
	if identity.Data.Name != row.name || identity.Data.Kind != endpoint.kind || identity.Data.Namespace != scope.namespace || identity.Data.DeletionTimestamp != nil {
		return nil, fmt.Errorf("workload identity changed or is deleting; reread before planning")
	}
	request := contractresource.ConditionalWorkloadActionInput{Preconditions: contractresource.ResourceMutationPreconditions{ExpectedUID: identity.Data.UID, ExpectedResourceVersion: identity.Data.ResourceVersion}, Replicas: input.Replicas, Revision: input.Revision, DryRun: true}
	preview, err := a.client.ExecuteKubernetesWorkloadAction(ctx, scope.cluster, scope.namespace, endpoint.kind, row.name, input.Action, request)
	if err != nil {
		return nil, err
	}
	request.DryRun = false
	preconditions := request.Preconditions
	return &tuiWritePlan{scope: scope, target: scope.cluster + "/" + scope.namespace + "/" + scope.section + "/" + row.name, preconditions: &preconditions, review: preview, submit: func(ctx context.Context) (any, error) {
		return a.client.ExecuteKubernetesWorkloadAction(ctx, scope.cluster, scope.namespace, endpoint.kind, row.name, input.Action, request)
	}}, nil
}

func (a tuiAPI) prepareComputeWrite(ctx context.Context, scope tuiScope, row *tuiRow, input tuiWriteInput) (*tuiWritePlan, error) {
	if input.Replicas != nil || input.Revision != "" || input.PlanID != "" {
		return nil, fmt.Errorf("compute action does not accept workload or delivery fields")
	}
	request, err := tuiComputeWriteRequest(scope, row, input)
	if err != nil {
		return nil, err
	}
	key := "tui-" + uuid.NewString()
	if err := validateDockerWorkbenchInput(request.body, key); err != nil {
		return nil, err
	}
	var preview sohaapi.OperationalPlanEnvelope
	if err := a.client.doJSON(ctx, http.MethodPost, request.path+"/plan", a.client.Token, nil, request.body, &preview); err != nil {
		return nil, err
	}
	if !preview.Data.Ready {
		return nil, fmt.Errorf("server plan is not ready; no write submitted")
	}
	if err := bindDockerProjectDeployPlan(request.body, request.target, preview.Data); err != nil {
		return nil, err
	}
	review := map[string]any{"input": sanitizeCLIJSONValue(request.body), "plan": sanitizeCLIJSONValue(preview)}
	return &tuiWritePlan{scope: scope, target: request.target, key: key, review: review, submit: func(ctx context.Context) (any, error) {
		err := a.client.doJSON(ctx, http.MethodPost, request.path, a.client.Token, idempotencyHeaders(key), request.body, request.output)
		return request.output, err
	}}, nil
}

type tuiComputeWrite struct {
	path, target string
	body, output any
}

func tuiComputeWriteRequest(scope tuiScope, row *tuiRow, input tuiWriteInput) (tuiComputeWrite, error) {
	var empty tuiComputeWrite
	switch scope.section {
	case "vms":
		if input.Action != "create" || input.Intent != "" {
			return empty, fmt.Errorf("VM view supports create with typed input")
		}
		request := &sohaapi.VirtualMachineCreateInput{}
		if err := decodeTUIWrite(string(input.Input), request); err != nil {
			return empty, err
		}
		if request.ConnectionID == "" || request.Name == "" {
			return empty, fmt.Errorf("connectionId and name are required")
		}
		return tuiComputeWrite{path: virtualizationPath + "/vms", body: request, output: &sohaapi.VirtualizationOperationEnvelope{}, target: "connection=" + request.ConnectionID + " name=" + request.Name}, nil
	case "hosts", "projects":
		return tuiDockerWriteRequest(scope, row, input)
	default:
		return empty, fmt.Errorf("compute write unavailable")
	}
}

func tuiDockerWriteRequest(scope tuiScope, row *tuiRow, input tuiWriteInput) (tuiComputeWrite, error) {
	var empty tuiComputeWrite
	ids := []string(nil)
	if scope.section == "projects" && row != nil {
		ids = []string{row.id}
	}
	req, _, err := prepareDockerWorkbench(scope.section, input.Action, ids, input.Intent)
	if err != nil || req.preflightPath == "" {
		return empty, fmt.Errorf("this compute action requires an existing server plan")
	}
	target := req.id
	if scope.section == "hosts" {
		if input.Intent != "" {
			return empty, fmt.Errorf("host creation does not accept intent")
		}
		if err := decodeTUIWrite(string(input.Input), req.body); err != nil {
			return empty, err
		}
		host, ok := req.body.(*sohaapi.DockerQuickCreateHostInput)
		if !ok || host == nil {
			return empty, fmt.Errorf("invalid host creation input")
		}
		target = "new Docker host " + host.Name
	} else if len(input.Input) != 0 {
		return empty, fmt.Errorf("project action uses the selected registered configuration")
	}
	return tuiComputeWrite{path: req.path, body: req.body, output: req.out, target: target}, nil
}

func (a tuiAPI) prepareDeliveryWrite(ctx context.Context, scope tuiScope, input tuiWriteInput) (*tuiWritePlan, error) {
	if input.Action != "confirm-plan" || !kubernetesSegment(input.PlanID) || input.Replicas != nil || input.Revision != "" || input.Intent != "" || len(input.Input) != 0 {
		return nil, fmt.Errorf("delivery expects action=confirm-plan and one planId")
	}
	path := "/api/v1/delivery/plans/" + url.PathEscape(input.PlanID)
	var preview sohaapi.DeliveryPlanEnvelope
	if err := a.client.doJSON(ctx, http.MethodGet, path, a.client.Token, nil, nil, &preview); err != nil {
		return nil, err
	}
	if preview.Data.ID != input.PlanID {
		return nil, fmt.Errorf("delivery plan identity mismatch")
	}
	return &tuiWritePlan{scope: scope, target: input.PlanID, review: sanitizeCLIJSONValue(preview), submit: func(ctx context.Context) (any, error) {
		var out sohaapi.DeliveryPlanConfirmResultEnvelope
		err := a.client.doJSON(ctx, http.MethodPost, path+"/confirm", a.client.Token, nil, nil, &out)
		return &out, err
	}}, nil
}

func (m *tuiModel) submitWrite(plan *tuiWritePlan) tea.Cmd {
	if !m.rememberReference(tuiRequestReference{Kind: "write", Cluster: plan.scope.cluster, Namespace: plan.scope.namespace, Target: plan.target, RequestID: plan.key, Preconditions: plan.preconditions}) {
		return nil
	}
	ctx, epoch := m.begin()
	m.detail = true
	m.writeReference = "Original write: " + plan.target + " · request=" + plan.key
	m.message = "Submitting once; disconnect is unknown. Inspect the original receipt before retrying."
	m.snapshot = tuiSnapshot{detail: map[string]any{"target": plan.target, "originalRequest": plan.key, "status": "submitting", "review": plan.review}, loaded: time.Now()}
	return func() tea.Msg {
		value, err := plan.submit(ctx)
		if err != nil {
			return tuiResult{epoch: epoch, err: fmt.Errorf("%w; write outcome may be unknown; no automatic replay", err)}
		}
		// Match native workbench projection, including hidden runner payloads.
		value = dockerWorkbenchProjection(value)
		return tuiResult{epoch: epoch, snapshot: tuiSnapshot{detail: sanitizeCLIJSONValue(value), loaded: time.Now(), message: "Server receipt returned. Read the referenced task/resource to verify completion."}}
	}
}
