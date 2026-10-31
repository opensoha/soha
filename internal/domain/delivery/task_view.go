package delivery

// PublicExecutionTask projects an already authorized task for a user response.
// Runner claim/status/callback paths retain the original internal task.
func PublicExecutionTask(task ExecutionTask) ExecutionTask {
	if task.CallbackToken != "" {
		// Keep the configured-callback signal without returning its authority.
		task.CallbackToken = "[redacted]"
	}
	task.SecretLease = nil
	if task.Payload == nil {
		task.Payload = map[string]any{}
	}
	return task
}
