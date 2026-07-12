package aigateway

import "slices"

type gatewaySecretClassifierDefinition struct {
	name    string
	pattern string
	aliases []string
}

var gatewaySpecificSecretClassifierDefinitions = []gatewaySecretClassifierDefinition{
	{name: "github", pattern: `(?i)(?:ghp|github_pat)_[A-Za-z0-9_]{20,}`, aliases: []string{"github", "githubtoken"}},
	{name: "gitlab", pattern: `(?i)glpat-[A-Za-z0-9_-]{20,}`, aliases: []string{"gitlab", "gitlabtoken"}},
	{name: "openai", pattern: `(?i)sk-[A-Za-z0-9_-]{20,}`, aliases: []string{"openai", "openaikey"}},
	{name: "anthropic", pattern: `(?i)sk-ant-[A-Za-z0-9_-]{20,}`, aliases: []string{"anthropic", "anthropickey"}},
	{name: "slack", pattern: `(?i)xox[baprs]-[A-Za-z0-9-]{20,}`, aliases: []string{"slack", "slacktoken"}},
	{name: "google_api_key", pattern: `AIza[0-9A-Za-z_-]{30,}`, aliases: []string{"google", "googleapikey", "gcpapikey"}},
	{name: "huggingface", pattern: `(?i)hf_[A-Za-z0-9]{30,}`, aliases: []string{"huggingface", "huggingfacetoken"}},
	{name: "cohere", pattern: `(?i)\bcohere[_-]?(?:api[_-]?)?key[_-]?[A-Za-z0-9]{20,}\b`, aliases: []string{"cohere", "coherekey", "coheretoken"}},
	{name: "mistral", pattern: `(?i)\bmistral[_-]?[A-Za-z0-9]{20,}\b`, aliases: []string{"mistral", "mistralkey", "mistraltoken"}},
	{name: "deepseek", pattern: `(?i)\bsk-(?:deepseek|ds)-[A-Za-z0-9_-]{20,}\b`, aliases: []string{"deepseek", "deepseekkey", "deepseektoken"}},
	{name: "groq", pattern: `(?i)\bgsk_[A-Za-z0-9]{20,}\b`, aliases: []string{"groq", "groqkey", "groqtoken"}},
	{name: "together", pattern: `(?i)\btgp_v1_[A-Za-z0-9_-]{20,}\b`, aliases: []string{"together", "togetherkey", "togethertoken", "togetherai"}},
	{name: "replicate", pattern: `(?i)\br8_[A-Za-z0-9]{20,}\b`, aliases: []string{"replicate", "replicatekey", "replicatetoken"}},
	{name: "langsmith", pattern: `(?i)\bls[v]2?_[A-Za-z0-9_-]{20,}\b`, aliases: []string{"langsmith", "langchain", "langsmithkey", "langsmithtoken"}},
	{name: "pinecone", pattern: `(?i)\bpcsk_[A-Za-z0-9_-]{20,}\b`, aliases: []string{"pinecone", "pineconekey", "pineconetoken"}},
	{name: "xai", pattern: `(?i)\bxai-[A-Za-z0-9_-]{20,}\b`, aliases: []string{"xai", "xaikey", "xaitoken", "grok", "grokkey", "groktoken"}},
	{name: "perplexity", pattern: `(?i)\bpplx-[A-Za-z0-9_-]{20,}\b`, aliases: []string{"perplexity", "perplexitykey", "perplexitytoken", "pplx"}},
	{name: "tavily", pattern: `(?i)\btvly-[A-Za-z0-9_-]{20,}\b`, aliases: []string{"tavily", "tavilykey", "tavilytoken"}},
	{name: "langfuse", pattern: `(?i)\b(?:pk|sk)-lf-[A-Za-z0-9_-]{20,}\b`, aliases: []string{"langfuse", "langfusekey", "langfusetoken"}},
	{name: "qdrant", pattern: `(?i)\bqdrant[_-][A-Za-z0-9_-]{20,}\b`, aliases: []string{"qdrant", "qdrantkey", "qdranttoken"}},
	{name: "wandb", pattern: `(?i)\bwandb_[A-Za-z0-9]{20,}\b`, aliases: []string{"wandb", "weightsandbiases", "wandbkey", "wandbtoken"}},
	{name: "linear", pattern: `(?i)\blin_api_[A-Za-z0-9]{20,}\b`, aliases: []string{"linear", "linearkey", "lineartoken"}},
	{name: "openrouter", pattern: `(?i)\bsk-or-v1-[A-Za-z0-9_-]{20,}\b`, aliases: []string{"openrouter", "openrouterkey", "openroutertoken"}},
	{name: "fireworks", pattern: `(?i)\bfw_[A-Za-z0-9_-]{20,}\b`, aliases: []string{"fireworks", "fireworksai", "fireworkskey", "fireworkstoken"}},
	{name: "voyage", pattern: `(?i)\bpa-[A-Za-z0-9_-]{20,}\b`, aliases: []string{"voyage", "voyageai", "voyagekey", "voyagetoken"}},
	{name: "brave_search", pattern: `(?i)\bBSA[A-Za-z0-9_-]{20,}\b`, aliases: []string{"bravesearch", "brave", "bravesearchkey", "bravesearchtoken"}},
	{name: "serpapi", pattern: `(?i)\bserpapi[_-]?[A-Za-z0-9]{20,}\b`, aliases: []string{"serpapi", "serp", "serpapikey", "serpapitoken"}},
	{name: "browserbase", pattern: `(?i)\bbb_[A-Za-z0-9_-]{20,}\b`, aliases: []string{"browserbase", "browserbasekey", "browserbasetoken"}},
	{name: "exa", pattern: `(?i)\bexa_[A-Za-z0-9_-]{20,}\b`, aliases: []string{"exa", "exasearch", "exakey", "exatoken"}},
	{name: "jina", pattern: `(?i)\bjina_[A-Za-z0-9_-]{20,}\b`, aliases: []string{"jina", "jinaai", "jinakey", "jinatoken"}},
	{name: "unstructured", pattern: `(?i)\bunstructured[_-]?[A-Za-z0-9_-]{20,}\b`, aliases: []string{"unstructured", "unstructuredio", "unstructuredkey", "unstructuredtoken"}},
	{name: "llama_cloud", pattern: `(?i)\bllx-[A-Za-z0-9_-]{20,}\b`, aliases: []string{"llamacloud", "llamaindex", "llamaparse", "llamacloudkey", "llamacloudtoken"}},
	{name: "helicone", pattern: `(?i)\bsk-helicone-[A-Za-z0-9_-]{20,}\b`, aliases: []string{"helicone", "heliconekey", "heliconetoken"}},
	{name: "dashscope", pattern: `(?i)\bsk-[A-Za-z0-9]{24,}\b`, aliases: []string{"dashscope", "dashscopekey", "dashscopetoken", "aliyunbailian", "bailian", "tongyi", "qwen"}},
	{name: "moonshot", pattern: `(?i)\bsk-[A-Za-z0-9]{32,}\b`, aliases: []string{"moonshot", "moonshotkey", "moonshottoken", "kimi"}},
	{name: "zhipu", pattern: `(?i)\b[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]{24,}\b`, aliases: []string{"zhipu", "zhipuai", "zhipukey", "zhiputoken", "glm"}},
	{name: "siliconflow", pattern: `(?i)\bsk-[A-Za-z0-9]{32,}\b`, aliases: []string{"siliconflow", "siliconflowkey", "siliconflowtoken"}},
	{name: "hunyuan", pattern: `(?i)\bAKID[A-Za-z0-9]{16,}\b`, aliases: []string{"hunyuan", "tencenthunyuan", "hunyuansecretid", "tencentcloud"}},
	{name: "qianfan", pattern: `(?i)\bbce-v3/[A-Za-z0-9._~+/=-]{24,}\b`, aliases: []string{"qianfan", "baiduqianfan", "wenxin", "ernie", "baiducloud"}},
	{name: "volcengine", pattern: `(?i)\b(?:aklt|volc)[A-Za-z0-9_-]{20,}\b`, aliases: []string{"volcengine", "volcano", "doubao", "ark", "volcengineark", "volctoken"}},
	{name: "grafana", pattern: `(?i)\bgl(?:sa|c)_[A-Za-z0-9_=-]{20,}\b`, aliases: []string{"grafana", "grafanakey", "grafanatoken", "grafanaserviceaccount"}},
	{name: "sentry", pattern: `(?i)\bsntrys_[A-Za-z0-9_=-]{20,}\b`, aliases: []string{"sentry", "sentrykey", "sentrytoken", "sentryauthtoken"}},
	{name: "newrelic", pattern: `(?i)\bNRAK[-_A-Za-z0-9]{20,}\b`, aliases: []string{"newrelic", "newrelickey", "newrelictoken", "newrelicuserkey"}},
	{name: "azure_openai", pattern: `(?i)\b(?:azure[_-]?(?:openai|ai)?[_-]?(?:api[_-]?)?key|AZURE_OPENAI_API_KEY|OCP_APIM_SUBSCRIPTION_KEY)[\s:=_-]+[A-Za-z0-9]{32,}\b`, aliases: []string{"azure", "azureopenai", "azureai", "azurekey", "azuretoken", "azureopenaikey", "azureopenaitoken"}},
	{name: "azure_devops", pattern: `(?i)\b[A-Za-z0-9]{76}AZDO[A-Za-z0-9]{4}\b`, aliases: []string{"azuredevops", "azuredevopspat", "azdo", "azdopat"}},
	{name: "datadog", pattern: `(?i)\b(?:datadog|dd)[_-]?(?:api|app)?[_-]?key[_-]?[A-Fa-f0-9]{32,40}\b`, aliases: []string{"datadog", "datadogkey", "datadogtoken", "datadogapikey", "datadogappkey"}},
	{name: "pagerduty", pattern: `(?i)\bpd(?:us|at)\+[A-Za-z0-9._~-]{20,}\b`, aliases: []string{"pagerduty", "pagerdutykey", "pagerdutytoken", "pdtoken"}},
	{name: "posthog", pattern: `(?i)\bph[cp]_[A-Za-z0-9_-]{20,}\b`, aliases: []string{"posthog", "posthogkey", "posthogtoken"}},
	{name: "splunk", pattern: `(?i)\bSplunk\s+[A-Za-z0-9+/=_-]{20,}\b`, aliases: []string{"splunk", "splunktoken", "splunkhectoken"}},
	{name: "elastic", pattern: `(?i)\bApiKey\s+[A-Za-z0-9+/=_-]{20,}\b`, aliases: []string{"elastic", "elasticsearch", "elastickey", "elastictoken", "elasticsearchapikey"}},
	{name: "terraform", pattern: `(?i)\batlasv1\.[A-Za-z0-9_-]{20,}\b`, aliases: []string{"terraform", "terraformcloud", "terraformtoken", "tfc", "tfctoken"}},
	{name: "npm", pattern: `(?i)npm_[A-Za-z0-9]{36,}`, aliases: []string{"npm", "npmtoken"}},
	{name: "stripe", pattern: `(?i)(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{16,}`, aliases: []string{"stripe", "stripetoken"}},
	{name: "jwt", pattern: `eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`, aliases: []string{"jwt"}},
	{name: "aws", pattern: `AKIA[0-9A-Z]{16}`, aliases: []string{"aws", "awsaccesskey"}},
	{name: "private_key", pattern: `-----BEGIN [A-Z ]*PRIVATE KEY-----`, aliases: []string{"privatekey", "pem"}},
	{name: "k8s_secret_yaml", pattern: `(?im)^\s*kind:\s*Secret\s*$`, aliases: []string{"kubernetes", "kubernetessecret", "k8ssecret"}},
	{name: "kubeconfig", pattern: `(?is)\bclusters\s*:.*\busers\s*:.*\bcurrent-context\s*:`, aliases: []string{"kubeconfig", "kubernetesconfig", "k8sconfig"}},
	{name: "docker_config", pattern: `(?is)"auths"\s*:\s*\{.*"auth"\s*:`, aliases: []string{"docker", "dockerconfig", "dockerauth"}},
}

