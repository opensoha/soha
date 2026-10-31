package delivery

// PublicExecutionTask projects an already authorized task for a user response.
// Runner claim/status/callback paths retain the original internal task.
func PublicExecutionTask(task ExecutionTask) ExecutionTask {
	task.CallbackToken = ""
	task.SecretLease = nil
	if task.Payload == nil {
		task.Payload = map[string]any{}
	}
	return task
}
