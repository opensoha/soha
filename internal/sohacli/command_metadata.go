package sohacli

import (
	"context"
	"fmt"
	"strings"
)

type commandHandler func(context.Context, []string, Runtime) error

type commandSpec struct {
	Name            string
	Aliases         []string
	Usage           string
	Summary         string
	Examples        []string
	CompletionWords []string
	Subcommands     []commandSpec
	Handler         commandHandler
}

var topLevelCommandSpecs = []commandSpec{
	observeCommandSpec(),
	directoryCommandSpec(),
	networkAccessCommandSpec(),
	{Name: "tui", Usage: "soha tui [--profile NAME] [--cluster-id ID] [--namespace NAME]", Summary: "Browse workbenches, chat and review explicit task or approval actions in an interactive terminal", Examples: []string{"soha tui --profile local --cluster-id cluster-1 --namespace default"}, Handler: runTUI},
	{
		Name:     "version",
		Usage:    "soha version [--json]",
		Summary:  "Print build version information",
		Examples: []string{"soha version", "soha version --json"},
		Handler:  func(_ context.Context, args []string, rt Runtime) error { return runVersion(args, rt) },
	},
	{
		Name:     "login",
		Usage:    "soha login [options]",
		Summary:  "Authenticate and store a local profile",
		Examples: []string{"soha login --server https://soha.example.com", "soha login --device-code --no-browser --credential-store file", "soha login --method password --login ada --password-stdin", "soha login --with-token --credential-store file"},
		Handler:  runLogin,
	},
	{Name: "auth", Usage: "soha auth <status|sessions|revoke-session|mfa> [options]", Summary: "Inspect authentication and manage your CLI sessions", Subcommands: []commandSpec{
		{Name: "mfa", Usage: "soha auth mfa <status|totp|verify> [options]", Summary: "Complete recent MFA in the current CLI session; Keys cannot borrow another session", Subcommands: []commandSpec{
			{Name: "status", Usage: "soha auth mfa status [--profile NAME]", Summary: "Read your MFA credential metadata"},
			{Name: "totp", Usage: "soha auth mfa totp [--profile NAME] [--show-enrollment]", Summary: "Start a TOTP enrollment or step-up challenge; explicitly opt in to the one-time provisioning URI"},
			{Name: "verify", Usage: "soha auth mfa verify --challenge-id UUID --response-stdin [--profile NAME]", Summary: "Verify a TOTP challenge using a code from stdin without logging the code"},
		}},
		{Name: "status", Usage: "soha auth status [--profile NAME] [--json] [--offline]", Summary: "Verify effective authentication or inspect cached metadata", Examples: []string{"soha auth status --json", "soha auth status --offline"}},
		{Name: "sessions", Usage: "soha auth sessions [--profile NAME] [--limit N] [--cursor VALUE]", Summary: "List your CLI sessions", Examples: []string{"soha auth sessions --profile local --limit 20"}},
		{Name: "revoke-session", Usage: "soha auth revoke-session --id ID [--profile NAME]", Summary: "Revoke one of your CLI sessions", Examples: []string{"soha auth revoke-session --id 11111111-1111-4111-8111-111111111111 --profile local"}},
	}, Handler: runAuth},
	{Name: "whoami", Usage: "soha whoami [--profile NAME] [--json]", Summary: "Show server-confirmed identity and permissions", Examples: []string{"soha whoami --json"}, Handler: runWhoami},
	{Name: "logout", Usage: "soha logout [--profile NAME] [--local-only|--revoke]", Summary: "Revoke the CLI session or clear a locally stored Key", Examples: []string{"soha logout --profile local", "soha logout --local-only"}, Handler: runLogout},
	{
		Name:     "capabilities",
		Usage:    "soha capabilities [--domain gateway|platform] [--output json|yaml|names|inputs]",
		Summary:  "Print AI Gateway or platform capability metadata",
		Examples: []string{"soha capabilities --output names", "soha capabilities --output inputs"},
		Handler:  runCapabilities,
	},
	{
		Name:    "logs",
		Usage:   "soha logs <query|tail> [options]",
		Summary: "Query or follow operational logs",
		Subcommands: []commandSpec{
			{Name: "query", Usage: "soha logs query --source <cluster|docker|delivery> [options]", Summary: "Query a bounded page of logs", Examples: []string{"soha logs query --source cluster --cluster-id local --namespace default --output json", "soha logs query --source docker --project-id project-1 --service api --output ndjson"}},
			{Name: "tail", Usage: "soha logs tail --source <cluster|docker|delivery> [options]", Summary: "Follow logs as NDJSON", Examples: []string{"soha logs tail --source cluster --cluster-id local --namespace default", "soha logs tail --source delivery --application-id app-1 --environment-id env-1"}},
		},
		Handler: runLogs,
	},
	{
		Name:    "operation",
		Usage:   "soha operation <get|wait|cancel> <domain> <id> [options]",
		Summary: "Inspect and control asynchronous operations",
		Subcommands: []commandSpec{
			{Name: "get", Usage: "soha operation get <virtualization|container_runtime> <id> [options]", Summary: "Get an operation", Examples: []string{"soha operation get virtualization task-1 --output json"}},
			{Name: "wait", Usage: "soha operation wait <virtualization|container_runtime> <id> [options]", Summary: "Wait for an operation to finish", Examples: []string{"soha operation wait container_runtime task-1 --wait-timeout 10m --output json"}},
			{Name: "cancel", Usage: "soha operation cancel <virtualization|container_runtime> <id> [options]", Summary: "Cancel an operation", Examples: []string{"soha operation cancel virtualization task-1 --yes"}},
		},
		Handler: runOperation,
	},
	{
		Name: "k8s", Usage: "soha k8s <clusters|namespaces|resources|workloads|rollout|events|logs|create> <action> [options]",
		Summary: "Operate Kubernetes through Soha permissions and registered cluster connections", Examples: []string{"soha k8s clusters list --profile local"},
		Subcommands: []commandSpec{
			{Name: "clusters", Usage: "soha k8s clusters <list|get> [--cluster-id ID] [options]", Summary: "List authorized clusters or describe one cluster", Examples: []string{"soha k8s clusters list --profile local", "soha k8s clusters get --cluster-id cluster-1 --profile local"}, CompletionWords: []string{"list", "get"}},
			{Name: "namespaces", Usage: "soha k8s namespaces list --cluster-id ID [options]", Summary: "List authorized namespaces", Examples: []string{"soha k8s namespaces list --cluster-id cluster-1 --profile local"}, CompletionWords: []string{"list"}},
			{Name: "pods", Usage: "soha k8s pods <exec|execution|terminal|terminal-session|terminal-close> POD --cluster-id ID [options] [-- argv...]", Summary: "Run bounded argv or an explicit native terminal; query and close original sessions", CompletionWords: []string{"exec", "execution", "terminal", "terminal-session", "terminal-close"}, Examples: []string{"soha k8s pods exec api-1 --cluster-id cluster-1 --expected-uid UID --idempotency-key exec-1 --yes --profile local -- printf hello", "soha k8s pods execution api-1 --cluster-id cluster-1 --idempotency-key exec-1 --profile local", "soha k8s pods terminal api-1 --cluster-id cluster-1 --expected-uid UID --yes --profile local", "soha k8s pods terminal-session api-1 --cluster-id cluster-1 --session-id UUID --profile local", "soha k8s pods terminal-close api-1 --cluster-id cluster-1 --session-id UUID --yes --profile local"}},
			{Name: "rollout", Usage: "soha k8s rollout <status|history> Deployment NAME --cluster-id ID [options]", Summary: "Read current server rollout conditions or bounded Deployment revision history", CompletionWords: []string{"status", "history", "Deployment"}, Examples: []string{"soha k8s rollout status Deployment api --cluster-id cluster-1 --profile local", "soha k8s rollout history Deployment api --cluster-id cluster-1 --profile local"}},
			{Name: "workloads", Usage: "soha k8s workloads <restart|scale|rollback> KIND NAME --cluster-id ID --expected-uid UID --expected-resource-version RV [options]", Summary: "Preflight and perform an authorized workload action against a reviewed identity", CompletionWords: []string{"restart", "scale", "rollback", "Deployment", "StatefulSet", "DaemonSet"}, Examples: []string{"soha k8s workloads scale Deployment api --cluster-id cluster-1 --replicas 3 --expected-uid UID --expected-resource-version RV --dry-run --profile local", "soha k8s workloads restart StatefulSet database --cluster-id cluster-1 --expected-uid UID --expected-resource-version RV --yes --profile local", "soha k8s workloads rollback Deployment api --cluster-id cluster-1 --revision 2 --expected-uid UID --expected-resource-version RV --dry-run --profile local"}},
			{Name: "resources", Usage: "soha k8s resources <list|get|identity|yaml|apply|delete> KIND [NAME] --cluster-id ID [options]", Summary: "Read typed resources or mutate a reviewed UID and resourceVersion", CompletionWords: []string{"list", "get", "identity", "yaml", "apply", "delete", "Pod", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "ReplicationController", "Job", "CronJob", "Node", "ConfigMap", "Secret", "Service", "Ingress", "PersistentVolumeClaim", "PersistentVolume", "StorageClass", "ServiceAccount", "Role", "RoleBinding"}, Examples: []string{"soha k8s resources list Deployment --cluster-id cluster-1 --namespace default --profile local", "soha k8s resources identity Deployment api --cluster-id cluster-1 --namespace default --profile local", "soha k8s resources yaml Deployment api --cluster-id cluster-1 --namespace default --output raw --profile local", "soha k8s resources apply Deployment api --cluster-id cluster-1 --namespace default --file api.yaml --expected-uid UID --expected-resource-version RV --dry-run --profile local", "soha k8s resources delete Pod api-1 --cluster-id cluster-1 --namespace default --expected-uid UID --expected-resource-version RV --yes --profile local"}},
			{Name: "events", Usage: "soha k8s events list --cluster-id ID [--namespace NAME] [options]", Summary: "Read bounded authorized cluster events", Examples: []string{"soha k8s events list --cluster-id cluster-1 --namespace default --limit 100 --profile local"}, CompletionWords: []string{"list"}},
			{Name: "logs", Usage: "soha k8s logs <query|tail> --cluster-id ID [options]", Summary: "Query or follow the existing bounded Kubernetes log API", Examples: []string{"soha k8s logs tail --cluster-id cluster-1 --namespace default --profile local"}, CompletionWords: []string{"query", "tail"}},
			{Name: "create", Usage: "soha k8s create <preflight|execute> --cluster-id ID --file YAML [--idempotency-key KEY] [options]", Summary: "Preflight manifests and create through the durable resource batch", CompletionWords: []string{"preflight", "execute"}, Examples: []string{"soha k8s create preflight --cluster-id cluster-1 --file workload.yaml --profile local", "soha k8s create execute --cluster-id cluster-1 --file workload.yaml --idempotency-key create-1 --yes --profile local"}},
		}, Handler: runKubernetes,
	},
	{
		Name:    "compute",
		Usage:   "soha compute <capabilities|overview|access-sources|providers|provider-instances|resources|tasks|capacity|vms> [options]",
		Summary: "Inspect and operate the unified compute resource surface",
		Subcommands: []commandSpec{
			{Name: "capabilities", Usage: "soha compute capabilities [options]", Summary: "Show compute capability rollout and availability", Examples: []string{"soha compute capabilities --output yaml"}},
			{Name: "overview", Usage: "soha compute overview [options]", Summary: "Show the permission-filtered compute overview", Examples: []string{"soha compute overview"}},
			{Name: "access-sources", Usage: "soha compute access-sources list [options]", Summary: "List compute access sources", CompletionWords: []string{"list"}},
			{Name: "providers", Usage: "soha compute providers list [options]", Summary: "List compute provider descriptors", CompletionWords: []string{"list"}},
			{Name: "provider-instances", Usage: "soha compute provider-instances <list|get|health|discover> [options]", Summary: "Inspect and refresh provider instances", CompletionWords: []string{"list", "get", "health", "discover"}},
			{Name: "resources", Usage: "soha compute resources <get|relations|action> <domain> <kind> <id> [action] [options]", Summary: "Read resource data, relations, or run a typed action", CompletionWords: []string{"get", "relations", "action"}},
			{Name: "capacity", Usage: "soha compute capacity <check|policy get|policy set> [connection-id] [--input file|-] [options]", Summary: "Inspect both admission gates and manage bounded cluster overcommit", CompletionWords: []string{"check", "policy", "get", "set"}, Examples: []string{"soha compute capacity check --input capacity.json --profile local", "soha compute capacity policy get connection-1 --profile local"}},
			{Name: "vms", Usage: "soha compute vms <list|get|detail|devices|metrics|plan|create|power|action|console|access get|access set|shell> [vm-id] [options]", Summary: "Read VM state, preflight creation, submit durable operations or use authorized access", CompletionWords: []string{"list", "get", "detail", "devices", "metrics", "plan", "create", "power", "action", "console", "access", "set", "shell"}, Examples: []string{"soha compute vms list --page 1 --page-size 50 --profile local", "soha compute vms plan --input create.json --profile local", "soha compute vms create --input create.json --idempotency-key vm-create-1 --yes --profile local", "soha compute vms power vm-1 --input power.json --idempotency-key vm-start-1 --yes --profile local", "soha compute vms access set vm-1 --input binding.json --profile local", "soha compute vms shell vm-1 --profile local"}},
			{Name: "tasks", Usage: "soha compute tasks <list|get|logs|cancel|retry|wait> [options]", Summary: "List, inspect, and control compute tasks", CompletionWords: []string{"list", "get", "logs", "cancel", "retry", "wait"}},
		},
		Examples: []string{"soha compute overview", "soha compute resources relations virtualization vm vm-1", "soha compute tasks list --status failed"},
		Handler:  runCompute,
	},
	dockerCommandSpec(),
	{
		Name:    "tool",
		Usage:   "soha tool call <name> [--preview] [--yes] [options]",
		Summary: "Invoke an AI Gateway tool with JSON input",
		Subcommands: []commandSpec{
			{
				Name:     "call",
				Usage:    "soha tool call <name> [--preview] [--yes] [options]",
				Summary:  "Invoke an AI Gateway tool with JSON input",
				Examples: []string{"soha tool call k8s.pods.list --input-json '{\"clusterId\":\"local\"}'", "soha tool call k8s.deployments.restart --preview --input-json '{\"clusterId\":\"local\"}'", "soha tool call k8s.deployments.restart --yes --input-json '{\"clusterId\":\"local\"}'"},
			},
		},
		Handler: runTool,
	},
	{
		Name:    "project",
		Usage:   "soha project <plan|apply> [file] [options]",
		Summary: "Plan or apply a dependency-ordered Soha project manifest",
		Subcommands: []commandSpec{
			{
				Name:     "plan",
				Usage:    "soha project plan [file] [--output json|yaml] [options]",
				Summary:  "Validate a project manifest and invoke side-effect-free plans",
				Examples: []string{"soha project plan", "soha project plan .soha/project.yaml --output yaml"},
			},
			{
				Name:     "apply",
				Usage:    "soha project apply [file] [--yes] [options]",
				Summary:  "Apply project steps in dependency order",
				Examples: []string{"soha project apply .soha/project.yaml", "soha project apply --yes"},
			},
		},
		Handler: runProject,
	},
	{
		Name:    "delivery",
		Usage:   "soha delivery <applications|services|environments|environment-catalog|tasks|blueprints|manifests|manifest-bindings|manifest-deployments|batches|workflows|plans|documents|template-sources|triggers> <action> [options]",
		Summary: "Manage application configuration, reviewed delivery plans and durable execution tasks",
		Subcommands: []commandSpec{
			catalogSpec("executions", "soha delivery executions <list|get|trigger|request|approve|reject|runtime> [RUN] [options]", "Read or explicitly operate workflow executions; trigger input requires idempotencyKey and workflow, request KEY reads the original receipt without replay", "list", "get", "trigger", "request", "approve", "reject", "runtime"),
			{Name: "applications", Usage: "soha delivery applications <list|get|detail|runtime|create|update|delete> [id] [options]", Summary: "Read or change scoped application configuration; creation needs a stable input ID and updates need expectedVersion", CompletionWords: []string{"list", "get", "detail", "runtime", "create", "update", "delete"}, Examples: []string{"soha delivery applications list --search api --limit 20 --profile local", "soha delivery applications update app-1 --input reviewed-app.json --yes --profile local"}},
			{Name: "services", Usage: "soha delivery services <list|get|create|update|delete> [id] --application-id <id> [options]", Summary: "Manage application services with an explicit owning application and reviewed update version", CompletionWords: []string{"list", "get", "create", "update", "delete"}, Examples: []string{"soha delivery services list --application-id app-1 --profile local", "soha delivery services create --application-id app-1 --input service.json --yes --profile local"}},
			{Name: "environments", Usage: "soha delivery environments <list|get|detail|create|update|delete> [id] [options]", Summary: "Manage application environment bindings; updates require the reviewed expectedUpdatedAt", CompletionWords: []string{"list", "get", "detail", "create", "update", "delete"}, Examples: []string{"soha delivery environments list --application-id app-1 --environment-id env-dev --profile local", "soha delivery environments update binding-1 --input reviewed-binding.json --yes --profile local"}},
			{Name: "environment-catalog", Usage: "soha delivery environment-catalog list [options]", Summary: "Read the authorized base environment catalog", CompletionWords: []string{"list"}, Examples: []string{"soha delivery environment-catalog list --profile local"}},
			{Name: "tasks", Usage: "soha delivery tasks <list|get|logs|artifacts|rollout|rollout-control|cancel|retry|wait> [id] [options]", Summary: "Observe durable execution, control it through action-scoped authorization, or wait for an actual terminal state", CompletionWords: []string{"list", "get", "logs", "artifacts", "rollout", "rollout-control", "cancel", "retry", "wait"}, Examples: []string{"soha delivery tasks list --application-id app-1 --status running --profile local", "soha delivery tasks logs task-1 --limit 200 --profile local", "soha delivery tasks cancel task-1 --reason reviewed --yes --profile local", "soha delivery tasks wait task-1 --wait-timeout 10m --profile local", "soha delivery tasks rollout-control task-1 --input rollout-control.json --yes --profile local"}},
			{Name: "blueprints", Usage: "soha delivery blueprints <list|usage|render> [id] [options]", Summary: "Read authorized delivery blueprints or explicitly render their configuration without claiming deployment", CompletionWords: []string{"list", "usage", "render"}, Examples: []string{"soha delivery blueprints list --profile local", "soha delivery blueprints render blueprint-1 --yes --profile local"}},
			{Name: "manifests", Usage: "soha delivery manifests <list|get|revisions|bindings> [id] [options]", Summary: "Read scoped manifest packages, immutable revisions and delivery bindings", CompletionWords: []string{"list", "get", "revisions", "bindings"}, Examples: []string{"soha delivery manifests list --application-id app-1 --page 1 --page-size 20 --profile local", "soha delivery manifests revisions package-1 --profile local", "soha delivery manifests bindings package-1 --profile local"}},
			{Name: "manifest-bindings", Usage: "soha delivery manifest-bindings <create|update|delete|set-revision> [id] [options]", Summary: "Bind reviewed configuration to an environment or select a desired revision under server policy", CompletionWords: []string{"create", "update", "delete", "set-revision"}, Examples: []string{"soha delivery manifest-bindings create --package-id package-1 --input binding.json --yes --profile local", "soha delivery manifest-bindings update binding-1 --input reviewed-binding.json --yes --profile local", "soha delivery manifest-bindings set-revision binding-1 --input revision.json --yes --profile local"}},
			{Name: "manifest-deployments", Usage: "soha delivery manifest-deployments <list|get|reconcile|repair|adopt|rollback> [id] [options]", Summary: "Read desired/observed deployment state and request generation-checked durable controls", CompletionWords: []string{"list", "get", "reconcile", "repair", "adopt", "rollback"}, Examples: []string{"soha delivery manifest-deployments list --application-id app-1 --profile local", "soha delivery manifest-deployments get deployment-1 --profile local", "soha delivery manifest-deployments rollback deployment-1 --input reviewed-rollback.json --yes --profile local"}},
			{Name: "triggers", Usage: "soha delivery triggers <list|get|create|update|events> [id] [options]", Summary: "Configure signed Git webhooks, polling and release calendars; send credentials through stdin or a private file", CompletionWords: []string{"list", "get", "create", "update", "events"}, Examples: []string{"soha delivery triggers list --target-kind workflow --target-id workflow-1 --profile local", "soha delivery triggers create --input - --yes --profile local", "soha delivery triggers update trigger-1 --input trigger.json --yes --profile local", "soha delivery triggers events trigger-1 --limit 50 --offset 0 --profile local"}},
			{Name: "documents", Usage: "soha delivery documents <validate|preview|import|export|source> [options]", Summary: "Validate YAML/JSON offline, preview authorized changes, import reviewed drafts, or export a version", CompletionWords: []string{"validate", "preview", "import", "export", "source"}, Examples: []string{"soha delivery documents validate --file build.yaml", "soha delivery documents preview --file build.yaml --file workflow.json --profile local", "soha delivery documents preview --input import-request.json --profile local", "soha delivery documents import --preview-id preview-1 --candidate-digest sha256:reviewed --idempotency-key import-attempt-1 --yes --profile local", "soha delivery documents export BuildTemplate template-1 --version 2 --format yaml --out build.yaml --profile local"}},
			{Name: "template-sources", Usage: "soha delivery template-sources <list|get|create|update|objects|sync|runs|run|apply|detach|remove> [id] [options]", Summary: "Manage Git sources, review immutable sync previews, and import drafts without publishing or executing", CompletionWords: []string{"list", "get", "create", "update", "objects", "sync", "runs", "run", "apply", "detach", "remove"}, Examples: []string{"soha delivery template-sources list --profile local", "soha delivery template-sources sync source-1 --input sync.json --yes --profile local", "soha delivery template-sources run source-1 --run-id run-1 --profile local", "soha delivery template-sources apply source-1 --run-id run-1 --input apply.json --yes --profile local", "soha delivery template-sources objects source-1 --limit 50 --offset 0 --profile local", "soha delivery documents source BuildTemplate template-1 --version 2 --profile local"}},
			{Name: "batches", Usage: "soha delivery batches <list|get|create|cancel> [id] [options]", Summary: "Inspect, start, or stop a delivery batch", CompletionWords: []string{"list", "get", "create", "cancel"}, Examples: []string{"soha delivery batches list --application-id app-1 --profile local", "soha delivery batches create --input batch.json --yes --profile local", "soha delivery batches cancel batch-1 --reason paused --yes --profile local"}},
			{Name: "workflows", Usage: "soha delivery workflows <list|get|create|update> [id] [options]", Summary: "Save versioned release workflows without starting execution", CompletionWords: []string{"list", "get", "create", "update"}, Examples: []string{"soha delivery workflows create --input workflow.json --yes --profile local", "soha delivery workflows update workflow-1 --input workflow.json --yes --profile local"}},
			{Name: "plans", Usage: "soha delivery plans <create|get|confirm|approve|reject> [id] [options]", Summary: "Inspect or decide a final deployment plan under environment policy", CompletionWords: []string{"create", "get", "confirm", "approve", "reject"}, Examples: []string{"soha delivery plans create --input plan.json --yes --profile local", "soha delivery plans confirm plan-1 --yes --profile local", "soha delivery plans get plan-1 --profile local", "soha delivery plans approve plan-1 --comment reviewed --yes --profile local"}},
		},
		Handler: runDelivery,
	},
	{
		Name:    "resource",
		Usage:   "soha resource read <uri> [options]",
		Summary: "Read an AI Gateway MCP resource",
		Subcommands: []commandSpec{
			{
				Name:     "read",
				Usage:    "soha resource read <uri> [options]",
				Summary:  "Read an AI Gateway MCP resource",
				Examples: []string{"soha resource read soha://k8s/runtime --context-json '{\"clusterId\":\"local\"}'"},
			},
		},
		Handler: runResource,
	},
	{
		Name:    "knowledge",
		Usage:   "soha knowledge <bases|sources|documents|sync-runs|index-revisions|search|connectors|sync|rebuild> [options]",
		Summary: "Search and operate authorized AI knowledge pipelines",
		Subcommands: []commandSpec{
			catalogSpec("bases", "soha knowledge bases <list|get|create|update|delete> [ID] [options]", "Manage authorized knowledge bases with typed JSON input", "list", "get", "create", "update", "delete"),
			catalogSpec("sources", "soha knowledge sources <list|create> --base-id ID [options]", "Read or register sources; source update/delete are unsupported by Core", "list", "create"),
			catalogSpec("documents", "soha knowledge documents list --base-id ID [--limit N] [options]", "Read bounded document metadata", "list"),
			catalogSpec("sync-runs", "soha knowledge sync-runs list --base-id ID [--limit N] [options]", "Read original synchronization receipts", "list"),
			catalogSpec("index-revisions", "soha knowledge index-revisions list --base-id ID [--limit N] [options]", "Read authoritative knowledge index revisions", "list"),
			{
				Name:     "search",
				Usage:    "soha knowledge search --base-ids <id[,id...]> --query <text> [options]",
				Summary:  "Search authorized knowledge bases and return grounded evidence",
				Examples: []string{"soha knowledge search --base-ids runbooks --query \"deployment rollback\"", "soha knowledge search --base-ids runbooks,handbook --query \"incident owner\" --output json"},
			},
			{Name: "connectors", Usage: "soha knowledge connectors <list|create|validate> [options]", Summary: "List, create, or validate external knowledge connectors", Examples: []string{"soha knowledge connectors list", "soha knowledge connectors create --base-id runbooks --name docs --kind http --config-ref soha://secrets/source-id --config-json '{\"url\":\"https://docs.example.com/\",\"allowedHosts\":[\"docs.example.com\"],\"maxBytes\":8388608}'", "soha knowledge connectors validate connector-1"}, CompletionWords: []string{"list", "create", "validate"}},
			{Name: "sync", Usage: "soha knowledge sync <start|status|cancel|retry> [options]", Summary: "Start or control bounded knowledge synchronization jobs", Examples: []string{"soha knowledge sync start --base-id runbooks --source-id source-1", "soha knowledge sync status sync-1"}, CompletionWords: []string{"start", "status", "cancel", "retry"}},
			{Name: "rebuild", Usage: "soha knowledge rebuild --base-id <id> [options]", Summary: "Start a bounded knowledge index rebuild", Examples: []string{"soha knowledge rebuild --base-id runbooks --reason \"embedding model update\""}},
		},
		Handler: runKnowledge,
	},
	{
		Name:    "ai",
		Usage:   "soha ai <chat|assistant|runs|task|inspection|evaluation|memory> [options]",
		Summary: "Operate chat, personal assistants, durable runs and governed AI workflows",
		Subcommands: []commandSpec{
			{Name: "chat", Usage: "soha ai chat <sessions|messages|request|native-request> [options]", Summary: "Read ordinary chat without implicitly enabling a personal assistant", Subcommands: []commandSpec{
				{Name: "sessions", Usage: "soha ai chat sessions <list|main|create|get> [SESSION] [--input FILE|-] [--yes] [options]", Summary: "List owned chats, explicitly ensure a main chat, create or read a side chat", CompletionWords: []string{"list", "main", "create", "get"}, Examples: []string{"soha ai chat sessions list --profile local", "soha ai chat sessions main --yes --profile local", "soha ai chat sessions create --input chat.json --yes --profile local"}},
				{Name: "messages", Usage: "soha ai chat messages <list|window|stream> SESSION [options]", Summary: "Read owned messages or explicitly submit a turn as bounded NDJSON; accepted is not completed", CompletionWords: []string{"list", "window", "stream"}, Examples: []string{"soha ai chat messages window session-1 --limit 40 --profile local", "soha ai chat messages stream session-1 --input turn.json --request-id original-turn-1 --yes --profile local"}},
				{Name: "request", Usage: "soha ai chat request SESSION --request-id ID [options]", Summary: "Find an original submission without resubmitting or resuming it", Examples: []string{"soha ai chat request session-1 --request-id original-turn-1 --profile local"}},
				{Name: "native-request", Usage: "soha ai chat native-request SESSION --request-id ID [options]", Summary: "Compatibility alias for chat request", Examples: []string{"soha ai chat native-request session-1 --request-id original-turn-1 --profile local"}},
			}},
			{Name: "assistant", Usage: "soha ai assistant <access|ensure|sessions|tasks|artifacts|environment|runtime|feed> [options]", Summary: "Inspect personal assistant access and explicitly operate owned assistant objects", Subcommands: []commandSpec{
				catalogSpec("environment", "soha ai assistant environment <get|templates|change> [options]", "Read or explicitly change the owned assistant environment", "get", "templates", "change"),
				catalogSpec("runtime", "soha ai assistant runtime <get|operation|execute> [OPERATION] [options]", "Read workspace runtime or submit a version-bound idempotent operation", "get", "operation", "execute"),
				catalogSpec("feed", "soha ai assistant feed <list|get|feedback|settings|refresh> [ITEM] [options]", "Read or explicitly update the owned assistant feed", "list", "get", "feedback", "settings", "refresh"),
				{Name: "access", Usage: "soha ai assistant access <get|request> [--yes] [options]", Summary: "Read access or request enablement; a request does not approve access or provision compute", CompletionWords: []string{"get", "request"}, Examples: []string{"soha ai assistant access get --profile local", "soha ai assistant access request --yes --profile local"}},
				{Name: "ensure", Usage: "soha ai assistant ensure [--yes] [options]", Summary: "Explicitly ensure the assistant identity after server approval", Examples: []string{"soha ai assistant ensure --yes --profile local"}},
				{Name: "sessions", Usage: "soha ai assistant sessions <list|create> [--input FILE|-] [--yes] [options]", Summary: "List or create owned personal sessions under server-side access checks", CompletionWords: []string{"list", "create"}},
				{Name: "tasks", Usage: "soha ai assistant tasks list [options]", Summary: "Read up to 50 recent tasks owned by the current user", CompletionWords: []string{"list"}},
				{Name: "artifacts", Usage: "soha ai assistant artifacts list [options]", Summary: "Read up to 50 recent saved artifacts owned by the current user", CompletionWords: []string{"list"}},
			}},
			{Name: "runs", Usage: "soha ai runs <list|events|cancel|resume> [RUN] [options]", Summary: "Inspect runs and request cancellation or resumption; an accepted cancel is not a terminal result", CompletionWords: []string{"list", "events", "cancel", "resume"}, Examples: []string{"soha ai runs list --session-id session-1 --profile local", "soha ai runs events run-1 --after-sequence 0 --profile local", "soha ai runs events run-1 --follow --session-id session-1 --wait-timeout 10m --profile local", "soha ai runs cancel run-1 --yes --profile local", "soha ai runs resume run-1 --input resume.json --yes --profile local"}},
			{Name: "inspection", Usage: "soha ai inspection <list|get|create|update|delete|run|runs> [options]", Summary: "Register governed inspections and follow durable trigger receipts", CompletionWords: []string{"list", "get", "create", "update", "delete", "run", "runs"}, Examples: []string{"soha ai inspection create --input inspection.json --yes --profile local", "soha ai inspection run inspect-1 --idempotency-key run-1 --expected-revision 2 --yes --profile local"}},
			{Name: "task", Usage: "soha ai task <validate|create|list|get|wait|cancel|resume> [options]", Summary: "Validate a capability plan and submit, observe, or stop a durable goal", CompletionWords: []string{"validate", "create", "list", "get", "wait", "cancel", "resume"}, Examples: []string{"soha ai task validate --input goal.json --profile local", "soha ai task create --input goal.json --yes --profile local", "soha ai task wait task-1 --profile local"}},
			{Name: "evaluation", Usage: "soha ai evaluation <datasets|runs|run|replay|gate> [options]", Summary: "Execute evaluation runs, isolated replay, or release gates", Examples: []string{"soha ai evaluation run --executor-profile-id executor-1 eval-run-1", "soha ai evaluation replay --id replay-1 --baseline-run-id baseline-1 --candidate-run-id candidate-1 --executor-profile-id executor-1", "soha ai evaluation gate --policy-id policy-1 --baseline-run-id baseline-1 --candidate-run-id candidate-1"}, CompletionWords: []string{"datasets", "runs", "run", "replay", "gate"}, Subcommands: []commandSpec{catalogSpec("datasets", "soha ai evaluation datasets <list|create> [options]", "Read or register evaluation datasets", "list", "create"), catalogSpec("runs", "soha ai evaluation runs <list|get|create|results|attempts|complete> [RUN] [options]", "Read, register and complete original evaluation runs; executor gate remains server-side", "list", "get", "create", "results", "attempts", "complete")}},
			{Name: "memory", Usage: "soha ai memory <inspect|delete> [options]", Summary: "Inspect or delete governed memory records", Examples: []string{"soha ai memory inspect --owner-type user --owner-id user-1", "soha ai memory delete memory-1"}, CompletionWords: []string{"inspect", "delete"}},
		},
		Handler: runAI,
	},
	{
		Name:    "prompt",
		Usage:   "soha prompt get <name> [options]",
		Summary: "Get an AI Gateway MCP prompt",
		Subcommands: []commandSpec{
			{
				Name:     "get",
				Usage:    "soha prompt get <name> [options]",
				Summary:  "Get an AI Gateway MCP prompt",
				Examples: []string{"soha prompt get soha.k8s.diagnose_workload --arguments-json '{}'"},
			},
		},
		Handler: runPrompt,
	},
	{
		Name:    "secret",
		Usage:   "soha secret <list|get|create|update|disable|versions|rotate|revoke-version|generate-ssh-key|references> [options]",
		Summary: "Manage write-only Secret Store values and immutable versions",
		Subcommands: []commandSpec{
			{Name: "list", Usage: "soha secret list [--scope-type type --scope-id id] [options]", Summary: "List authorized secret metadata", Examples: []string{"soha secret list --scope-type project --scope-id demo"}},
			{Name: "get", Usage: "soha secret get <secret-id> [options]", Summary: "Get secret metadata without revealing its value", Examples: []string{"soha secret get registry-token"}},
			{Name: "create", Usage: "soha secret create --name <name> [--binding type=target] [options]", Summary: "Create a typed local or Vault KV v2 secret; import SSH keys with --input file|-", Examples: []string{"printf '%s\\n' \"$REGISTRY_TOKEN\" | soha secret create --name registry-token --scope-type project --scope-id demo --binding capability=docker.projects.deploy.trigger", "soha secret create --name registry-token --vault-mount secret --vault-path demo/app --vault-key token --vault-version 3"}},
			{Name: "references", Usage: "soha secret references <id> [--profile name] [--output json|yaml]", Summary: "List visible VM bindings and initialization operations using fixed credential versions"},
			{Name: "generate-ssh-key", Usage: "soha secret generate-ssh-key --name <name> --binding <type:purpose=target> [options]", Summary: "Generate an encrypted Ed25519 credential and return only public metadata", Examples: []string{"soha secret generate-ssh-key --name vm-key --binding vm_create:vm_cloud_init=connection-1 --profile local"}},
			{Name: "update", Usage: "soha secret update <secret-id> [options]", Summary: "Update secret metadata and bindings", Examples: []string{"soha secret update registry-token --binding capability=docker.projects.deploy.trigger"}},
			{Name: "disable", Usage: "soha secret disable <secret-id> [options]", Summary: "Disable a secret", Examples: []string{"soha secret disable registry-token"}},
			{Name: "versions", Usage: "soha secret versions <secret-id> [options]", Summary: "List immutable secret versions", Examples: []string{"soha secret versions registry-token"}},
			{Name: "rotate", Usage: "soha secret rotate <secret-id> [options]", Summary: "Create a typed local or Vault KV v2 secret; import SSH keys with --input file|- version", Examples: []string{"printf '%s\\n' \"$REGISTRY_TOKEN\" | soha secret rotate registry-token", "soha secret rotate registry-token --vault-mount secret --vault-path demo/app --vault-key token --vault-version 4"}},
			{Name: "revoke-version", Usage: "soha secret revoke-version <secret-id> <version> [options]", Summary: "Revoke a secret version", Examples: []string{"soha secret revoke-version registry-token 1"}},
		},
		Handler: runSecret,
	},
	{
		Name:    "token",
		Usage:   "soha token <list|create|revoke|rotate|rotation-status> [options]",
		Summary: "Manage personal access tokens",
		Subcommands: []commandSpec{
			{Name: "list", Usage: "soha token list [options]", Summary: "List personal access tokens", Examples: []string{"soha token list"}},
			{Name: "create", Usage: "soha token create [options]", Summary: "Create a personal access token", Examples: []string{"soha token create --name local --permission-keys ai.gateway.view", "soha token create --name codex --governed-ai --ai-client-id codex --skill-id k8s-sre --profile local"}},
			{Name: "revoke", Usage: "soha token revoke <token-id> [options]", Summary: "Revoke a personal access token", Examples: []string{"soha token revoke pat_123"}},
			{Name: "rotate", Usage: "soha token rotate <token-id> [--auth-profile NAME] [--request-id UUID] [--expires-at RFC3339]", Summary: "Atomically replace a personal Key and revoke the old value", Examples: []string{"soha token rotate --id 11111111-1111-4111-8111-111111111111 --profile pat --auth-profile local"}},
			{Name: "rotation-status", Usage: "soha token rotation-status --request-id UUID [--auth-profile NAME]", Summary: "Recover a rotation outcome without retrieving its secret value", Examples: []string{"soha token rotation-status --request-id 22222222-2222-4222-8222-222222222222 --profile pat --auth-profile local"}},
		},
		Handler: runToken,
	},
	{
		Name:    "service-account",
		Usage:   "soha service-account <list|create|token-list|token-create|token-revoke|token-rotate|rotation-status> [options]",
		Summary: "Manage AI Gateway service accounts and tokens",
		Subcommands: []commandSpec{
			{Name: "list", Usage: "soha service-account list [options]", Summary: "List AI Gateway service accounts", Examples: []string{"soha service-account list"}},
			{Name: "create", Usage: "soha service-account create [options]", Summary: "Create a service account", Examples: []string{"soha service-account create --name runner --role-ids operator"}},
			{Name: "token-list", Usage: "soha service-account token-list [options]", Summary: "List service account tokens", Examples: []string{"soha service-account token-list"}},
			{Name: "token-create", Usage: "soha service-account token-create [options]", Summary: "Create a service account token", Examples: []string{"soha service-account token-create --service-account-id sa_123 --name ci", "soha service-account token-create --service-account-id sa_123 --name automation --governed-ai --ai-client-id ci-agent --skill-id delivery-developer --profile local"}},
			{Name: "token-revoke", Usage: "soha service-account token-revoke <token-id> [options]", Summary: "Revoke a service account token", Examples: []string{"soha service-account token-revoke sat_123"}},
			{Name: "token-rotate", Usage: "soha service-account token-rotate <token-id> [--auth-profile NAME] [--request-id UUID]", Summary: "Atomically replace a service account Key and revoke the old value", Examples: []string{"soha service-account token-rotate --id 11111111-1111-4111-8111-111111111111 --profile ci --auth-profile local"}},
			{Name: "rotation-status", Usage: "soha service-account rotation-status --request-id UUID [--auth-profile NAME]", Summary: "Query the durable Key rotation outcome", Examples: []string{"soha service-account rotation-status --request-id 22222222-2222-4222-8222-222222222222 --profile ci --auth-profile local"}},
		},
		Handler: runServiceAccount,
	},
	{
		Name:    "audit",
		Usage:   "soha audit list [options]",
		Summary: "Query AI Gateway audit logs",
		Subcommands: []commandSpec{
			{Name: "list", Usage: "soha audit list [options]", Summary: "Query AI Gateway audit logs", Examples: []string{"soha audit list --tool-name k8s.pods.logs --limit 20"}},
		},
		Handler: runAudit,
	},
	{
		Name:    "approval",
		Usage:   "soha approval <list|timeline|approve|reject|cancel> [options]",
		Summary: "List, trace, or decide AI Gateway approval requests",
		Subcommands: []commandSpec{
			{Name: "list", Usage: "soha approval list [options]", Summary: "List approval requests", Examples: []string{"soha approval list --status pending"}},
			{Name: "timeline", Usage: "soha approval timeline <approval-id> [options]", Summary: "Show an approval timeline", Examples: []string{"soha approval timeline approval_123"}},
			{Name: "approve", Usage: "soha approval approve <approval-id> [options]", Summary: "Approve an approval request", Examples: []string{"soha approval approve approval_123 --comment \"ship\""}},
			{Name: "reject", Usage: "soha approval reject <approval-id> [options]", Summary: "Reject an approval request", Examples: []string{"soha approval reject approval_123 --comment \"needs review\""}},
			{Name: "cancel", Usage: "soha approval cancel <approval-id> [options]", Summary: "Cancel an approval request", Examples: []string{"soha approval cancel approval_123"}},
		},
		Handler: runApproval,
	},
	{
		Name:    "governance",
		Usage:   "soha governance status [options]",
		Summary: "Show AI Gateway governance health and metrics",
		Subcommands: []commandSpec{
			{Name: "status", Usage: "soha governance status [options]", Summary: "Show AI Gateway governance health and metrics", Examples: []string{"soha governance status --window-hours 48", "soha governance status --json"}},
		},
		Handler: runGovernance,
	},
	{
		Name:    "cloud",
		Usage:   "soha cloud fleet diagnostics [options]",
		Summary: "Show Cloud managed agent fleet capability diagnostics",
		Subcommands: []commandSpec{
			{
				Name:    "fleet",
				Usage:   "soha cloud fleet diagnostics [options]",
				Summary: "Manage Cloud agent fleet diagnostics",
				Subcommands: []commandSpec{
					{
						Name:     "diagnostics",
						Usage:    "soha cloud fleet diagnostics --tenant-id <tenant-id> --fleet-id <fleet-id> [--output summary|json|yaml]",
						Summary:  "Show Cloud managed agent fleet capability diagnostics",
						Examples: []string{"soha cloud fleet diagnostics --tenant-id tenant_123 --fleet-id agent-fleet_123", "soha cloud fleet diagnostics --tenant-id tenant_123 --fleet-id agent-fleet_123 --output json"},
					},
				},
			},
		},
		Handler: runCloud,
	},
	{
		Name:    "profile",
		Usage:   "soha profile <list|show|use|remove> [options]",
		Summary: "List, show, switch, or remove local profiles",
		Subcommands: []commandSpec{
			{Name: "list", Usage: "soha profile list", Summary: "List local profiles", Examples: []string{"soha profile list"}},
			{Name: "show", Usage: "soha profile show [profile]", Summary: "Show one local profile with tokens redacted", Examples: []string{"soha profile show default"}},
			{Name: "use", Usage: "soha profile use <profile>", Summary: "Switch the current profile", Examples: []string{"soha profile use cloud"}},
			{Name: "remove", Usage: "soha profile remove <profile>", Summary: "Remove one local profile without revoking its server credential"},
		},
		Handler: runProfile,
	},
	{
		Name:    "context",
		Usage:   "soha context <show|set> [options]",
		Summary: "Show or update AI client context headers and marketplace defaults",
		Subcommands: []commandSpec{
			{Name: "show", Usage: "soha context show [options]", Summary: "Show AI client context headers and marketplace defaults", Examples: []string{"soha context show"}},
			{Name: "set", Usage: "soha context set [options]", Summary: "Update AI client context headers and marketplace defaults", Examples: []string{"soha context set --skill-id k8s-sre --source codex", "soha context set --marketplace https://marketplace.opensoha.com --marketplace-source-id opensoha-official"}},
		},
		Handler: runContext,
	},
	{
		Name:    "mcp",
		Usage:   "soha mcp [options] | soha mcp <start|install> [options]",
		Summary: "Run the Soha MCP stdio server or print client configuration",
		Subcommands: []commandSpec{
			{Name: "start", Usage: "soha mcp start [options]", Summary: "Compatibility form of soha mcp", Examples: []string{"soha mcp start --profile default"}},
			{Name: "install", Usage: "soha mcp install [options]", Summary: "Print MCP client configuration", Examples: []string{"soha mcp install --command /usr/local/bin/soha", "soha mcp install --base-url https://soha.example.com"}},
		},
		Examples: []string{"soha mcp", "soha mcp --skill-id k8s-sre", "soha mcp --base-url https://soha.example.com"},
		Handler:  runMCP,
	},
	{
		Name:    "skill",
		Usage:   "soha skill <list|install|status|update|remove|rollback> [options]",
		Summary: "Install and manage verified Soha AI Gateway skills",
		Subcommands: []commandSpec{
			{Name: "list", Usage: "soha skill list [options]", Summary: "List skills from a local or verified release source", Examples: []string{"soha skill list"}},
			{Name: "install", Usage: "soha skill install [options] [skill-id...]", Summary: "Install skills from a local or verified release source", Examples: []string{"soha skill install --dest ~/.soha/skills k8s-sre"}},
			{Name: "status", Usage: "soha skill status [options]", Summary: "Show the active and previous managed skills generations", Examples: []string{"soha skill status", "soha skill status --scope project --json"}},
			{Name: "update", Usage: "soha skill update [options] [skill-id...]", Summary: "Update managed skills from the latest stable release", Examples: []string{"soha skill update", "soha skill update --all"}},
			{Name: "remove", Usage: "soha skill remove [options] <skill-id...>", Summary: "Remove managed skills with a rollback generation", Examples: []string{"soha skill remove k8s-sre"}},
			{Name: "rollback", Usage: "soha skill rollback [options]", Summary: "Switch back to the previous verified skills generation", Examples: []string{"soha skill rollback"}},
		},
		Handler: runSkill,
	},
	{
		Name:            "setup",
		Usage:           "soha setup [target] [--client target] [--mode mcp|skill|both] [--scope user|project] [options]",
		Summary:         "Configure Soha MCP and skills for an AI agent or IDE",
		Examples:        []string{"soha setup --client codex", "soha setup --client codex --scope project", "soha setup --client codex --mode both --skills k8s-sre --skill-id k8s-sre", "soha setup --client claude --mode mcp --base-url https://soha.example.com", "soha setup --client codex --check"},
		CompletionWords: []string{"codex", "claude", "cursor", "kiro", "gemini", "antigravity", "antigravity-ide", "trae", "all"},
		Handler:         runSetup,
	},
	{
		Name:            "add",
		Usage:           "soha add [target] [options]",
		Summary:         "Compatibility alias for configuring Soha MCP and skills",
		Examples:        []string{"soha add --profile local --server http://localhost:8080", "soha add claude --profile default --server https://mcp.opensoha.com --command /usr/local/bin/soha"},
		CompletionWords: []string{"codex", "claude", "cursor", "kiro", "gemini", "antigravity", "antigravity-ide", "trae", "all"},
		Handler:         runAdd,
	},
	{
		Name:    "plugin",
		Usage:   "soha plugin <search|show|install|list|enable|disable|upgrade|config|remove> [options]",
		Summary: "Search, install, and manage Soha plugins",
		Subcommands: []commandSpec{
			{Name: "search", Usage: "soha plugin search [options]", Summary: "Search plugin marketplace entries", Examples: []string{"soha plugin search --query k8s", "soha plugin search --marketplace https://marketplace.opensoha.com --source-id opensoha-official"}},
			{Name: "show", Usage: "soha plugin show [options] <plugin-id>", Summary: "Show marketplace, installed, or manifest details", Examples: []string{"soha plugin show --installed opensoha.k8s-sre-pack", "soha plugin show --marketplace https://marketplace.opensoha.com --version 0.1.0 opensoha.k8s-sre-pack"}},
			{Name: "install", Usage: "soha plugin install [options] [plugin-id]", Summary: "Install a marketplace or manifest plugin", Examples: []string{"soha plugin install --enable opensoha.k8s-sre-pack", "soha plugin install --marketplace https://marketplace.opensoha.com --version 0.1.0 opensoha.k8s-sre-pack"}},
			{Name: "list", Usage: "soha plugin list [options]", Summary: "List installed plugins", Examples: []string{"soha plugin list"}},
			{Name: "enable", Usage: "soha plugin enable <plugin-id> [options]", Summary: "Enable an installed plugin", Examples: []string{"soha plugin enable opensoha.k8s-sre-pack"}},
			{Name: "disable", Usage: "soha plugin disable <plugin-id> [options]", Summary: "Disable an installed plugin", Examples: []string{"soha plugin disable opensoha.k8s-sre-pack"}},
			{Name: "upgrade", Usage: "soha plugin upgrade [options] <plugin-id>", Summary: "Upgrade an installed plugin", Examples: []string{"soha plugin upgrade opensoha.k8s-sre-pack", "soha plugin upgrade --marketplace https://marketplace.opensoha.com --version 0.2.0 opensoha.k8s-sre-pack"}},
			{Name: "config", Usage: "soha plugin config <plugin-id> [options]", Summary: "Configure metadata and secret refs", Examples: []string{"soha plugin config opensoha.k8s-sre-pack --secret-ref token=secret://k8s/token"}},
			{Name: "remove", Aliases: []string{"rm"}, Usage: "soha plugin remove <plugin-id> [options]", Summary: "Remove an installed plugin", Examples: []string{"soha plugin remove opensoha.k8s-sre-pack"}},
		},
		Handler: runPlugin,
	},
	{
		Name:     "diagnose",
		Usage:    "soha diagnose [--client <name>] [--output text|json] [options]",
		Summary:  "Check profile, client setup, and Gateway connectivity",
		Examples: []string{"soha diagnose --client codex --output json", "soha diagnose --tool k8s.pods.logs --resource soha://k8s/runtime"},
		Handler:  runDiagnose,
	},
	{
		Name:            "completion",
		Usage:           "soha completion [bash|zsh|fish|powershell]",
		Summary:         "Print shell completion script",
		Examples:        []string{"soha completion bash", "soha completion zsh", "soha completion fish", "soha completion powershell"},
		CompletionWords: []string{"bash", "zsh", "fish", "powershell"},
	},
	{
		Name:     "docs",
		Usage:    "soha docs [--format markdown]",
		Summary:  "Generate CLI command reference documentation",
		Examples: []string{"soha docs --format markdown"},
	},
}

