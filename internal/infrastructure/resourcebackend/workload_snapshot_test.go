package resourcebackend

import (
	"fmt"
	"testing"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/yaml"
)

func TestBuildWorkloadSnapshotCopiesRuntimeAndBuildsBatchResources(t *testing.T) {
	t.Parallel()
	direct := &Direct{}
	for _, sourceKind := range []domainresource.WorkloadSnapshotSourceKind{
		domainresource.WorkloadSnapshotSourceDeployment,
		domainresource.WorkloadSnapshotSourceStatefulSet,
		domainresource.WorkloadSnapshotSourceDaemonSet,
	} {
		t.Run(string(sourceKind), func(t *testing.T) {
			result, err := direct.BuildWorkloadSnapshot(snapshotSourceYAML(string(sourceKind)), domainresource.WorkloadSnapshotRequest{
				Namespace: "ops", SourceKind: sourceKind, SourceName: "api", SourceContainer: "worker",
				TargetKind: domainresource.WorkloadSnapshotTargetJob, TargetName: "report", RestartPolicy: domainresource.WorkloadSnapshotRestartNever,
				Description: "nightly report", Command: []string{"php"}, Args: []string{"artisan", "report"}, BackoffLimit: 0,
			})
			if err != nil {
				t.Fatalf("BuildWorkloadSnapshot() error = %v", err)
			}
			if result.SelectedContainer != "worker" || len(result.Containers) != 2 || len(result.Warnings) == 0 {
				t.Fatalf("snapshot summary = %#v", result)
			}
			var job batchv1.Job
			if err := yaml.Unmarshal([]byte(result.Content), &job); err != nil {
				t.Fatalf("decode Job: %v", err)
			}
			containers := job.Spec.Template.Spec.Containers
			if len(containers) != 1 || containers[0].Name != "worker" || len(containers[0].Env) != 1 || len(containers[0].Command) != 1 || len(containers[0].Args) != 2 {
				t.Fatalf("selected container = %#v", containers)
			}
			if containers[0].LivenessProbe == nil || containers[0].ReadinessProbe == nil {
				t.Fatalf("legacy snapshot omitted probes = %#v", containers[0])
			}
			if job.Spec.Template.Spec.ServiceAccountName != "runtime" || len(job.Spec.Template.Spec.InitContainers) != 1 || len(job.Spec.Template.Spec.Volumes) != 1 || job.Spec.Template.Spec.RestartPolicy != "Never" {
				t.Fatalf("copied pod spec = %#v", job.Spec.Template.Spec)
			}
			if len(job.OwnerReferences) != 0 || len(job.Spec.Template.OwnerReferences) != 0 || job.Spec.Template.Labels["pod-template-hash"] != "" || job.Spec.Template.Labels["app"] != "api" {
				t.Fatalf("cleaned metadata = target %#v template %#v", job.ObjectMeta, job.Spec.Template.ObjectMeta)
			}
			if job.Annotations["soha.io/source-kind"] != string(sourceKind) || job.Annotations["soha.io/source-uid"] != "source-uid" || job.Annotations["soha.io/description"] != "nightly report" {
				t.Fatalf("provenance annotations = %#v", job.Annotations)
			}
		})
	}

	result, err := direct.BuildWorkloadSnapshot(snapshotSourceYAML("StatefulSet"), domainresource.WorkloadSnapshotRequest{
		Namespace: "ops", SourceKind: domainresource.WorkloadSnapshotSourceStatefulSet, SourceName: "api",
		TargetKind: domainresource.WorkloadSnapshotTargetCronJob, TargetName: "hourly", RestartPolicy: domainresource.WorkloadSnapshotRestartOnFailure,
		Schedule: "0 * * * *", Suspend: true, BackoffLimit: 6,
	})
	if err != nil {
		t.Fatalf("BuildWorkloadSnapshot(CronJob) error = %v", err)
	}
	var cronJob batchv1.CronJob
	if err := yaml.Unmarshal([]byte(result.Content), &cronJob); err != nil {
		t.Fatalf("decode CronJob: %v", err)
	}
	if cronJob.Spec.Schedule != "0 * * * *" || cronJob.Spec.Suspend == nil || !*cronJob.Spec.Suspend || cronJob.Spec.JobTemplate.Spec.Template.Spec.RestartPolicy != "OnFailure" {
		t.Fatalf("CronJob spec = %#v", cronJob.Spec)
	}

	result, err = direct.BuildWorkloadSnapshot(snapshotSourceYAML("Deployment"), domainresource.WorkloadSnapshotRequest{
		Namespace: "ops", SourceKind: domainresource.WorkloadSnapshotSourceDeployment, SourceName: "api", SourceContainer: "worker",
		TargetKind: domainresource.WorkloadSnapshotTargetWorkloadCronJob, TargetName: "following", RestartPolicy: domainresource.WorkloadSnapshotRestartNever,
		Schedule: "*/5 * * * *", BackoffLimit: 2,
	})
	if err != nil {
		t.Fatalf("BuildWorkloadSnapshot(WorkloadCronJob) error = %v", err)
	}
	var following workloadCronJob
	if err := yaml.Unmarshal([]byte(result.Content), &following); err != nil {
		t.Fatalf("decode WorkloadCronJob: %v", err)
	}
	container := following.Spec.CronJobSpec.JobTemplate.Spec.Template.Spec.Containers[0]
	if following.APIVersion != "workloads.soha.io/v1alpha1" || following.Kind != "WorkloadCronJob" ||
		following.Spec.SourceRef != (workloadReference{Kind: "Deployment", Name: "api", Container: "worker"}) ||
		following.Spec.TargetContainer != "worker" || following.Spec.CronJobSpec.Schedule != "*/5 * * * *" ||
		container.Name != "worker" || container.Image != "example/api:1" || len(container.Env) != 1 {
		t.Fatalf("WorkloadCronJob = %#v", following)
	}
}

