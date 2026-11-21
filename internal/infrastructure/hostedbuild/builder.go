// Package hostedbuild submits platform-owned application builds to an isolated BuildKit pool.
package hostedbuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/distribution/reference"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

const (
	managedByLabel  = "app.kubernetes.io/managed-by"
	managedBy       = "soha-hosted-build"
	revisionLabel   = "apphosting.soha.dev/revision-id"
	inputAnnotation = "apphosting.soha.dev/build-input"
	repoAnnotation  = "apphosting.soha.dev/registry-repository"
	buildContainer  = "build"
	sourceKey       = "source-url"
	requestTimeout  = 15 * time.Second
)

var (
	revisionPattern = regexp.MustCompile(`^rev_[0-9a-f]{32}$`)
	digestPattern   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

	// ErrBuildFailed indicates that a build ended without a trustworthy image digest.
	ErrBuildFailed = errors.New("hosted build failed")
)

// Config binds the adapter to administrator-provisioned build resources.
type Config struct {
	Namespace          string
	BuildClientImage   string
	NodeBase           string
	PythonBase         string
	RegistryRepository string
	DockerConfigSecret string
	BuildKitAddress    string
	BuildKitTLSSecret  string
	SourceOrigin       string
	RuntimeClassName   string
}

// Spec represents one immutable revision and a short-lived URL to its validated source.
type Spec struct {
	RevisionID string
	Template   string
	SourceURL  string
}

type Builder struct {
	client     kubernetes.Interface
	cfg        Config
	sourceHost string
}

func New(client kubernetes.Interface, cfg Config) (*Builder, error) {
	if client == nil || (reflect.ValueOf(client).Kind() == reflect.Pointer && reflect.ValueOf(client).IsNil()) {
		return nil, errors.New("hosted build Kubernetes client is required")
	}
	if len(validation.IsDNS1123Label(cfg.Namespace)) > 0 {
		return nil, errors.New("invalid hosted build namespace")
	}
	if !validSecretNames(cfg) {
		return nil, errors.New("invalid hosted build secret names")
	}
	if cfg.RuntimeClassName != "" && len(validation.IsDNS1123Subdomain(cfg.RuntimeClassName)) > 0 {
		return nil, errors.New("invalid hosted build runtime class")
	}
	if !pinnedImage(cfg.BuildClientImage) || !pinnedImage(cfg.NodeBase) || !pinnedImage(cfg.PythonBase) {
		return nil, errors.New("hosted build images must have a fully qualified repository and pinned SHA-256 digest")
	}
	repo, err := reference.ParseNamed(cfg.RegistryRepository)
	if err != nil || reference.Domain(repo) == "" || !reference.IsNameOnly(repo) {
		return nil, errors.New("invalid hosted build registry repository")
	}
	if !validBuildKitAddress(cfg.BuildKitAddress) {
		return nil, errors.New("hosted build requires a TCP BuildKit address")
	}
	host := sourceOriginHost(cfg.SourceOrigin)
	if host == "" {
		return nil, errors.New("hosted build requires an HTTPS source origin")
	}
	return &Builder{client: client, cfg: cfg, sourceHost: host}, nil
}

func validSecretNames(cfg Config) bool {
	for _, name := range []string{cfg.DockerConfigSecret, cfg.BuildKitTLSSecret} {
		if len(validation.IsDNS1123Subdomain(name)) > 0 || strings.HasPrefix(name, managedBy+"-") {
			return false
		}
	}
	return cfg.DockerConfigSecret != cfg.BuildKitTLSSecret
}

func validBuildKitAddress(address string) bool {
	value, ok := strings.CutPrefix(address, "tcp://")
	if !ok {
		return false
	}
	host, service, err := net.SplitHostPort(value)
	if err != nil || (net.ParseIP(host) == nil && len(validation.IsDNS1123Subdomain(host)) > 0) {
		return false
	}
	port, err := strconv.ParseUint(service, 10, 16)
	return err == nil && port > 0
}