func findTopLevelCommandSpec(name string) (*commandSpec, bool) {
	for i := range topLevelCommandSpecs {
		if topLevelCommandSpecs[i].matches(name) {
			return &topLevelCommandSpecs[i], true
		}
	}
	return nil, false
}

func findCommandSpec(path []string) (*commandSpec, bool) {
	if len(path) == 0 {
		return nil, false
	}
	spec, ok := findTopLevelCommandSpec(path[0])
	if !ok {
		return nil, false
	}
	for _, name := range path[1:] {
		if isHelpArg(name) || strings.HasPrefix(name, "-") {
			break
		}
		var next *commandSpec
		for i := range spec.Subcommands {
			if spec.Subcommands[i].matches(name) {
				next = &spec.Subcommands[i]
				break
			}
		}
		if next == nil {
			return nil, false
		}
		spec = next
	}
	return spec, true
}

func dispatchTopLevelCommand(ctx context.Context, name string, args []string, rt Runtime) error {
	spec, ok := findTopLevelCommandSpec(name)
	if !ok {
		return usageError{message: fmt.Sprintf("unknown command %q", name)}
	}
	if spec.Handler == nil {
		switch spec.Name {
		case "completion":
			return runCompletion(args, rt)
		case "docs":
			return runDocs(args, rt)
		default:
			return fmt.Errorf("unknown command %q", name)
		}
	}
	return spec.Handler(ctx, args, rt)
}

func topLevelCommandNames(includeHelp bool) []string {
	names := make([]string, 0, len(topLevelCommandSpecs)+1)
	for _, spec := range topLevelCommandSpecs {
		names = append(names, spec.Name)
		names = append(names, spec.Aliases...)
	}
	if includeHelp {
		names = append(names, "help")
	}
	return names
}

func (s commandSpec) matches(name string) bool {
	if s.Name == name {
		return true
	}
	for _, alias := range s.Aliases {
		if alias == name {
			return true
		}
	}
	return false
}

func (s commandSpec) completionWords() []string {
	if len(s.CompletionWords) > 0 {
		return append([]string(nil), s.CompletionWords...)
	}
	words := make([]string, 0, len(s.Subcommands))
	for _, subcommand := range s.Subcommands {
		words = append(words, subcommand.Name)
		words = append(words, subcommand.Aliases...)
	}
	return words
}

func joinWords(words []string) string {
	return strings.Join(words, " ")
}
