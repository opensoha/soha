package hostedbuild

import (
	_ "embed"
	"errors"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//go:embed source.py
var sourceScript string

const buildScript = `umask 077
mkdir /scratch/recipe
printf '%s\n' "$1" > /scratch/recipe/Dockerfile
if ! buildctl --addr "$2" --tlscacert /buildkit-tls/ca.crt \
  --tlscert /buildkit-tls/tls.crt --tlskey /buildkit-tls/tls.key build \
  --frontend dockerfile.v0 --local context=/source --local dockerfile=/scratch/recipe \
  --no-cache --output "type=image,name=$3,push=true,oci-mediatypes=true" \
  --metadata-file /scratch/metadata.json >/dev/null 2>&1; then
  printf '%s\n' 'Hosted image build or push failed' >&2
  exit 1
fi
python3 -I -c '
import json
import re

try:
    with open("/scratch/metadata.json", encoding="utf-8") as metadata:
        value = json.load(metadata)["containerimage.digest"]
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", value):
        raise ValueError()
    with open("/dev/termination-log", "w", encoding="ascii") as result:
        result.write(value)
except (OSError, KeyError, TypeError, ValueError):
    raise SystemExit("Hosted image has no valid manifest digest") from None
'`

const imageSetup = `USER 0
RUN mkdir -p /app && chown 10001:10001 /app
WORKDIR /app
USER 10001:10001
ENV HOME=/tmp PORT=8080
`

func (b *Builder) dockerfile(template string) (string, error) {
	switch template {
	case "node":
		return fmt.Sprintf(`FROM %s
%sCOPY --chown=10001:10001 package.json package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund --registry=https://registry.npmjs.org && npm cache clean --force
COPY --chown=10001:10001 . .
EXPOSE 8080
ENTRYPOINT ["node", "server.js"]
`, b.cfg.NodeBase, imageSetup), nil
	case "python":
		return fmt.Sprintf(`FROM %s
%sCOPY --chown=10001:10001 requirements.txt ./
RUN python -m pip --isolated install --no-input --disable-pip-version-check --no-cache-dir --only-binary=:all: --no-deps --index-url https://pypi.org/simple --target /app/.deps -r requirements.txt
COPY --chown=10001:10001 . .
ENV PYTHONPATH=/app/.deps
EXPOSE 8080
ENTRYPOINT ["python", "app.py"]
`, b.cfg.PythonBase, imageSetup), nil
	default:
		return "", errors.New("unsupported hosted build template")
	}
}

func (b *Builder) job(revisionID, template, dockerfile, sourcePath string) *batchv1.Job {
	name := jobName(revisionID)
	labels := map[string]string{managedByLabel: managedBy, revisionLabel: revisionID}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: b.cfg.Namespace,
			Labels:    labels,
			Annotations: map[string]string{
				inputAnnotation: buildInputHash(b.cfg, template, dockerfile, sourcePath),
				repoAnnotation:  b.cfg.RegistryRepository,
			},
		},
		Spec: batchv1.JobSpec{
			Suspend:               new(true),
			BackoffLimit:          new(int32(0)),
			Parallelism:           new(int32(1)),
			Completions:           new(int32(1)),
			ActiveDeadlineSeconds: new(int64(600)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       b.pod(name, template, dockerfile),
			},
		},
	}
}

func (b *Builder) pod(name, template, dockerfile string) corev1.PodSpec {
	pod := corev1.PodSpec{
		AutomountServiceAccountToken:  new(false),
		EnableServiceLinks:            new(false),
		RestartPolicy:                 corev1.RestartPolicyNever,
		TerminationGracePeriodSeconds: new(int64(15)),
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot:   new(true),
			RunAsUser:      new(int64(10001)),
			RunAsGroup:     new(int64(10001)),
			FSGroup:        new(int64(10001)),
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
		Volumes: []corev1.Volume{
			scratchVolume("source", "180Mi"),
			scratchVolume("scratch", "40Mi"),
			secretVolume("source-credential", name+"-source", sourceKey),
			secretVolume("docker-config", b.cfg.DockerConfigSecret, corev1.DockerConfigJsonKey),
			secretVolume("buildkit-tls", b.cfg.BuildKitTLSSecret, "ca.crt", "tls.crt", "tls.key"),
		},
		InitContainers: []corev1.Container{b.sourceContainer(template)},
		Containers:     []corev1.Container{b.buildContainer(name, dockerfile)},
	}
	if b.cfg.RuntimeClassName != "" {
		pod.RuntimeClassName = &b.cfg.RuntimeClassName
	}
	return pod
}

func (b *Builder) sourceContainer(template string) corev1.Container {
	container := b.container("fetch-source")
	container.Command = []string{"python3", "-I", "-c"}
	container.Args = []string{sourceScript, template}
	container.VolumeMounts = []corev1.VolumeMount{
		{Name: "source", MountPath: "/source"},
		{Name: "scratch", MountPath: "/scratch"},
		{Name: "source-credential", MountPath: "/source-credential", ReadOnly: true},
	}
	return container
}

func (b *Builder) buildContainer(name, dockerfile string) corev1.Container {
	container := b.container(buildContainer)
	container.Command = []string{"/bin/sh", "-eu", "-c"}
	container.Args = []string{buildScript, buildContainer, dockerfile, b.cfg.BuildKitAddress, b.cfg.RegistryRepository + ":" + name}
	container.Env = []corev1.EnvVar{
		{Name: "DOCKER_CONFIG", Value: "/registry"},
		{Name: "HOME", Value: "/scratch"},
		{Name: "TMPDIR", Value: "/scratch"},
	}
	container.VolumeMounts = []corev1.VolumeMount{
		{Name: "source", MountPath: "/source", ReadOnly: true},
		{Name: "scratch", MountPath: "/scratch"},
		{Name: "docker-config", MountPath: "/registry", ReadOnly: true},
		{Name: "buildkit-tls", MountPath: "/buildkit-tls", ReadOnly: true},
	}
	return container
}

func (b *Builder) container(name string) corev1.Container {
	return corev1.Container{
		Name:  name,
		Image: b.cfg.BuildClientImage,
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: new(false),
			ReadOnlyRootFilesystem:   new(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:              resource.MustParse("100m"),
				corev1.ResourceMemory:           resource.MustParse("128Mi"),
				corev1.ResourceEphemeralStorage: resource.MustParse("220Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:              resource.MustParse("1"),
				corev1.ResourceMemory:           resource.MustParse("512Mi"),
				corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
	}
}

func scratchVolume(name, size string) corev1.Volume {
	limit := resource.MustParse(size)
	return corev1.Volume{
		Name:         name,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &limit}},
	}
}

func secretVolume(name, secret string, keys ...string) corev1.Volume {
	items := make([]corev1.KeyToPath, 0, len(keys))
	for _, key := range keys {
		file := key
		if key == corev1.DockerConfigJsonKey {
			file = "config.json"
		}
		items = append(items, corev1.KeyToPath{Key: key, Path: file})
	}
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
			SecretName:  secret,
			DefaultMode: new(int32(0440)),
			Items:       items,
		}},
	}
}