func pinnedImage(value string) bool {
	img, err := reference.ParseNamed(value)
	if err != nil || reference.Domain(img) == "" {
		return false
	}
	digested, ok := img.(reference.Digested)
	return ok && digestPattern.MatchString(digested.Digest().String())
}

func validHTTPS(value *url.URL) bool {
	return value.Scheme == "https" && value.Hostname() != "" && value.User == nil && value.Fragment == "" && !strings.HasSuffix(value.Host, ":")
}

func sourceOriginHost(value string) string {
	origin, err := url.Parse(value)
	if err != nil || !validHTTPS(origin) || (origin.Path != "" && origin.EscapedPath() != "/") || origin.RawQuery != "" || origin.ForceQuery {
		return ""
	}
	return origin.Host
}

// Prepare submits a new Job or resumes preparation after an interrupted request.
func (b *Builder) Prepare(ctx context.Context, spec Spec) error {
	if !revisionPattern.MatchString(spec.RevisionID) {
		return errors.New("invalid hosted build revision ID")
	}
	dockerfile, err := b.dockerfile(spec.Template)
	if err != nil {
		return err
	}
	source, err := url.Parse(spec.SourceURL)
	if err != nil || len(spec.SourceURL) > 8192 || !validHTTPS(source) || !strings.EqualFold(source.Host, b.sourceHost) || source.Path == "" || source.RawQuery == "" || strings.Contains(spec.SourceURL, "#") {
		return errors.New("invalid hosted build source URL")
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	expected := b.job(spec.RevisionID, spec.Template, dockerfile, source.EscapedPath())
	jobs := b.client.BatchV1().Jobs(b.cfg.Namespace)
	_, err = jobs.Create(ctx, expected, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create hosted build Job: %w", err)
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		job, err := jobs.Get(ctx, expected.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("read hosted build Job: %w", err)
		}
		if !b.ownsJob(job, spec.RevisionID) || job.DeletionTimestamp != nil || job.Annotations[inputAnnotation] != expected.Annotations[inputAnnotation] {
			return errors.New("hosted build Job identity or inputs changed")
		}
		if job.Spec.Suspend == nil || !*job.Spec.Suspend {
			return nil
		}
		if err := b.ensureSource(ctx, job, spec.SourceURL); err != nil {
			return err
		}
		job.Spec.Suspend = new(false)
		_, err = jobs.Update(ctx, job, metav1.UpdateOptions{})
		return err
	})
}

func (b *Builder) ensureSource(ctx context.Context, job *batchv1.Job, source string) error {
	secrets := b.client.CoreV1().Secrets(b.cfg.Namespace)
	item := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            job.Name + "-source",
			Namespace:       b.cfg.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(job, batchv1.SchemeGroupVersion.WithKind("Job"))},
		},
		Immutable: new(true),
		Data:      map[string][]byte{sourceKey: []byte(source)},
	}
	_, err := secrets.Create(ctx, item, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		// Admission errors may quote fields from the submitted Secret.
		return errors.New("create hosted build source credential failed")
	}
	existing, err := secrets.Get(ctx, item.Name, metav1.GetOptions{})
	if err != nil {
		return errors.New("read hosted build source credential failed")
	}
	if !controlledBy(existing, job) || existing.DeletionTimestamp != nil || existing.Immutable == nil || !*existing.Immutable || len(existing.Data[sourceKey]) == 0 {
		return errors.New("hosted build source credential has an unexpected owner or state")
	}
	return nil
}