var gatewayDefaultSecretClassifierNames = []string{
	"jwt", "aws", "private_key", "anthropic", "google_api_key", "huggingface", "cohere", "mistral", "deepseek", "groq",
	"together", "replicate", "langsmith", "pinecone", "xai", "perplexity", "tavily", "langfuse", "qdrant", "wandb",
	"linear", "openrouter", "fireworks", "voyage", "brave_search", "serpapi", "browserbase", "exa", "jina", "unstructured",
	"llama_cloud", "helicone", "dashscope", "moonshot", "zhipu", "siliconflow", "hunyuan", "qianfan", "volcengine",
	"grafana", "sentry", "newrelic", "azure_openai", "azure_devops", "datadog", "pagerduty", "posthog", "splunk",
	"elastic", "terraform", "npm", "stripe", "docker_config", "kubeconfig",
}

var gatewayDefaultSecretPatternOverrides = map[string]string{
	"dashscope":   `(?i)\b(?:dashscope|dash_scope|aliyun[_-]?bailian|bailian|tongyi|qwen)[\s:=_-]+sk-[A-Za-z0-9]{24,}\b`,
	"moonshot":    `(?i)\b(?:moonshot|kimi)[\s:=_-]+sk-[A-Za-z0-9]{32,}\b`,
	"zhipu":       `(?i)\b(?:zhipu|zhipuai|glm)[\s:=_-]+[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]{24,}\b`,
	"siliconflow": `(?i)\bsilicon[_-]?flow[\s:=_-]+sk-[A-Za-z0-9]{32,}\b`,
}

