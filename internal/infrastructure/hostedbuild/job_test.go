package hostedbuild

import (
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestPlatformsRenderFixedNonRootCommands(t *testing.T) {
	t.Parallel()
	b, _ := testBuilder(t)
	cases := []struct {
		name    string
		base    string
		install string
		entry   string
	}{
		{name: "node", base: b.cfg.NodeBase, install: "npm ci --ignore-scripts", entry: `["node", "server.js"]`},
		{name: "python", base: b.cfg.PythonBase, install: "--only-binary=:all: --no-deps", entry: `["python", "app.py"]`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dockerfile, err := b.dockerfile(tt.name)
			if err != nil {
				t.Fatal(err)
			}
			for _, part := range []string{"FROM " + tt.base + "\n", "USER 10001:10001\n", "PORT=8080", tt.install, "ENTRYPOINT " + tt.entry} {
				if !strings.Contains(dockerfile, part) {
					t.Fatalf("recipe missing %q", part)
				}
			}
			if strings.Index(dockerfile, tt.install) > strings.Index(dockerfile, "COPY --chown=10001:10001 . .") {
				t.Fatal("project files reached dependency install stage")
			}
		})
	}
}

func TestBuildJobSeparatesCredentialsAndDisablesPrivileges(t *testing.T) {
	t.Parallel()
	b, _ := testBuilder(t)
	dockerfile, err := b.dockerfile("node")
	if err != nil {
		t.Fatal(err)
	}
	job := b.job(testRevision, "node", dockerfile, "/private-object")
	pod := job.Spec.Template.Spec
	if (job.Spec.Suspend == nil || !*job.Spec.Suspend) || (pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken) ||
		(pod.SecurityContext.RunAsNonRoot == nil || !*pod.SecurityContext.RunAsNonRoot) || pod.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatal("Job lacks suspension or pod privileges are insufficiently constrained")
	}
	fetch, builder := pod.InitContainers[0], pod.Containers[0]
	assertBuildCredentialMounts(t, fetch, builder)
	for _, container := range []corev1.Container{fetch, builder} {
		if container.Image != b.cfg.BuildClientImage || (container.SecurityContext.AllowPrivilegeEscalation == nil || *container.SecurityContext.AllowPrivilegeEscalation) ||
			(container.SecurityContext.ReadOnlyRootFilesystem == nil || !*container.SecurityContext.ReadOnlyRootFilesystem) || len(container.Resources.Limits) != 3 {
			t.Fatalf("unexpected container security or limits for %s", container.Name)
		}
	}
	body, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "/private-object") || strings.Contains(string(body), "hostPath") {
		t.Fatal("Job includes private source locator or host mount")
	}
}

func assertBuildCredentialMounts(t *testing.T, fetch, builder corev1.Container) {
	t.Helper()
	if !mounts(fetch, "source-credential") || mounts(fetch, "docker-config") || mounts(fetch, "buildkit-tls") ||
		mounts(builder, "source-credential") || !mounts(builder, "docker-config") || !mounts(builder, "buildkit-tls") {
		t.Fatal("unexpected credential volume mounts")
	}
}

func mounts(container corev1.Container, name string) bool {
	for _, mount := range container.VolumeMounts {
		if mount.Name == name {
			return mount.ReadOnly
		}
	}
	return false
}
