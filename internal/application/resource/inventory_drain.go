package resource

import (
	"errors"

	contractresource "github.com/opensoha/soha-contracts/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func nodeDrainFailure(err error) error {
	var failure *contractresource.NodeDrainError
	if !errors.As(err, &failure) || !contractresource.IsNodeDrainErrorCode(failure.Code) {
		return err
	}
	translations := map[string]string{
		"node_drain_pdb_blocked":      "PodDisruptionBudget 阻止了驱逐，排水未完成。请检查剩余 Pod 和调度状态，再决定是否重试。",
		"node_drain_eviction_blocked": "驱逐请求持续被拒绝，排水未完成。请检查 PDB 或 API 限流，以及剩余 Pod。",
		"node_drain_timeout":          "排水超时，可迁移 Pod 尚未全部终止。请检查剩余 Pod 和调度状态后再决定是否重试。",
		"node_drain_canceled":         "排水已中断，之前提交的驱逐仍可能完成。请检查剩余 Pod 和调度状态。",
		"node_drain_pod_selection":    "未能确认可驱逐的 Pod，请检查 Pod 读取权限和排水选项（无控制器或 emptyDir 本地数据）。",
		"node_drain_failed":           "排水未完成，请检查剩余 Pod、PDB 和权限后再决定是否重试。",
	}
	return apperrors.NewBusiness(apperrors.ErrConflict, failure.Code, failure.Error(), translations[failure.Code])
}
