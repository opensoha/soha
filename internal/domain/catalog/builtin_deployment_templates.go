package catalog

import (
	"fmt"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
)

// BuiltinDeploymentTemplates are ordinary versioned templates. Seeding never
// overwrites an administrator's saved draft, publication or deprecation.
func BuiltinDeploymentTemplates() []DeploymentTemplateSpec {
	items := make([]DeploymentTemplateSpec, 0, 6)
	for _, kind := range []string{"http", "worker", "static", "job", "http-periodic"} {
		minimum, maximum := float64(1), float64(100)
		envItem := ParameterSchema{Type: "object", Properties: map[string]ParameterSchema{"name": {Type: "string"}, "value": {Type: "string"}}, Required: []string{"name", "value"}}
		parameters := map[string]ParameterSchema{
			"env":     {Type: "array", Items: &envItem},
			"command": {Type: "array", Items: &ParameterSchema{Type: "string"}},
			"args":    {Type: "array", Items: &ParameterSchema{Type: "string"}},
		}
		defaults := map[string]any{"env": []any{}, "command": []any{}, "args": []any{}}
		overrides := []string{"env"}
		mode, title := "workload_ready", "后台 Worker"
		workload := builtinDeploymentYAML
		if kind == "job" {
			title, mode = "一次性 Job", "job_complete"
			workload = builtinJobYAML
		} else {
			parameters["replicas"] = ParameterSchema{Type: "integer", Minimum: &minimum, Maximum: &maximum}
			defaults["replicas"] = 1
			overrides = append(overrides, "replicas")
		}
		if kind == "http" || kind == "static" || kind == "http-periodic" {
			title, defaults["port"] = "HTTP 服务", 8080
			if kind == "static" {
				title, defaults["port"] = "静态站点", 80
			}
			portMax, pathMin := float64(65535), 1
			parameters["port"] = ParameterSchema{Type: "integer", Minimum: &minimum, Maximum: &portMax}
			parameters["healthPath"] = ParameterSchema{Type: "string", MinLength: &pathMin}
			defaults["healthPath"] = "/"
			workload += builtinHTTPServiceYAML
		}
		if kind == "http-periodic" {
			title = "HTTP 服务与跟随式定时任务"
			parameters["taskSchedule"] = ParameterSchema{Type: "string", Description: "定时任务的五段 Cron 表达式"}
			parameters["taskTimeZone"] = ParameterSchema{Type: "string", Description: "调度时区，例如 Asia/Shanghai"}
			parameters["taskSuspend"] = ParameterSchema{Type: "boolean", Description: "默认暂停；配置任务命令后显式关闭暂停。就绪表示配置已同步，不表示任务执行成功。"}
			parameters["taskCommand"] = ParameterSchema{Type: "array", Items: &ParameterSchema{Type: "string"}, Description: "任务容器命令；镜像、环境变量与挂载跟随同一服务"}
			parameters["taskArgs"] = ParameterSchema{Type: "array", Items: &ParameterSchema{Type: "string"}}
			defaults["taskSchedule"], defaults["taskTimeZone"], defaults["taskSuspend"] = "0 2 * * *", "Etc/UTC", true
			defaults["taskCommand"], defaults["taskArgs"] = []any{}, []any{}
			overrides = append(overrides, "taskSchedule", "taskTimeZone", "taskSuspend")
			workload += builtinWorkloadCronJobYAML
		}
		items = append(items, DeploymentTemplateSpec{
			Key: "soha-" + kind, Name: title, Description: fmt.Sprintf("%s；使用已有容器入口或显式 command/args，需要可拉取的镜像及目标命名空间。", title),
			Source:          DeploymentTemplateSource{Renderer: "raw_yaml", Files: []domainmanifest.File{{Path: "workload.yaml", Content: workload}}},
			ParameterSchema: ParameterSchema{Type: "object", Properties: parameters}, Defaults: defaults, EnvironmentOverrides: overrides,
			Artifacts: map[string]string{"main": "main"}, Health: DeploymentTemplateHealth{Mode: mode, TimeoutSeconds: 300}, Enabled: true,
		})
		if kind == "http-periodic" {
			items[len(items)-1].Description = "同一发布清单包含 HTTP 服务及 WorkloadCronJob；任务镜像、环境变量与挂载跟随服务。目标集群须安装 Soha Operator；任务默认暂停，配置命令后显式启用。"
		}
	}
	return append(items, builtinGitOpsDeploymentTemplate(), builtinRolloutDeploymentTemplate(false), builtinRolloutDeploymentTemplate(true))
}

