package templateupdate

import (
	"context"
	"encoding/json"
	"maps"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
)

// Apply is called only by the existing confirmed configuration Plan. The
// candidate receipt and owning configuration CAS share the same transaction.
func (s *Service) Apply(ctx context.Context, p domainidentity.Principal, id, planID string, complete func(context.Context) error) error {
	if planID == "" {
		return invalid("a confirmed template adoption Plan is required")
	}
	if complete == nil {
		return invalid("configuration adoption requires its Plan receipt callback")
	}
	return s.deps.Repository.ApplyCandidate(ctx, id, planID, func(ctx context.Context, _ domainupdate.FrozenCandidate) error {
		frozen, err := s.Candidate(ctx, p, id, true)
		if err != nil {
			return err
		}
		b := frozen.Binding
		versions, err := s.versions(ctx, p, b.Target.Kind, b.Target.TemplateID)
		if err != nil {
			return err
		}
		to, ok := findVersion(versions, frozen.Candidate.ToVersion)
		if !ok {
			return conflict("published target version no longer exists")
		}
		err = s.adoptCandidate(ctx, p, frozen, to, planID)
		if err != nil {
			return err
		}
		return complete(ctx)
	})
}

func (s *Service) adoptCandidate(ctx context.Context, p domainidentity.Principal, frozen domainupdate.FrozenCandidate, to version, planID string) error {
	b := frozen.Binding
	ctx = domainupdate.WithAdoption(ctx, string(b.Target.Kind), b.Application.ID, planID)
	switch b.Target.Kind {
	case "deployment":
		if b.ReleaseTarget.Helm != nil {
			return s.adoptHelm(ctx, p, frozen, to, planID)
		}
		return s.deps.Manifests.AdoptTemplate(ctx, p, b.ManifestBinding.ID, b.ManifestBinding.Version, domainmanifest.TemplateSelection{TemplateID: b.Target.TemplateID, Version: to.Number, Parameters: frozen.Candidate.Parameters, BaseFilesDigest: digest(b.Package.Files), AdoptionID: planID})
	case "build":
		return s.adoptBuild(ctx, p, frozen, to, planID)
	case "workflow":
		return s.adoptWorkflow(ctx, p, frozen, to)
	default:
		return invalid("creation-only templates cannot mutate existing applications")
	}
}

func (s *Service) adoptBuild(ctx context.Context, p domainidentity.Principal, frozen domainupdate.FrozenCandidate, to version, planID string) error {
	b := frozen.Binding
	var input domainapp.UpsertInput
	if err := copyJSON(b.Application, &input); err != nil {
		return err
	}
	input.ExpectedVersion = &b.Application.Version
	for index, source := range input.BuildSources {
		if source.ID != b.BuildSourceID {
			continue
		}
		config := maps.Clone(source.Config)
		config["buildTemplateVersion"], config["variables"], config["templateAdoptionId"] = to.Number, map[string]any(frozen.Candidate.Parameters), planID
		input.BuildSources[index].Config = config
	}
	_, err := s.deps.Applications.Update(ctx, p, b.Application.ID, input)
	return err
}

func (s *Service) adoptWorkflow(ctx context.Context, p domainidentity.Principal, frozen domainupdate.FrozenCandidate, to version) error {
	b := frozen.Binding
	input, err := environmentInput(b.Environment)
	if err != nil {
		return err
	}
	input.WorkflowTemplateVersion = to.Number
	_, err = s.deps.Environments.UpdateApplicationEnvironment(ctx, p, b.Environment.ID, input)
	return err
}

func (s *Service) adoptHelm(ctx context.Context, p domainidentity.Principal, frozen domainupdate.FrozenCandidate, to version, planID string) error {
	b := frozen.Binding
	preview, err := s.renderPreview(ctx, p, b, to, frozen.Candidate.Parameters)
	if err != nil {
		return err
	}
	if preview.Source.Helm == nil {
		return invalid("target template has no native Helm source")
	}
	input, err := environmentInput(b.Environment)
	if err != nil {
		return err
	}
	for index, target := range input.Targets {
		if target.ID != b.ReleaseTarget.ID {
			continue
		}
		var source sohaapi.DeploymentTemplateHelmSource
		if err := copyJSON(preview.Source.Helm, &source); err != nil {
			return err
		}
		target.Helm.Source = source // Target-level Values and image mappings remain independent overrides.
		target.Metadata = maps.Clone(target.Metadata)
		if target.Metadata == nil {
			target.Metadata = map[string]any{}
		}
		target.Metadata["templateSelection"] = domainmanifest.TemplateSelection{TemplateID: b.Target.TemplateID, Version: to.Number, Parameters: frozen.Candidate.Parameters, BaseFilesDigest: digest(target.Helm.Source), AdoptionID: planID}
		input.Targets[index] = target
	}
	_, err = s.deps.Environments.UpdateApplicationEnvironment(ctx, p, b.Environment.ID, input)
	return err
}

func environmentInput(environment domaincatalog.ApplicationEnvironment) (domaincatalog.ApplicationEnvironmentInput, error) {
	var input domaincatalog.ApplicationEnvironmentInput
	err := copyJSON(environment, &input)
	input.ExpectedUpdatedAt = &environment.UpdatedAt
	return input, err
}

func copyJSON(from, to any) error {
	body, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, to)
}
