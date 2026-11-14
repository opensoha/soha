package catalog

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type MigrationVersion struct {
	Number int64
	Digest string
	Policy *sohaapi.TemplateMigrationPolicy
}

// AssessInitialization is selected only after the owner establishes absence of
// prior writes. It is not a migration from a guessed historic template version.
func AssessInitialization(assessment *sohaapi.TemplateMigrationAssessment, to int64) *sohaapi.TemplateMigrationAssessment {
	if assessment == nil {
		assessment = &sohaapi.TemplateMigrationAssessment{Steps: []sohaapi.TemplateMigrationStep{}}
	}
	assessment.Initialization = true
	assessment.FromVersion = 0
	assessment.ToVersion = to
	assessment.Blocked = false
	assessment.RequiresApproval = true
	assessment.Reason = "新目标首次发布：仅创建不存在的资源；审批前核对外部数据和迁移指引，健康及原始UID确认后登记运行基线"
	return assessment
}

func ValidateMigrationPolicy(policy *sohaapi.TemplateMigrationPolicy) error {
	if policy == nil {
		return nil
	}
	if utf8.RuneCountInString(policy.Guide) > 2000 || ((policy.Sequential || policy.Irreversible) && strings.TrimSpace(policy.Guide) == "") {
		return fmt.Errorf("%w: migration declarations require a nonblank guide of at most 2000 characters", apperrors.ErrInvalidArgument)
	}
	return nil
}

// AssessMigration checks immutable version boundaries, never release notes or scripts.
func AssessMigration(versions []MigrationVersion, from, to int64) *sohaapi.TemplateMigrationAssessment {
	active := slices.ContainsFunc(versions, func(v MigrationVersion) bool {
		return v.Policy != nil && (v.Policy.Sequential || v.Policy.Irreversible)
	})
	if !active {
		return nil
	}
	result := &sohaapi.TemplateMigrationAssessment{FromVersion: from, ToVersion: to, Steps: []sohaapi.TemplateMigrationStep{}}
	if from == to {
		return result
	}
	low, high := min(from, to), max(from, to)
	if high-low > 200 {
		result.Blocked, result.Reason = true, "迁移版本区间超过200个版本，请分段核对"
		return result
	}
	known, steps := migrationSteps(versions, low, high)
	result.Steps = steps
	if reason := migrationHistoryError(known, from, to); reason != "" {
		result.Blocked, result.Reason = true, reason
		return result
	}
	for _, step := range result.Steps {
		result.RequiresApproval = true
		if step.Policy.Irreversible && to < from {
			result.Blocked = true
			result.Reason = fmt.Sprintf("v%d 声明不可逆数据变更，禁止普通回退；请按迁移指引审查前向修复", step.Version)
			return result
		}
		if step.Policy.Sequential && high-low > 1 {
			result.Blocked = true
			next := from + 1
			if to < from {
				next = from - 1
			}
			result.Reason = fmt.Sprintf("v%d 要求按相邻版本发布，请先完成 v%d；审批不能跳过该边界", step.Version, next)
			return result
		}
	}
	if result.RequiresApproval {
		result.Reason = "迁移变更需要Plan审批及明确审批意见；请先完成迁移指引中的人工前提"
	}
	return result
}

func migrationSteps(versions []MigrationVersion, low, high int64) (map[int64]bool, []sohaapi.TemplateMigrationStep) {
	known := map[int64]bool{}
	steps := []sohaapi.TemplateMigrationStep{}
	for _, v := range versions {
		known[v.Number] = true
		if v.Number > low && v.Number <= high && v.Policy != nil && (v.Policy.Sequential || v.Policy.Irreversible) {
			steps = append(steps, sohaapi.TemplateMigrationStep{Version: v.Number, ContentDigest: v.Digest, Policy: *v.Policy})
		}
	}
	slices.SortFunc(steps, func(a, b sohaapi.TemplateMigrationStep) int {
		if a.Version < b.Version {
			return -1
		}
		if a.Version > b.Version {
			return 1
		}
		return 0
	})
	return known, steps
}

func migrationHistoryError(known map[int64]bool, from, to int64) string {
	if from < 1 || !known[from] || !known[to] {
		return "迁移发布基线未知，请先核实成功发布版本；配置采用或审批不能替代运行证据"
	}
	low, high := min(from, to), max(from, to)
	for n := low; ; n++ {
		if !known[n] {
			return "迁移版本历史缺失，不能通过普通历史缺口确认跳过"
		}
		if n == high {
			break
		}
	}
	return ""
}

// MigrationBaselineVersion accepts only a receipt matching immutable published history.
func MigrationBaselineVersion(versions []MigrationVersion, templateID string, ref *sohaapi.TemplateVersionReference) int64 {
	if ref != nil && ref.TemplateID == templateID {
		for _, v := range versions {
			if v.Number == ref.TemplateVersion && v.Digest == ref.TemplateDigest && v.Digest != "" {
				return v.Number
			}
		}
	}
	return 0
}