func builtinGitOpsDeploymentTemplate() DeploymentTemplateSpec {
	minimum := 1
	return DeploymentTemplateSpec{
		Key: "soha-gitops", Name: "Argo CD GitOps 服务", Enabled: true,
		Description: "使用固定 Git 提交与本次发布的镜像摘要；仓库中的镜像名称须与服务镜像仓库一致。目标命名空间须已配置 Argo CD、专用受限项目及仓库凭据。",
		ParameterSchema: ParameterSchema{Type: "object", Required: []string{"repositoryId", "repositoryURL", "commit", "project"}, Properties: map[string]ParameterSchema{
			"repositoryId":  {Type: "string", MinLength: &minimum, Description: "应用已关联的 Git 仓库 ID"},
			"repositoryURL": {Type: "string", MinLength: &minimum, Description: "与已登记仓库一致的 URL；不含凭据"},
			"commit":        {Type: "string", MinLength: &minimum, Description: "部署配置的完整 Git commit SHA"},
			"path":          {Type: "string", MinLength: &minimum, Description: "仓库内的 Kustomize 目录"},
			"project":       {Type: "string", MinLength: &minimum, Description: "仅授权目标命名空间及仓库的 Argo CD AppProject"},
		}},
		Defaults: map[string]any{"path": "."}, EnvironmentOverrides: []string{"commit", "path", "project"},
		Artifacts: map[string]string{"main": "main"}, Health: DeploymentTemplateHealth{Mode: "workload_ready", TimeoutSeconds: 300},
		Source: DeploymentTemplateSource{Renderer: "raw_yaml", Files: []domainmanifest.File{{Path: "application.yaml", Content: `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: ${{ system.serviceKey }}
  namespace: ${{ system.namespace }}
  annotations:
    delivery.soha.io/repository-id: ${{ parameters.repositoryId }}
spec:
  project: ${{ parameters.project }}
  source:
    repoURL: ${{ parameters.repositoryURL }}
    targetRevision: ${{ parameters.commit }}
    path: ${{ parameters.path }}
    kustomize:
      namespace: ${{ system.namespace }}
      images:
        - ${{ artifacts.main }}
  destination:
    server: https://kubernetes.default.svc
    namespace: ${{ system.namespace }}
  syncPolicy:
    syncOptions:
      - FailOnSharedResource=true
`}}},
	}
}

const builtinDeploymentYAML = `apiVersion: apps/v1
kind: Deployment
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
`

const builtinJobYAML = `apiVersion: batch/v1
kind: Job
metadata:
  name: ${{ system.serviceKey }}
  namespace: ${{ system.namespace }}
spec:
  backoffLimit: 0
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: main
          image: ${{ artifacts.main }}
          command: ${{ parameters.command }}
          args: ${{ parameters.args }}
          env: ${{ parameters.env }}
`

const builtinHTTPServiceYAML = `          ports:
            - name: http
              containerPort: ${{ parameters.port }}
          readinessProbe:
            httpGet:
              path: ${{ parameters.healthPath }}
              port: http
---
apiVersion: v1
kind: Service
metadata:
  name: ${{ system.serviceKey }}
  namespace: ${{ system.namespace }}
spec:
  selector:
    app.kubernetes.io/name: ${{ system.serviceKey }}
  ports:
    - name: http
      port: ${{ parameters.port }}
      targetPort: http
`

const builtinWorkloadCronJobYAML = `---
apiVersion: workloads.soha.io/v1alpha1
kind: WorkloadCronJob
metadata:
  name: ${{ system.serviceKey }}
  namespace: ${{ system.namespace }}
spec:
  sourceRef:
    kind: Deployment
    name: ${{ system.serviceKey }}
    container: main
  targetContainer: task
  cronJobSpec:
    schedule: ${{ parameters.taskSchedule }}
    timeZone: ${{ parameters.taskTimeZone }}
    suspend: ${{ parameters.taskSuspend }}
    concurrencyPolicy: Forbid
    jobTemplate:
      spec:
        backoffLimit: 0
        template:
          spec:
            restartPolicy: Never
            containers:
              - name: task
                image: ${{ artifacts.main }}
                command: ${{ parameters.taskCommand }}
                args: ${{ parameters.taskArgs }}
`
