package dto

type CreateClusterRequest struct {
	ID                     string            `json:"id"`
	Name                   string            `json:"name"`
	Region                 string            `json:"region"`
	Environment            string            `json:"environment"`
	Labels                 map[string]string `json:"labels"`
	ConnectionMode         string            `json:"connectionMode"`
	Kubeconfig             string            `json:"kubeconfig"`
	Context                string            `json:"context"`
	AgentEndpoint          string            `json:"agentEndpoint"`
	AgentToken             string            `json:"agentToken"`
	PrometheusBaseURL      string            `json:"prometheusBaseUrl"`
	PrometheusBearerToken  string            `json:"prometheusBearerToken"`
	PrometheusClusterLabel string            `json:"prometheusClusterLabel"`
	GrafanaBaseURL         string            `json:"grafanaBaseUrl"`
}

type UpdateClusterRequest struct {
	Name                   string            `json:"name"`
	Region                 string            `json:"region"`
	Environment            string            `json:"environment"`
	Labels                 map[string]string `json:"labels"`
	ConnectionMode         string            `json:"connectionMode"`
	Kubeconfig             string            `json:"kubeconfig"`
	Context                string            `json:"context"`
	AgentEndpoint          string            `json:"agentEndpoint"`
	AgentToken             string            `json:"agentToken"`
	PrometheusBaseURL      string            `json:"prometheusBaseUrl"`
	PrometheusBearerToken  string            `json:"prometheusBearerToken"`
	PrometheusClusterLabel string            `json:"prometheusClusterLabel"`
	GrafanaBaseURL         string            `json:"grafanaBaseUrl"`
}

type NamespaceUpsertRequest struct {
	Name        string            `json:"name"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

type NodeTaintRequest struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Effect string `json:"effect"`
}

type NodeUpdateRequest struct {
	Labels map[string]string  `json:"labels"`
	Taints []NodeTaintRequest `json:"taints"`
}

type RestartDeploymentRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type ScaleDeploymentRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Replicas  int32  `json:"replicas"`
}

type RestartStatefulSetRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type ScaleStatefulSetRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Replicas  int32  `json:"replicas"`
}

type RestartDaemonSetRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type ExecPodRequest struct {
	Command        string `json:"command"`
	Container      string `json:"container"`
	TimeoutSeconds int64  `json:"timeoutSeconds"`
}

type RollbackDeploymentRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Revision  string `json:"revision"`
}
