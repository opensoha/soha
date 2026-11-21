package plugin

import (
	"fmt"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

var ErrRepositoryRateLimited = fmt.Errorf("%w: repository rate limit reached", apperrors.ErrServiceUnavailable)

var ErrRepositoryStableReleaseMissing = apperrors.NewBusiness(apperrors.ErrConflict,
	"plugin_repository_branch_required", "This repository has no stable release. Select a branch.",
	"此仓库没有稳定 Release，请选择分支。")
