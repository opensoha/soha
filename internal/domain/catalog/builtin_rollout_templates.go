package catalog

import domainmanifest "github.com/opensoha/soha/internal/domain/manifest"

func builtinRolloutDeploymentTemplate(canary bool) DeploymentTemplateSpec {
	minimum, maximum, portMax, countMin, countMax := float64(1), float64(100), float64(65535), float64(2), float64(1000)
	textMin := 1
	parameters := map[string]ParameterSchema{
		"replicas":               {Type: "integer", Minimum: &minimum, Maximum: &maximum},
		"port":                   {Type: "integer", Minimum: &minimum, Maximum: &portMax},
		"healthPath":             {Type: "string", MinLength: &textMin},
		"command":                {Type: "array", Items: &ParameterSchema{Type: "string"}},
		"args":                   {Type: "array", Items: &ParameterSchema{Type: "string"}},
		"env":                    {Type: "array", Items: &ParameterSchema{Type: "object", Properties: map[string]ParameterSchema{"name": {Type: "string"}, "value": {Type: "string"}}, Required: []string{"name", "value"}}},
		"metricURL":              {Type: "string", MinLength: &textMin, Description: "预览服务的只读 JSON 指标 URL，例如 http://app-preview.dev.svc:8080/metrics；须与本模板服务及命名空间一致"},
		"metricJSONPath":         {Type: "string", MinLength: &textMin, Description: "从指标响应读取数值的 JSONPath"},
		"metricSuccessCondition": {Type: "string", MinLength: &textMin, Description: "每次观测的通过条件，例如 result >= 0.99"},
		"metricInterval":         {Type: "string", MinLength: &textMin, Description: "观测间隔，例如 10s；完整窗口不得超过 24 小时"},
		"metricCount":            {Type: "integer", Minimum: &countMin, Maximum: &countMax, Description: "观测次数；全部通过后才可完成发布"},
	}
	defaults := map[string]any{"replicas": 2, "port": 8080, "healthPath": "/", "command": []any{}, "args": []any{}, "env": []any{}, "metricJSONPath": "{$.successRate}", "metricSuccessCondition": "result >= 0.99", "metricInterval": "10s", "metricCount": 6}
	key, name := "soha-bluegreen", "蓝绿 HTTP 服务"
	strategy := `    blueGreen:
      activeService: ${{ system.serviceKey }}
      previewService: ${{ system.previewServiceName }}
      autoPromotionEnabled: false
      prePromotionAnalysis:
        templates:
          - templateName: ${{ system.serviceKey }}
`
	traffic := ""
	required := []string{"metricURL"}
	if canary {
		key, name = "soha-canary", "金丝雀 HTTP 服务"
		weightMax := float64(99)
		parameters["initialWeight"] = ParameterSchema{Type: "integer", Minimum: &minimum, Maximum: &weightMax, Description: "首阶段预览版本的实际流量权重"}
		parameters["routeMatch"] = ParameterSchema{Type: "string", MinLength: &textMin, Description: "Traefik 路由规则，例如 Host(\"app.example.com\")；须使用已授权的业务域名"}
		defaults["initialWeight"] = 20
		required = append(required, "routeMatch")
		strategy = `    canary:
      stableService: ${{ system.serviceKey }}
      canaryService: ${{ system.previewServiceName }}
      trafficRouting:
        traefik:
          weightedTraefikServiceName: ${{ system.serviceKey }}
      steps:
        - setWeight: ${{ parameters.initialWeight }}
        - pause: {}
        - analysis:
            templates:
              - templateName: ${{ system.serviceKey }}
        - setWeight: 100
`
		traffic = builtinCanaryTrafficYAML
	}
	workload := builtinRolloutYAML + strategy
	for _, serviceName := range []string{"${{ system.serviceKey }}", "${{ system.previewServiceName }}"} {
		workload += `---
apiVersion: v1
kind: Service
metadata:
  name: ` + serviceName + `
  namespace: ${{ system.namespace }}
spec:
  selector:
    app.kubernetes.io/name: ${{ system.serviceKey }}
  ports:
    - name: http
      port: ${{ parameters.port }}
      targetPort: http
`
	}
	workload += builtinRolloutAnalysisYAML + traffic
	description := "目标命名空间须安装 Argo Rollouts；预览服务须提供只读 JSON 业务指标。更新先观测完整指标窗口，再人工提升；失败或取消须确认流量恢复到稳定版本。"
	if canary {
		description = "目标命名空间须安装 Argo Rollouts 与支持 CRD 的 Traefik，入口为 web。先按实际权重分流并人工提升，再观测完整指标窗口；预览服务须提供只读 JSON 业务指标。"
	}
	return DeploymentTemplateSpec{Key: key, Name: name, Description: description, Enabled: true,
		Source:          DeploymentTemplateSource{Renderer: "raw_yaml", Files: []domainmanifest.File{{Path: "rollout.yaml", Content: workload}}},
		ParameterSchema: ParameterSchema{Type: "object", Properties: parameters, Required: required}, Defaults: defaults,
		EnvironmentOverrides: []string{"env", "replicas", "metricInterval", "metricCount", "metricSuccessCondition"},
		Artifacts:            map[string]string{"main": "main"}, Health: DeploymentTemplateHealth{Mode: "workload_ready", TimeoutSeconds: 3600},
	}
}

const builtinCanaryTrafficYAML = `---
apiVersion: traefik.io/v1alpha1
kind: TraefikService
metadata:
  name: ${{ system.serviceKey }}
  namespace: ${{ system.namespace }}
spec:
  weighted:
    services:
      - name: ${{ system.serviceKey }}
        port: ${{ parameters.port }}
        weight: 100
      - name: ${{ system.previewServiceName }}
        port: ${{ parameters.port }}
        weight: 0
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: ${{ system.serviceKey }}
  namespace: ${{ system.namespace }}
spec:
  entryPoints: [web]
  routes:
    - kind: Rule
      match: ${{ parameters.routeMatch }}
      services:
        - name: ${{ system.serviceKey }}
          kind: TraefikService
`

const builtinRolloutYAML = `apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: ${{ system.serviceKey }}
  namespace: ${{ system.namespace }}
spec:
  replicas: ${{ parameters.replicas }}
  selector:
    matchLabels:
      app.kubernetes.io/name: ${{ system.serviceKey }}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: ${{ system.serviceKey }}
    spec:
      containers:
        - name: main
          image: ${{ artifacts.main }}
          command: ${{ parameters.command }}
          args: ${{ parameters.args }}
          env: ${{ parameters.env }}
          ports:
            - name: http
              containerPort: ${{ parameters.port }}
          readinessProbe:
            httpGet:
              path: ${{ parameters.healthPath }}
              port: http
  strategy:
`

const builtinRolloutAnalysisYAML = `---
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: ${{ system.serviceKey }}
  namespace: ${{ system.namespace }}
spec:
  metrics:
    - name: success
      interval: ${{ parameters.metricInterval }}
      count: ${{ parameters.metricCount }}
      successCondition: ${{ parameters.metricSuccessCondition }}
      failureLimit: 0
      provider:
        web:
          url: ${{ parameters.metricURL }}
          jsonPath: ${{ parameters.metricJSONPath }}
`
