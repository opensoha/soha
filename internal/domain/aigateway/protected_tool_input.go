package aigateway

const (
	ProtectedResourceUpdateTool = "k8s.resources.conditional.update"
	ProtectedPodExecTool        = "k8s.pods.exec"
)

func RequiresProtectedToolInput(tool string) bool {
	return tool == ProtectedResourceUpdateTool || tool == ProtectedPodExecTool
}

func RequiresProtectedPlanInput(plan CapabilityPlan) bool {
	for _, step := range plan.Steps {
		if RequiresProtectedToolInput(step.Call.ToolName) {
			return true
		}
	}
	return false
}
