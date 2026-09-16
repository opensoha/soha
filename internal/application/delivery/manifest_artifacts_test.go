package delivery

import (
	"strings"
	"testing"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
)

func TestManifestArtifactsMatchBuildSourceContainerAndReportedDigest(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, wrong := range []string{"", "source", "container", "digest", "reported-output"} {
		t.Run(wrong, func(t *testing.T) {
			bundle := domaindelivery.ReleaseBundle{ID: "bundle", ArtifactRef: "registry:5000/api:v1", ArtifactDigest: digest}
			task := domaindelivery.ExecutionTask{Payload: map[string]any{"buildSourceId": "source", "image": bundle.ArtifactRef}, Result: map[string]any{"image": bundle.ArtifactRef, "imageDigest": digest}}
			service := domainapp.Service{ID: "api", BuildSourceID: "source", Containers: []domainapp.ServiceContainer{{Name: "main", ImageRepository: "registry:5000/api"}, {Name: "metrics", ImageRepository: "registry:5000/metrics"}}}
			switch wrong {
			case "source":
				task.Payload["buildSourceId"] = "other"
			case "container":
				service.Containers[0].ImageRepository = "registry:5000/other"
			case "digest":
				bundle.ArtifactDigest = "sha256:" + strings.Repeat("b", 64)
			case "reported-output":
				task.Result["image"] = "registry:5000/other:v1"
			}
			images, err := serviceBundleArtifacts(bundle, task, service)
			if (err == nil) != (wrong == "") {
				t.Fatalf("artifact selection = %+v, %v", images, err)
			}
			if wrong == "" && (images.ServiceID != "api" || len(images.ContainerImages) != 1 || images.ContainerImages["main"] != "registry:5000/api@"+digest) {
				t.Fatalf("artifact mapped to the wrong container: %+v", images)
			}
		})
	}
}