var gatewayDefaultSecretClassifierAliases = []string{"default", "all", "builtin", "builtins", "secret", "secrets", "token", "tokens"}

func gatewaySecretClassifierDefinitionsForType(secretType string) []gatewaySecretClassifierDefinition {
	if slices.Contains(gatewayDefaultSecretClassifierAliases, secretType) {
		return gatewayDefaultSecretDefinitions()
	}
	for _, definition := range gatewaySpecificSecretClassifierDefinitions {
		if slices.Contains(definition.aliases, secretType) {
			return []gatewaySecretClassifierDefinition{definition}
		}
	}
	return nil
}

func gatewayDefaultSecretDefinitions() []gatewaySecretClassifierDefinition {
	out := []gatewaySecretClassifierDefinition{{
		name:    "token",
		pattern: `(?i)(?:bearer\s+)?(?:ghp|github_pat|glpat|sk|xox[baprs])[-_A-Za-z0-9]{12,}`,
	}}
	for _, name := range gatewayDefaultSecretClassifierNames {
		for _, definition := range gatewaySpecificSecretClassifierDefinitions {
			if definition.name != name {
				continue
			}
			if pattern := gatewayDefaultSecretPatternOverrides[name]; pattern != "" {
				definition.pattern = pattern
			}
			out = append(out, definition)
			break
		}
	}
	return out
}
