package resource

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.yaml.in/yaml/v3"
	"golang.org/x/sync/errgroup"
)

type helmWorkloadDocument struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name        string            `yaml:"name"`
		Namespace   string            `yaml:"namespace"`
		Labels      map[string]string `yaml:"labels"`
		Annotations map[string]string `yaml:"annotations"`
	} `yaml:"metadata"`
	Items []helmWorkloadDocument `yaml:"items"`
}

// GetHelmReleaseWorkloadSources keeps Helm and workload authorization on their existing read paths.
func (r *Runtime) GetHelmReleaseWorkloadSources(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, releaseName string) ([]domainresource.ResourceSourceSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	release, err := r.GetHelmReleaseDetail(ctx, principal, clusterID, namespace, releaseName)
	if err != nil {
		return nil, err
	}
	if release.Status != "deployed" {
		return nil, fmt.Errorf("%w: Helm release is not deployed", apperrors.ErrConflict)
	}
	connection, _, err := r.Helm.authorize(ctx, principal, clusterID, namespace, "HelmRelease", domainaccess.ActionView)
	if err != nil {
		return nil, err
	}
	// Only workload identities leave this method; raw manifest/values still require their separate permission.
	manifest, err := r.readHelmReleaseManifest(ctx, connection, clusterID, namespace, releaseName, release.Revision)
	if err != nil {
		return nil, err
	}
	if manifest.Revision != release.Revision {
		return nil, fmt.Errorf("%w: Helm release revision changed", apperrors.ErrConflict)
	}
	refs, err := helmWorkloadRefs(manifest.Content, namespace)
	if err != nil {
		return nil, err
	}
	sources := make([]domainresource.ResourceSourceSnapshot, len(refs))
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(4)
	for index, ref := range refs {
		group.Go(func() error {
			source, current, readErr := r.readImportSource(ctx, principal, clusterID, namespace, ref.Kind, ref.Name, "")
			if readErr != nil {
				return readErr
			}
			var object helmWorkloadDocument
			if yaml.Unmarshal([]byte(current.Content), &object) != nil ||
				object.Metadata.Labels["app.kubernetes.io/managed-by"] != "Helm" ||
				object.Metadata.Annotations["meta.helm.sh/release-name"] != releaseName ||
				object.Metadata.Annotations["meta.helm.sh/release-namespace"] != namespace {
				return fmt.Errorf("%w: live workload is not owned by this Helm release", apperrors.ErrConflict)
			}
			sources[index] = source
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return sources, nil
}

func helmWorkloadRefs(content, namespace string) ([]domainresource.ResourceRef, error) {
	invalid := func() ([]domainresource.ResourceRef, error) {
		return nil, fmt.Errorf("%w: Helm workload manifest is invalid or exceeds the observation limit", apperrors.ErrInvalidArgument)
	}
	if len(content) > 8*1024*1024 {
		return invalid()
	}
	decoder := yaml.NewDecoder(strings.NewReader(content))
	p := helmWorkloadParser{namespace: namespace, seen: map[string]bool{}}
	for count := 0; count <= 1000; count++ {
		var document helmWorkloadDocument
		err := decoder.Decode(&document)
		if err == io.EOF {
			return p.refs, nil
		}
		if err != nil || count == 1000 || !p.visit(document, 0) {
			return invalid()
		}
	}
	return invalid()
}

type helmWorkloadParser struct {
	namespace string
	refs      []domainresource.ResourceRef
	seen      map[string]bool
}

func (p *helmWorkloadParser) visit(document helmWorkloadDocument, depth int) bool {
	if depth > 4 || len(p.refs) >= 300 {
		return false
	}
	if document.Kind == "List" {
		for _, item := range document.Items {
			if !p.visit(item, depth+1) {
				return false
			}
		}
		return true
	}
	if document.APIVersion != "apps/v1" ||
		(document.Kind != "Deployment" && document.Kind != "StatefulSet" && document.Kind != "DaemonSet") ||
		document.Metadata.Annotations["helm.sh/hook"] != "" {
		return true
	}
	actualNamespace := document.Metadata.Namespace
	if actualNamespace == "" {
		actualNamespace = p.namespace
	}
	if actualNamespace != p.namespace {
		return true
	}
	if document.Metadata.Name == "" {
		return false
	}
	key := document.Kind + "/" + document.Metadata.Name
	if !p.seen[key] {
		p.seen[key] = true
		p.refs = append(p.refs, domainresource.ResourceRef{APIVersion: document.APIVersion, Kind: document.Kind, Namespace: p.namespace, Name: document.Metadata.Name})
	}
	return true
}