// Result returns the approved repository at the digest emitted by the successful build.
func (b *Builder) Result(ctx context.Context, revisionID string) (string, bool, error) {
	if !revisionPattern.MatchString(revisionID) {
		return "", false, errors.New("invalid hosted build revision ID")
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	job, err := b.client.BatchV1().Jobs(b.cfg.Namespace).Get(ctx, jobName(revisionID), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read hosted build Job: %w", err)
	}
	if !b.ownsJob(job, revisionID) || job.DeletionTimestamp != nil || job.Annotations[repoAnnotation] != b.cfg.RegistryRepository {
		return "", false, errors.New("hosted build Job identity or registry changed")
	}
	if jobCondition(job, batchv1.JobFailed) {
		return "", false, fmt.Errorf("%w: Job did not complete", ErrBuildFailed)
	}
	if !jobCondition(job, batchv1.JobComplete) {
		return "", false, nil
	}
	digest, err := b.resultDigest(ctx, job)
	if err != nil {
		return "", false, err
	}
	return b.cfg.RegistryRepository + "@" + digest, true, nil
}

func (b *Builder) resultDigest(ctx context.Context, job *batchv1.Job) (string, error) {
	pods, err := b.client.CoreV1().Pods(b.cfg.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set{batchv1.ControllerUidLabel: string(job.UID)}.String(),
	})
	if err != nil {
		return "", fmt.Errorf("list hosted build Job Pods: %w", err)
	}
	digest := ""
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !controlledBy(pod, job) || pod.Status.Phase != corev1.PodSucceeded {
			continue
		}
		if len(pod.Spec.Containers) != 1 || pod.Spec.Containers[0].Name != buildContainer || pod.Spec.Containers[0].Image != b.cfg.BuildClientImage {
			return "", fmt.Errorf("%w: unexpected build container", ErrBuildFailed)
		}
		emitted := podDigest(pod)
		if !digestPattern.MatchString(emitted) || (digest != "" && digest != emitted) {
			return "", fmt.Errorf("%w: invalid or ambiguous image digest", ErrBuildFailed)
		}
		digest = emitted
	}
	if digest == "" {
		return "", fmt.Errorf("%w: missing image digest", ErrBuildFailed)
	}
	return digest, nil
}

// Delete only removes the owned Job and its Kubernetes-owned dependents.
func (b *Builder) Delete(ctx context.Context, revisionID string) error {
	if !revisionPattern.MatchString(revisionID) {
		return errors.New("invalid hosted build revision ID")
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	jobs := b.client.BatchV1().Jobs(b.cfg.Namespace)
	job, err := jobs.Get(ctx, jobName(revisionID), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read hosted build Job for deletion: %w", err)
	}
	if !b.ownsJob(job, revisionID) {
		return errors.New("refusing to delete an unowned hosted build Job")
	}
	err = jobs.Delete(ctx, job.Name, metav1.DeleteOptions{
		Preconditions:     &metav1.Preconditions{UID: &job.UID},
		PropagationPolicy: new(metav1.DeletePropagationForeground),
	})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func jobName(revisionID string) string {
	return managedBy + "-" + strings.TrimPrefix(revisionID, "rev_")
}

func (b *Builder) ownsJob(job *batchv1.Job, revisionID string) bool {
	return job.Namespace == b.cfg.Namespace && job.Name == jobName(revisionID) && job.UID != "" &&
		job.Labels[managedByLabel] == managedBy && job.Labels[revisionLabel] == revisionID
}

func controlledBy(item metav1.Object, job *batchv1.Job) bool {
	owner := metav1.GetControllerOf(item)
	return item.GetNamespace() == job.Namespace && owner != nil && owner.APIVersion == batchv1.SchemeGroupVersion.String() &&
		owner.Kind == "Job" && owner.Name == job.Name && owner.UID == job.UID
}

func jobCondition(job *batchv1.Job, kind batchv1.JobConditionType) bool {
	for _, state := range job.Status.Conditions {
		if state.Type == kind && state.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func podDigest(pod *corev1.Pod) string {
	for _, status := range pod.Status.ContainerStatuses {
		if state := status.State.Terminated; status.Name == buildContainer && state != nil && state.ExitCode == 0 {
			return state.Message
		}
	}
	return ""
}

func buildInputHash(config Config, template, dockerfile, sourcePath string) string {
	inputs := []string{fmt.Sprintf("%#v", config), template, dockerfile, sourcePath, sourceScript, buildScript}
	sum := sha256.Sum256([]byte(strings.Join(inputs, "\x00")))
	return hex.EncodeToString(sum[:])
}
