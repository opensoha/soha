package templateupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) CreatePreview(ctx context.Context, p domainidentity.Principal, input sohaapi.TemplateUpdatePreviewInput) (sohaapi.TemplateUpdatePreview, error) {
	return s.createPreview(ctx, p, input, nil)
}

func (s *Service) createPreview(ctx context.Context, p domainidentity.Principal, input sohaapi.TemplateUpdatePreviewInput, policy *domainupdate.Policy) (sohaapi.TemplateUpdatePreview, error) {
	result := sohaapi.TemplateUpdatePreview{ID: uuid.NewString(), Kind: input.Kind, TemplateID: input.TemplateID, ToVersion: input.ToVersion, Candidates: []sohaapi.TemplateUpdateCandidate{}, ConfigurationOnly: true, CreatedAt: time.Now().UTC()}
	result.ExpiresAt = result.CreatedAt.Add(24 * time.Hour)
	if len(input.Targets) < 1 || len(input.Targets) > 100 || input.ToVersion < 1 {
		return result, invalid("select 1–100 application bindings and a published version")
	}
	f := domainupdate.Filter{Kind: input.Kind, TemplateID: input.TemplateID}
	f.ApplicationID = input.ApplicationID
	if err := validFilter(f); err != nil {
		return result, err
	}
	versions, err := s.versions(ctx, p, f.Kind, f.TemplateID)
	if err != nil {
		return result, err
	}
	if len(versions)*len(input.Targets) > 1000 {
		return result, invalid("preview exceeds 1000 binding-version comparisons; select a smaller batch")
	}
	to, ok := findVersion(versions, input.ToVersion)
	if !ok {
		return result, apperrors.ErrNotFound
	}
	bindings, err := s.bindings(ctx, p, f)
	if err != nil {
		return result, err
	}
	frozen := []domainupdate.FrozenCandidate{}
	seen := map[string]bool{}
	for _, selection := range input.Targets {
		if seen[selection.TargetID] || selection.Wave < 0 || selection.Wave > 199 {
			return result, invalid("duplicate target or invalid wave")
		}
		seen[selection.TargetID] = true
		index := slices.IndexFunc(bindings, func(b domainupdate.Binding) bool { return b.Target.ID == selection.TargetID })
		if index < 0 {
			return result, apperrors.ErrNotFound
		}
		binding := bindings[index]
		sensitive, err := s.authorizePreviewHistory(ctx, p, binding, selection, versions, to.Number)
		if err != nil {
			return result, err
		}
		candidate := s.previewCandidate(ctx, p, binding, selection, versions, to)
		result.Candidates = append(result.Candidates, candidate)
		// Persist only the selectors, hashes and selected template inputs. Existing
		// application/Helm/Manifest records remain their configuration owners.
		item := domainupdate.FrozenCandidate{PreviewID: result.ID, Candidate: candidate, TemplateDigest: to.Digest, Selection: selection, ExpiresAt: result.ExpiresAt, Sensitive: sensitive}
		if policy != nil {
			item.PolicyID = policy.Value.ID
			item.PolicyVersion = policy.Value.Version
		}
		frozen = append(frozen, item)
	}
	result.Digest = digest(result)
	data, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	if len(data) > 8<<20 {
		return result, invalid("preview exceeds 8 MiB; select fewer targets or versions")
	}
	if err := s.deps.Repository.SavePreview(ctx, result, frozen); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Service) previewCandidate(ctx context.Context, p domainidentity.Principal, b domainupdate.Binding, selection sohaapi.TemplateUpdateSelection, versions []version, to version) sohaapi.TemplateUpdateCandidate {
	parameters := selectedParameters(b, selection)
	c := sohaapi.TemplateUpdateCandidate{ID: uuid.NewString(), Target: b.Target, ToVersion: to.Number, Parameters: parameters, Eligible: b.Target.Eligible, Reason: b.Target.Reason, RequiresApproval: b.Target.RequiresApproval || b.Target.Mode == "approval", Wave: selection.Wave, Versions: []sohaapi.TemplateUpdateDiff{}}
	from, ok := findVersion(versions, b.Target.FromVersion)
	if !ok {
		from = version{Number: b.Target.FromVersion}
		c.Eligible = false
		c.Reason = "当前固定版本快照缺失，无法安全核对独立修改"
	}
	c.Cumulative = s.compare(ctx, p, b, from, to, parameters)
	var gap bool
	c.Versions, gap = versionDiffs(versions, from.Number, to.Number, func(left, right version) sohaapi.TemplateUpdateDiff {
		historical := b
		historical.Parameters = parameters
		return s.compare(ctx, p, historical, left, right, parameters)
	})
	c.Cumulative.HistoryGap = gap
	if !to.Enabled {
		c.Eligible = false
		c.Reason = "目标模板已禁用或撤回"
	}
	if from.Number == to.Number {
		c.Eligible = false
		c.Reason = "已采用该版本"
	}
	if _, err := resolveParameters(to, b, parameters); err != nil {
		c.Eligible = false
		c.Reason = safeReason(err)
	}
	if to.Deployment != nil {
		if err := s.validateDeploymentCandidate(ctx, p, b, from, to, parameters); err != nil {
			c.Eligible = false
			c.Reason = err.Error()
		}
	}
	if err := s.authorizeBinding(ctx, p, b, false); err != nil {
		c.Eligible = false
		c.Reason = "缺少目标应用或环境的配置更新权限"
	}
	c.Digest = digest(struct {
		Target         sohaapi.TemplateUpdateTarget
		Version        int64
		TemplateDigest string
		Parameters     any
		Selection      sohaapi.TemplateUpdateSelection
	}{c.Target, c.ToVersion, to.Digest, c.Parameters, selection})
	return c
}

