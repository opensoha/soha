package sohacli

func catalogSpec(name, usage, summary string, actions ...string) commandSpec {
	return commandSpec{Name: name, Usage: usage, Summary: summary, CompletionWords: actions}
}

func observeCommandSpec() commandSpec {
	return commandSpec{Name: "observe", Usage: "soha observe <summary|metrics|traces|alerts|events|healing-runs> [options]", Summary: "Query telemetry and explicitly operate scoped alerts and healing runs", Handler: runObserve, Subcommands: []commandSpec{
		catalogSpec("summary", "soha observe summary [options]", "Read monitoring summary"),
		catalogSpec("metrics", "soha observe metrics query --input FILE|- [options]", "Query a bounded metric window", "query"),
		catalogSpec("traces", "soha observe traces query --input FILE|- [options]", "Query bounded authorized traces", "query"),
		catalogSpec("alerts", "soha observe alerts <list|get|assign|acknowledge> [ID] [options]", "Read or explicitly claim and acknowledge alerts", "list", "get", "assign", "acknowledge"),
		catalogSpec("events", "soha observe events <list|get|acknowledge|resolve|heal> [ID] [options]", "Read and operate alert events; heal requires --policy-id", "list", "get", "acknowledge", "resolve", "heal"),
		catalogSpec("healing-runs", "soha observe healing-runs <list|get|request|approve|reject|retry> [ID] [options]", "Recover an original request or decide a healing run under current server policy", "list", "get", "request", "approve", "reject", "retry"),
	}}
}
func directoryCommandSpec() commandSpec {
	return commandSpec{Name: "directory", Usage: "soha directory <sync|runs|runtime|events> [options]", Summary: "Preview and operate directory synchronization without exporting connector credentials", Handler: runDirectory, Subcommands: []commandSpec{
		catalogSpec("sync", "soha directory sync <preview|start|request> [UUID] --connection-id ID [--idempotency-key UUID] [--yes] [options]", "Preview, start once with an original UUID, or query that request without replaying it", "preview", "start", "request", "--idempotency-key", "--connection-id"),
		catalogSpec("runs", "soha directory runs <list|get|cancel> [RUN] [--connection-id ID] [--yes] [options]", "Read runs or cancel one original run", "list", "get", "cancel"),
		catalogSpec("runtime", "soha directory runtime get --connection-id ID [options]", "Read callback, reconciliation and backlog state", "get"),
		catalogSpec("events", "soha directory events <list|retry> [EVENT] --connection-id ID [--yes] [options]", "Read events or retry one failed original event", "list", "retry"),
	}}
}
func networkAccessCommandSpec() commandSpec {
	return commandSpec{Name: "network-access", Usage: "soha network-access <sessions|telemetry|enrollments|access-grants|policy|conflicts> [options]", Summary: "Inspect access and submit server-validated session plans, grants and enrollment operations", Handler: runNetworkAccess, Subcommands: []commandSpec{
		catalogSpec("sessions", "soha network-access sessions <list|get|plan|execute|command|request> [ID|PLAN_HASH] [--session-id ID] [options]", "Review session actions; observe an original command by ID or the reviewed planHash without replay", "list", "get", "plan", "execute", "command", "request"),
		catalogSpec("telemetry", "soha network-access telemetry get [options]", "Read network access telemetry", "get"),
		catalogSpec("enrollments", "soha network-access enrollments <list|get|create|revoke> [ID] [options]", "Issue or revoke bounded runtime enrollment; --show-access-details explicitly exports one-time credentials", "list", "get", "create", "revoke"),
		catalogSpec("access-grants", "soha network-access access-grants <list|get|create|revoke> [ID] [options]", "Issue or revoke scoped access; recent server MFA still applies to creation", "list", "get", "create", "revoke"),
		catalogSpec("policy", "soha network-access policy <snapshot|preview> [options]", "Read compiled policy or preview one authorized decision", "snapshot", "preview"),
		catalogSpec("conflicts", "soha network-access conflicts analyze --input FILE|- [options]", "Analyze explicit runtime network ranges", "analyze"),
	}}
}
