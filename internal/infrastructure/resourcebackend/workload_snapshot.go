package resourcebackend

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

var generatedTemplateMetadata = map[string]struct{}{
	"apps.kubernetes.io/pod-index":             {},
	"batch.kubernetes.io/controller-uid":       {},
	"batch.kubernetes.io/job-name":             {},
	"controller-revision-hash":                 {},
	"controller-uid":                           {},
	"deployment.kubernetes.io/revision":        {},
	"deprecated.daemonset.template.generation": {},
	"job-name": {},
	"kubectl.kubernetes.io/last-applied-configuration": {},
	"pod-template-hash":                  {},
	"statefulset.kubernetes.io/pod-name": {},
}

type workloadCronJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              workloadCronJobSpec `json:"spec"`
}

type workloadCronJobSpec struct {
	SourceRef       workloadReference   `json:"sourceRef"`
	TargetContainer string              `json:"targetContainer"`
	CronJobSpec     batchv1.CronJobSpec `json:"cronJobSpec"`
}

type workloadReference struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Container string `json:"container"`
}

func (d *Direct) BuildWorkloadSnapshot(sourceYAML string, request domainresource.WorkloadSnapshotRequest) (domainresource.WorkloadSnapshot, error) {
	template, uid, containers, err := workloadSnapshotSource(sourceYAML, request)
	if err != nil {
		return domainresource.WorkloadSnapshot{}, err
	}
	selected := request.SourceContainer
	if selected == "" {
		selected = containers[0].Name
	}
	containerIndex := slices.IndexFunc(template.Spec.Containers, func(container corev1.Container) bool { return container.Name == selected })
	if containerIndex < 0 {
		return domainresource.WorkloadSnapshot{}, fmt.Errorf("%w: source container %q was not found", apperrors.ErrInvalidArgument, selected)
	}

	warnings := make([]string, 0, 3)
	if request.SourceContainer == "" {
		warnings = append(warnings, fmt.Sprintf("selected the first regular container %q", selected))
	}
	if len(template.Spec.Containers) > 1 {
		warnings = append(warnings, fmt.Sprintf("omitted %d additional regular container(s)", len(template.Spec.Containers)-1))
	}
	if template.Spec.RestartPolicy != corev1.RestartPolicy(request.RestartPolicy) {
		warnings = append(warnings, fmt.Sprintf("changed restartPolicy from %q to %q", template.Spec.RestartPolicy, request.RestartPolicy))
	}
	if len(template.Spec.EphemeralContainers) > 0 {
		warnings = append(warnings, "omitted ephemeral containers")
	}

	container := template.Spec.Containers[containerIndex].DeepCopy()
	if len(request.Command) > 0 {
		container.Command = slices.Clone(request.Command)
	}
	if len(request.Args) > 0 {
		container.Args = slices.Clone(request.Args)
	}
	template.Spec.Containers = []corev1.Container{*container}
	template.Spec.EphemeralContainers = nil
	template.Spec.RestartPolicy = corev1.RestartPolicy(request.RestartPolicy)
	cleanPodTemplateMetadata(&template.ObjectMeta)

	jobSpec := batchv1.JobSpec{Template: *template}
	if request.Parallelism > 0 {
		value := int32(request.Parallelism)
		jobSpec.Parallelism = &value
	}
	if request.Completions > 0 {
		value := int32(request.Completions)
		jobSpec.Completions = &value
	}
	backoffLimit := int32(request.BackoffLimit)
	jobSpec.BackoffLimit = &backoffLimit
	if request.ActiveDeadlineSeconds > 0 {
		value := int64(request.ActiveDeadlineSeconds)
		jobSpec.ActiveDeadlineSeconds = &value
	}

	metadata := metav1.ObjectMeta{
		Name: request.TargetName, Namespace: request.Namespace,
		Labels: maps.Clone(request.Labels), Annotations: maps.Clone(request.Annotations),
	}
	if metadata.Annotations == nil {
		metadata.Annotations = map[string]string{}
	}
	metadata.Annotations["soha.io/source-kind"] = string(request.SourceKind)
	metadata.Annotations["soha.io/source-namespace"] = request.Namespace
	metadata.Annotations["soha.io/source-name"] = request.SourceName
	metadata.Annotations["soha.io/source-container"] = selected
	metadata.Annotations["soha.io/source-uid"] = string(uid)
	if request.Description != "" {
		metadata.Annotations["soha.io/description"] = request.Description
	}

	var target any
	if request.TargetKind == domainresource.WorkloadSnapshotTargetCronJob || request.TargetKind == domainresource.WorkloadSnapshotTargetWorkloadCronJob {
		suspend := request.Suspend
		cronJobSpec := batchv1.CronJobSpec{Schedule: request.Schedule, Suspend: &suspend, JobTemplate: batchv1.JobTemplateSpec{Spec: jobSpec}}
		if request.TargetKind == domainresource.WorkloadSnapshotTargetWorkloadCronJob {
			target = &workloadCronJob{
				TypeMeta: metav1.TypeMeta{APIVersion: "workloads.soha.io/v1alpha1", Kind: "WorkloadCronJob"}, ObjectMeta: metadata,
				Spec: workloadCronJobSpec{
					SourceRef:       workloadReference{Kind: string(request.SourceKind), Name: request.SourceName, Container: selected},
					TargetContainer: selected,
					CronJobSpec:     cronJobSpec,
				},
			}
		} else {
			target = &batchv1.CronJob{
				TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "CronJob"}, ObjectMeta: metadata,
				Spec: cronJobSpec,
			}
		}
	} else {
		target = &batchv1.Job{TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"}, ObjectMeta: metadata, Spec: jobSpec}
	}
	content, err := yaml.Marshal(target)
	if err != nil {
		return domainresource.WorkloadSnapshot{}, err
	}
	return domainresource.WorkloadSnapshot{
		Content: string(content), SourceUID: string(uid), SelectedContainer: selected,
		Containers: containers, Warnings: warnings,
	}, nil
}

