package hostedbuild

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/distribution/reference"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var registryBearerToken = regexp.MustCompile(`^[a-zA-Z0-9._~+/-]+=*$`)

type registryImageCleanup struct {
	host          string
	authorization string
	client        *http.Client
}

// WithRegistryImageCleanup enables per-revision repositories under a dedicated registry prefix.
// The caller supplies a separate authorization credential scoped to that prefix.
func WithRegistryImageCleanup(authorization string) Option {
	return func(b *Builder) error {
		if authorization == "" {
			return nil
		}
		if !validRegistryAuthorization(authorization) {
			return errors.New("invalid hosted registry cleanup authorization")
		}
		// Validate the longest repository shape used by the managed build and deletion paths.
		repo, err := reference.ParseNamed(b.managedImageRepository("rev_" + strings.Repeat("0", 32)))
		if err != nil || !reference.IsNameOnly(repo) || reference.Domain(repo) != strings.ToLower(reference.Domain(repo)) {
			return errors.New("invalid hosted registry cleanup repository")
		}
		b.registryCleanup = &registryImageCleanup{
			host:          reference.Domain(repo),
			authorization: authorization,
			client: &http.Client{
				Timeout: requestTimeout,
				CheckRedirect: func(*http.Request, []*http.Request) error {
					return http.ErrUseLastResponse
				},
			},
		}
		return nil
	}
}

func validRegistryAuthorization(value string) bool {
	if len(value) > 8192 {
		return false
	}
	scheme, credential, _ := strings.Cut(value, " ")
	switch scheme {
	case "Basic":
		decoded, err := base64.StdEncoding.DecodeString(credential)
		username, password, found := strings.Cut(string(decoded), ":")
		return err == nil && found && username != "" && password != "" && !strings.ContainsAny(credential, "\r\n")
	case "Bearer":
		return registryBearerToken.MatchString(credential)
	default:
		return false
	}
}

func (b *Builder) managedImageRepository(revisionID string) string {
	return b.cfg.RegistryRepository + "/" + jobName(revisionID)
}

func (b *Builder) validBuildRepository(revisionID, repo string) bool {
	return repo == b.cfg.RegistryRepository || repo == b.managedImageRepository(revisionID)
}

// IsManagedImage identifies a cleanup-eligible repository and digest in a trusted build result.
func (b *Builder) IsManagedImage(revisionID, imageRef string) bool {
	repo, digest, found := strings.Cut(imageRef, "@")
	return found && revisionPattern.MatchString(revisionID) && repo == b.managedImageRepository(revisionID) && digestPattern.MatchString(digest)
}

func (b *Builder) matchesResultContainer(pod *corev1.Pod, job *batchv1.Job) bool {
	if len(pod.Spec.Containers) != 1 {
		return false
	}
	container := pod.Spec.Containers[0]
	if container.Name != buildContainer || container.Image != b.cfg.BuildClientImage {
		return false
	}
	repo := job.Annotations[repoAnnotation]
	if repo == b.cfg.RegistryRepository {
		return true
	}
	return slices.Equal(container.Command, []string{"/bin/sh", "-eu", "-c"}) &&
		len(container.Args) == 5 && container.Args[0] == buildScript && container.Args[1] == buildContainer &&
		container.Args[3] == b.cfg.BuildKitAddress && container.Args[4] == repo+":"+job.Name
}

// ManagedImageForCleanup finds proof from a succeeded Pod before its Job is removed.
func (b *Builder) ManagedImageForCleanup(ctx context.Context, revisionID string) (string, error) {
	if !revisionPattern.MatchString(revisionID) {
		return "", errors.New("invalid hosted build revision ID")
	}
	job, err := b.client.BatchV1().Jobs(b.cfg.Namespace).Get(ctx, jobName(revisionID), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read hosted build Job for image cleanup: %w", err)
	}
	repository := b.managedImageRepository(revisionID)
	if !b.ownsJob(job, revisionID) || job.Annotations[repoAnnotation] != repository {
		return "", nil
	}
	digest, err := b.resultDigest(ctx, job)
	if errors.Is(err, ErrBuildFailed) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return repository + "@" + digest, nil
}

// DeleteManagedImage removes the top-level manifest emitted by an owned build.
// Per-revision repositories keep deletion isolated from other manifest and blob references.
func (b *Builder) DeleteManagedImage(ctx context.Context, revisionID, imageRef string) error {
	if !b.IsManagedImage(revisionID, imageRef) {
		return errors.New("invalid hosted managed image identity")
	}
	if b.registryCleanup == nil {
		return errors.New("hosted registry image cleanup is unavailable")
	}
	repository, digest, _ := strings.Cut(imageRef, "@")
	_, repositoryPath, _ := strings.Cut(repository, "/")
	return b.registryCleanup.deleteManifest(ctx, repositoryPath, digest)
}

func (c *registryImageCleanup) deleteManifest(ctx context.Context, repository, digest string) error {
	u := url.URL{Scheme: "https", Host: c.host, Path: "/v2/" + repository + "/manifests/" + digest}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
	if err != nil {
		return errors.New("invalid hosted registry deletion request")
	}
	req.Header.Set("Authorization", c.authorization)
	resp, err := c.client.Do(req)
	if err != nil {
		return errors.New("hosted registry deletion request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted || (resp.StatusCode == http.StatusNotFound && unknownManifest(resp.Body)) {
		return nil
	}
	return fmt.Errorf("hosted registry manifest deletion returned HTTP %d", resp.StatusCode)
}

func unknownManifest(body io.Reader) bool {
	const maxErrorBytes = 8192
	var response struct {
		Errors []struct{ Code string }
	}
	data, err := io.ReadAll(io.LimitReader(body, maxErrorBytes+1))
	if err != nil || len(data) > maxErrorBytes || json.Unmarshal(data, &response) != nil || len(response.Errors) != 1 {
		return false
	}
	code := response.Errors[0].Code
	return code == "MANIFEST_UNKNOWN" || code == "NAME_UNKNOWN"
}
