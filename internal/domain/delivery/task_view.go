package delivery

// PublicExecutionTask preserves the configured-callback signal without granting
// a task viewer the runner's callback capability. Internal/runner paths use the
// original task. The existing SDK string shape and Web presence check are kept.
func PublicExecutionTask(task ExecutionTask) ExecutionTask {
	if task.CallbackToken != "" {
		task.CallbackToken = "[redacted]"
	}
	return task
}

func PublicExecutionTasks(tasks []ExecutionTask) []ExecutionTask {
	views := make([]ExecutionTask, len(tasks))
	for i, task := range tasks {
		views[i] = PublicExecutionTask(task)
	}
	return views
}