func workloadSnapshotSource(sourceYAML string, request domainresource.WorkloadSnapshotRequest) (*corev1.PodTemplateSpec, types.UID, []domainresource.WorkloadSnapshotContainer, error) {
	var metadata metav1.ObjectMeta
	var template *corev1.PodTemplateSpec
	switch request.SourceKind {
	case domainresource.WorkloadSnapshotSourceDeployment:
		var source appsv1.Deployment
		if err := yaml.Unmarshal([]byte(sourceYAML), &source); err != nil {
			return nil, "", nil, invalidSnapshotSource(request.SourceKind, err)
		}
		metadata, template = source.ObjectMeta, source.Spec.Template.DeepCopy()
	case domainresource.WorkloadSnapshotSourceStatefulSet:
		var source appsv1.StatefulSet
		if err := yaml.Unmarshal([]byte(sourceYAML), &source); err != nil {
			return nil, "", nil, invalidSnapshotSource(request.SourceKind, err)
		}
		metadata, template = source.ObjectMeta, source.Spec.Template.DeepCopy()
	case domainresource.WorkloadSnapshotSourceDaemonSet:
		var source appsv1.DaemonSet
		if err := yaml.Unmarshal([]byte(sourceYAML), &source); err != nil {
			return nil, "", nil, invalidSnapshotSource(request.SourceKind, err)
		}
		metadata, template = source.ObjectMeta, source.Spec.Template.DeepCopy()
	default:
		return nil, "", nil, fmt.Errorf("%w: unsupported source kind", apperrors.ErrInvalidArgument)
	}
	if metadata.Name != request.SourceName || metadata.Namespace != request.Namespace || metadata.UID == "" {
		return nil, "", nil, fmt.Errorf("%w: source workload identity does not match the request", apperrors.ErrInvalidArgument)
	}
	if len(template.Spec.Containers) == 0 {
		return nil, "", nil, fmt.Errorf("%w: source workload has no regular containers", apperrors.ErrInvalidArgument)
	}
	containers := make([]domainresource.WorkloadSnapshotContainer, 0, len(template.Spec.Containers))
	for _, container := range template.Spec.Containers {
		containers = append(containers, domainresource.WorkloadSnapshotContainer{Name: container.Name, Image: container.Image})
	}
	return template, metadata.UID, containers, nil
}

func invalidSnapshotSource(kind domainresource.WorkloadSnapshotSourceKind, err error) error {
	return fmt.Errorf("%w: invalid %s YAML: %v", apperrors.ErrInvalidArgument, kind, err)
}

func cleanPodTemplateMetadata(metadata *metav1.ObjectMeta) {
	metadata.Name = ""
	metadata.GenerateName = ""
	metadata.Namespace = ""
	metadata.UID = ""
	metadata.ResourceVersion = ""
	metadata.Generation = 0
	metadata.CreationTimestamp = metav1.Time{}
	metadata.DeletionTimestamp = nil
	metadata.DeletionGracePeriodSeconds = nil
	metadata.OwnerReferences = nil
	metadata.Finalizers = nil
	metadata.ManagedFields = nil
	metadata.Labels = cleanTemplateMap(metadata.Labels)
	metadata.Annotations = cleanTemplateMap(metadata.Annotations)
}

func cleanTemplateMap(values map[string]string) map[string]string {
	cleaned := maps.Clone(values)
	for key := range cleaned {
		if _, generated := generatedTemplateMetadata[strings.ToLower(key)]; generated {
			delete(cleaned, key)
		}
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}