func TestBuildWorkloadSnapshotSelectsRuntimeModules(t *testing.T) {
	t.Parallel()
	result, err := (&Direct{}).BuildWorkloadSnapshot(snapshotSourceYAML("Deployment"), domainresource.WorkloadSnapshotRequest{
		Namespace: "ops", SourceKind: domainresource.WorkloadSnapshotSourceDeployment, SourceName: "api", SourceContainer: "worker",
		TargetKind: domainresource.WorkloadSnapshotTargetJob, TargetName: "report", RestartPolicy: domainresource.WorkloadSnapshotRestartNever,
		Inherit: []domainresource.WorkloadSnapshotInheritance{
			domainresource.WorkloadSnapshotInheritEnvironment,
			domainresource.WorkloadSnapshotInheritStorage,
			domainresource.WorkloadSnapshotInheritResources,
			domainresource.WorkloadSnapshotInheritSecurityContext,
			domainresource.WorkloadSnapshotInheritScheduling,
		},
	})
	if err != nil {
		t.Fatalf("BuildWorkloadSnapshot() error = %v", err)
	}
	var job batchv1.Job
	if err := yaml.Unmarshal([]byte(result.Content), &job); err != nil {
		t.Fatalf("decode Job: %v", err)
	}
	template := job.Spec.Template
	container := template.Spec.Containers[0]
	if len(template.Labels) != 0 || len(template.Annotations) != 0 {
		t.Fatalf("template metadata was inherited = %#v", template.ObjectMeta)
	}
	if container.LivenessProbe != nil || container.ReadinessProbe != nil || container.StartupProbe != nil || container.Lifecycle != nil || len(container.Ports) != 0 {
		t.Fatalf("service runtime was inherited = %#v", container)
	}
	if len(template.Spec.InitContainers) != 0 {
		t.Fatalf("init containers were inherited = %#v", template.Spec.InitContainers)
	}
	if len(container.Env) != 1 || len(container.EnvFrom) != 1 || len(container.VolumeMounts) != 1 || len(container.Resources.Limits) != 1 || container.SecurityContext == nil ||
		len(template.Spec.Volumes) != 1 || template.Spec.ServiceAccountName != "runtime" || template.Spec.NodeSelector["pool"] != "batch" || template.Spec.SecurityContext == nil {
		t.Fatalf("selected runtime modules = %#v", template.Spec)
	}
}

func snapshotSourceYAML(kind string) string {
	return fmt.Sprintf(`apiVersion: apps/v1
kind: %s
metadata:
  name: api
  namespace: ops
  uid: source-uid
spec:
  template:
    metadata:
      labels:
        app: api
        pod-template-hash: generated
      annotations:
        workload.example/revision: one
      ownerReferences:
        - apiVersion: apps/v1
          kind: ReplicaSet
          name: api-generated
          uid: generated-uid
    spec:
      serviceAccountName: runtime
      nodeSelector:
        pool: batch
      securityContext:
        runAsNonRoot: true
      restartPolicy: Always
      initContainers:
        - name: prepare
          image: busybox:1.36
      containers:
        - name: worker
          image: example/api:1
          ports:
            - name: http
              containerPort: 8080
          env:
            - name: APP_ENV
              value: production
          envFrom:
            - configMapRef:
                name: api-env
          resources:
            limits:
              cpu: "1"
          securityContext:
            allowPrivilegeEscalation: false
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
          readinessProbe:
            httpGet:
              path: /readyz
              port: http
          startupProbe:
            httpGet:
              path: /startupz
              port: http
          lifecycle:
            preStop:
              exec:
                command: ["/bin/sh", "-c", "true"]
          volumeMounts:
            - name: config
              mountPath: /etc/app
        - name: metrics
          image: example/metrics:1
      volumes:
        - name: config
          configMap:
            name: api-config
`, kind)
}
