package templateupdate

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"slices"
)

type migrationBlockedError struct{ reason string }

func (e *migrationBlockedError) Error() string { return e.reason }
func (*migrationBlockedError) Unwrap() error   { return apperrors.ErrConflict }

func policyFailure(err error) string {
	var blocked *migrationBlockedError
	if errors.As(err, &blocked) {
		return "自动采用已拦截：" + blocked.reason
	}
	return "自动采用暂未完成：请核对执行身份权限、固定版本、变量与最新批次；修正后恢复策略"
}

func migrationVersions(versions []version) []domaincatalog.MigrationVersion {
	result := make([]domaincatalog.MigrationVersion, 0, len(versions))
	for _, v := range versions {
		result = append(result, domaincatalog.MigrationVersion{Number: v.Number, Digest: v.Digest, Policy: v.MigrationPolicy})
	}
	return result
}

func (s *Service) AssessRelease(ctx context.Context, p domainidentity.Principal, kind sohaapi.TemplateUpdateKind, id string, from, to int64) (*sohaapi.TemplateMigrationAssessment, error) {
	versions, err := s.versions(ctx, p, kind, id)
	if err != nil {
		return nil, err
	}
	return domaincatalog.AssessMigration(migrationVersions(versions), from, to), nil
}

func (s *Service) candidateMigration(ctx context.Context, p domainidentity.Principal, b domainupdate.Binding, versions []version, to int64) (*sohaapi.TemplateMigrationAssessment, error) {
	items := migrationVersions(versions)
	assessment := domaincatalog.AssessMigration(items, b.Target.FromVersion, to)
	if assessment == nil || b.Target.Kind != "deployment" {
		return assessment, nil
	}
	baseline, err := s.deps.Baselines.TemplateMigrationBaseline(ctx, p, b.Application.ID, b.Environment.ID, b.ReleaseTarget.ID)
	if err != nil {
		return nil, err
	}
	from := domaincatalog.MigrationBaselineVersion(items, b.Target.TemplateID, baseline)
	runtime := domaincatalog.AssessMigration(items, from, to)
	if runtime.Blocked {
		return runtime, nil
	}
	if assessment.Blocked {
		return assessment, nil
	}
	runtime.RequiresApproval = runtime.RequiresApproval || assessment.RequiresApproval
	if runtime.Reason == "" && assessment.RequiresApproval {
		runtime.Reason = assessment.Reason
	}
	for _, step := range assessment.Steps {
		if !slices.ContainsFunc(runtime.Steps, func(existing sohaapi.TemplateMigrationStep) bool { return existing.Version == step.Version }) {
			runtime.Steps = append(runtime.Steps, step)
		}
	}
	slices.SortFunc(runtime.Steps, func(a, b sohaapi.TemplateMigrationStep) int {
		if a.Version < b.Version {
			return -1
		}
		if a.Version > b.Version {
			return 1
		}
		return 0
	})
	return runtime, nil
}

func sameMigration(a, b *sohaapi.TemplateMigrationAssessment) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func (s *Service) ReleaseReference(ctx context.Context, p domainidentity.Principal, id string, n int64) (*sohaapi.TemplateVersionReference, error) {
	values, err := s.versions(ctx, p, "deployment", id)
	if err != nil {
		return nil, err
	}
	v, ok := findVersion(values, n)
	if !ok || !v.Enabled {
		return nil, conflict("published template is unavailable")
	}
	return &sohaapi.TemplateVersionReference{TemplateID: id, TemplateVersion: n, TemplateDigest: v.Digest}, nil
}