func (s *Service) validateDeploymentCandidate(ctx context.Context, p domainidentity.Principal, b domainupdate.Binding, from, to version, parameters map[string]any) error {
	if b.Service.DeploymentTemplate == nil || b.Service.DeploymentTemplate.Detached {
		return invalid("独立副本不参与模板传播")
	}
	if from.Deployment == nil || to.Deployment == nil || from.Deployment.Source.Renderer != to.Deployment.Source.Renderer {
		return invalid("渲染器发生变化，请创建新的服务配置并手动迁移")
	}
	for _, container := range to.Deployment.Artifacts {
		if !slices.ContainsFunc(b.Service.Containers, func(item domainapp.ServiceContainer) bool { return item.Name == container }) {
			return invalid("目标版本引用了当前服务未声明的产物容器")
		}
	}
	if b.ReleaseTarget.Helm != nil && b.ReleaseTarget.Metadata["templateSelection"] == nil {
		original, err := s.renderPreview(ctx, p, b, from, b.Parameters)
		if err != nil {
			return invalid("当前 Helm 模板无法渲染，先核对来源")
		}
		var source sohaapi.DeploymentTemplateHelmSource
		if original.Source.Helm == nil {
			return invalid("当前版本不是原生 Helm 来源")
		}
		if err := copyJSON(original.Source.Helm, &source); err != nil {
			return err
		}
		if digest(source) != digest(b.ReleaseTarget.Helm.Source) {
			return invalid("Helm 来源存在独立修改，模板传播不会覆盖")
		}
	}
	if b.ReleaseTarget.Helm == nil {
		if b.ManifestBinding.ID == "" || len(to.Deployment.Source.Files) == 0 && to.Deployment.Source.Git == nil {
			return invalid("模板缺少固定的 Manifest 文件；先通过现有来源导入固化文件")
		}
	}
	var err error
	if b.ManifestBinding.ID != "" {
		_, err = s.deps.Manifests.PreviewTemplateAdoption(ctx, p, b.ManifestBinding.ID, templateSelection(b, to, parameters))
	} else {
		_, err = s.renderPreview(ctx, p, b, to, parameters)
	}
	if err != nil {
		return invalid("目标版本无法在当前环境渲染；检查变量和资源配置")
	}
	return nil
}

func (s *Service) GetPreview(ctx context.Context, p domainidentity.Principal, id string) (sohaapi.TemplateUpdatePreview, error) {
	preview, err := s.deps.Repository.GetPreview(ctx, id)
	if err != nil {
		return preview, err
	}
	visible := make([]sohaapi.TemplateUpdateCandidate, 0, len(preview.Candidates))
	for _, candidate := range preview.Candidates {
		if _, err := s.ReadCandidate(ctx, p, candidate.ID); err != nil {
			if errors.Is(err, apperrors.ErrAccessDenied) || errors.Is(err, apperrors.ErrNotFound) {
				continue
			}
			return sohaapi.TemplateUpdatePreview{}, err
		}
		visible = append(visible, candidate)
	}
	if len(visible) == 0 {
		return sohaapi.TemplateUpdatePreview{}, apperrors.ErrAccessDenied
	}
	// Scope filtering must not change the digest of the original frozen preview.
	preview.Candidates = visible
	return preview, nil
}

func (s *Service) Candidate(ctx context.Context, p domainidentity.Principal, id string, write bool) (domainupdate.FrozenCandidate, error) {
	frozen, err := s.deps.Repository.GetCandidate(ctx, id)
	if err != nil {
		return frozen, err
	}
	if frozen.Sensitive {
		if err := s.deps.Manifests.AuthorizeSensitiveTemplatePreview(ctx, p); err != nil {
			return domainupdate.FrozenCandidate{}, err
		}
	}
	binding, err := s.findBinding(ctx, p, frozen.Candidate.Target, write)
	if err != nil {
		return frozen, err
	}
	frozen.Binding = binding
	if !frozen.Candidate.Eligible {
		return frozen, invalid("candidate was not eligible in its frozen preview")
	}
	if frozen.AppliedPlanID != "" {
		return frozen, nil
	}
	if err := s.validateCandidatePolicy(ctx, frozen); err != nil {
		return frozen, err
	}
	if time.Now().After(frozen.ExpiresAt) {
		return frozen, conflict("preview expired; compare again")
	}
	if !binding.Target.Eligible || binding.Target.BindingRevision != frozen.Candidate.Target.BindingRevision {
		return frozen, conflict("application configuration changed; create a new preview")
	}
	versions, err := s.versions(ctx, p, binding.Target.Kind, binding.Target.TemplateID)
	if err != nil {
		return frozen, err
	}
	to, ok := findVersion(versions, frozen.Candidate.ToVersion)
	if !ok || !to.Enabled || to.Digest != frozen.TemplateDigest {
		return frozen, conflict("published template is unavailable or changed")
	}
	if (binding.Target.RequiresApproval || binding.Target.Mode == "approval") && !frozen.Candidate.RequiresApproval {
		return frozen, conflict("environment now requires approval; compare again")
	}
	return frozen, nil
}

func (s *Service) ReadCandidate(ctx context.Context, p domainidentity.Principal, id string) (domainupdate.FrozenCandidate, error) {
	frozen, err := s.deps.Repository.GetCandidate(ctx, id)
	if err != nil {
		return frozen, err
	}
	if frozen.Sensitive {
		if err := s.deps.Manifests.AuthorizeSensitiveTemplatePreview(ctx, p); err != nil {
			return domainupdate.FrozenCandidate{}, err
		}
	}
	frozen.Binding, err = s.findBinding(ctx, p, frozen.Candidate.Target, false)
	return frozen, err
}

func (s *Service) AuthorizeApproval(ctx context.Context, p domainidentity.Principal, id string) error {
	frozen, err := s.Candidate(ctx, p, id, false)
	if err != nil {
		return err
	}
	return s.authorizeBinding(ctx, p, frozen.Binding, true)
}

func (s *Service) CreateBatch(ctx context.Context, p domainidentity.Principal, previewID string, input sohaapi.TemplateUpdateBatchInput) (domainworkflow.DeliveryBatch, error) {
	var empty domainworkflow.DeliveryBatch
	preview, err := s.GetPreview(ctx, p, previewID)
	if err != nil {
		return empty, err
	}
	if input.PreviewDigest != preview.Digest {
		return empty, conflict("preview digest changed or expired")
	}
	if len(input.CandidateIDs) < 1 || len(input.CandidateIDs) > 100 || input.WaveSize < 1 || input.WaveSize > 100 || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 {
		return empty, invalid("select candidates, an idempotency key and a wave size between 1 and 100")
	}
	definition := domainworkflow.DeliveryWorkflowDefinition{Name: fmt.Sprintf("模板采用 %s v%d", preview.TemplateID, preview.ToVersion), Mode: domainworkflow.DeliveryModeBuildAll, StopOnFailure: &input.StopOnFailure, MaxConcurrency: int(min(input.WaveSize, 32)), Targets: []domainworkflow.DeliveryTargetInput{}}
	seen := map[string]bool{}
	for index, id := range input.CandidateIDs {
		if seen[id] {
			return empty, invalid("candidate selected twice")
		}
		seen[id] = true
		candidate, err := s.ReadCandidate(ctx, p, id)
		if err != nil {
			return empty, err
		}
		if candidate.PreviewID != previewID || !candidate.Candidate.Eligible {
			return empty, conflict("candidate belongs to another preview or was already adopted")
		}
		if candidate.Candidate.Cumulative.HistoryGap && !input.AcknowledgeHistoryGaps {
			return empty, invalid("acknowledge missing intermediate history before adopting the final version")
		}
		t := candidate.Candidate.Target
		definition.Targets = append(definition.Targets, domainworkflow.DeliveryTargetInput{ID: fmt.Sprintf("adopt-%d", index+1), ApplicationID: t.ApplicationID, ServiceID: t.ServiceID, ApplicationEnvironmentID: t.ApplicationEnvironmentID, ReleaseTargetID: t.ReleaseTargetID, Action: "config_update", TemplateCandidateID: id, Group: max(int(candidate.Candidate.Wave), index/int(input.WaveSize))})
	}
	batch, err := s.deps.Batches.CreateDeliveryBatch(domainupdate.WithPreviewBatch(ctx, previewID), p, domainworkflow.DeliveryBatchInput{IdempotencyKey: "template:" + digest([]string{previewID, input.IdempotencyKey}), Definition: &definition})
	if errors.Is(err, apperrors.ErrConflict) {
		return batch, err
	}
	return batch, err
}
