package kubernetes

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"
)

const workerOperationAnnotation = "soha.io/worker-operation-id"

func (m *Manager) InspectWorkerCluster(ctx context.Context, spec sohaapi.VirtualizationWorkerPoolSpec) (domain.WorkerPoolIdentity, error) {
	identity := domain.WorkerPoolIdentity{DaemonSetUIDs: map[string]string{}}
	if err := domain.ValidateWorkerPoolSpec(spec); err != nil {
		return identity, err
	}
	bundle, err := m.Bundle(ctx, spec.ClusterID)
	if err != nil {
		return identity, err
	}
	if bundle.RESTConfig == nil || bundle.RESTConfig.Insecure {
		return identity, fmt.Errorf("%w: worker bootstrap requires verified direct Kubernetes TLS", apperrors.ErrInvalidArgument)
	}
	namespace, err := bundle.Typed.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
	if err != nil || namespace.UID == "" {
		return identity, fmt.Errorf("%w: target cluster identity is unavailable", apperrors.ErrConflict)
	}
	identity.ClusterUID = string(namespace.UID)
	config, err := bundle.Typed.CoreV1().ConfigMaps("kube-system").Get(ctx, "kubeadm-config", metav1.GetOptions{})
	if err != nil {
		return identity, fmt.Errorf("%w: target must be a configured kubeadm cluster", apperrors.ErrConflict)
	}
	var clusterConfig struct{ Kind, KubernetesVersion string }
	if err := yaml.Unmarshal([]byte(config.Data["ClusterConfiguration"]), &clusterConfig); err != nil {
		return identity, fmt.Errorf("%w: invalid kubeadm cluster configuration", apperrors.ErrConflict)
	}
	if clusterConfig.Kind != "ClusterConfiguration" || clusterConfig.KubernetesVersion != spec.KubernetesVersion {
		return identity, fmt.Errorf("%w: kubeadm and worker image versions must match", apperrors.ErrConflict)
	}
	discovery, err := requestDiscoveryClient(ctx, bundle.RESTConfig)
	if err != nil {
		return identity, err
	}
	version, err := discovery.ServerVersion()
	if err != nil || version.GitVersion != spec.KubernetesVersion {
		return identity, fmt.Errorf("%w: live API server and worker image versions must match", apperrors.ErrConflict)
	}
	info, err := bundle.Typed.CoreV1().ConfigMaps("kube-public").Get(ctx, "cluster-info", metav1.GetOptions{})
	if err != nil {
		return identity, fmt.Errorf("%w: kubeadm cluster discovery is unavailable", apperrors.ErrConflict)
	}
	identity.APIServerEndpoint, identity.CAHash, err = workerDiscoveryIdentity(info.Data["kubeconfig"], bundle.RESTConfig)
	if err != nil {
		return identity, err
	}
	for _, ref := range spec.RequiredDaemonSets {
		daemon, err := bundle.Typed.AppsV1().DaemonSets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil || daemon.UID == "" || daemon.DeletionTimestamp != nil {
			return identity, fmt.Errorf("%w: required node daemon is unavailable", apperrors.ErrConflict)
		}
		identity.DaemonSetUIDs[ref.Namespace+"/"+ref.Name] = string(daemon.UID)
	}
	return identity, nil
}

func workerDiscoveryIdentity(kubeconfig string, config *rest.Config) (string, string, error) {
	invalid := fmt.Errorf("%w: worker discovery endpoint or pinned cluster CA is invalid", apperrors.ErrConflict)
	parsed, err := clientcmd.Load([]byte(kubeconfig))
	if err != nil || len(parsed.Clusters) != 1 {
		return "", "", invalid
	}
	trusted := rest.CopyConfig(config)
	if rest.LoadTLSFiles(trusted) != nil || len(trusted.CAData) == 0 {
		return "", "", invalid
	}
	for _, cluster := range parsed.Clusters {
		endpoint, err := url.Parse(cluster.Server)
		if err != nil || endpoint.Scheme != "https" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "" && endpoint.Path != "/" || endpoint.Hostname() == "" || cluster.InsecureSkipTLSVerify {
			return "", "", invalid
		}
		hash, err := workerCAHash(cluster.CertificateAuthorityData, trusted.CAData)
		if err != nil {
			return "", "", invalid
		}
		port := endpoint.Port()
		if port == "" {
			port = "443"
		}
		return net.JoinHostPort(endpoint.Hostname(), port), hash, nil
	}
	return "", "", invalid
}

func workerCAHash(ca, trusted []byte) (string, error) {
	invalid := fmt.Errorf("invalid pinned worker CA")
	block, remainder := pem.Decode(ca)
	if block == nil || len(strings.TrimSpace(string(remainder))) != 0 {
		return "", invalid
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil || !certificate.IsCA {
		return "", invalid
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(trusted) {
		return "", invalid
	}
	if _, err := certificate.Verify(x509.VerifyOptions{Roots: roots}); err != nil {
		return "", invalid
	}
	hash := sha256.Sum256(certificate.RawSubjectPublicKeyInfo)
	return "sha256:" + hex.EncodeToString(hash[:]), nil
}

func (m *Manager) checkWorkerCluster(ctx context.Context, pool domain.WorkerPool) (*Bundle, error) {
	current, err := m.InspectWorkerCluster(ctx, pool.Spec)
	if err != nil {
		return nil, err
	}
	frozen := pool.Identity
	if current.ClusterUID != frozen.ClusterUID || current.APIServerEndpoint != frozen.APIServerEndpoint || current.CAHash != frozen.CAHash || !reflect.DeepEqual(current.DaemonSetUIDs, frozen.DaemonSetUIDs) {
		return nil, fmt.Errorf("%w: target cluster or node daemon identity changed", apperrors.ErrConflict)
	}
	return m.Bundle(ctx, pool.Spec.ClusterID)
}

func workerBootstrapSecretName(operationID string) string {
	hash := sha256.Sum256([]byte(operationID))
	return "bootstrap-token-" + hex.EncodeToString(hash[:3])
}

func (m *Manager) PrepareWorkerBootstrap(ctx context.Context, pool domain.WorkerPool, operationID, nodeName string, deadline time.Time) (domain.WorkerBootstrap, error) {
	var result domain.WorkerBootstrap
	if _, err := uuid.Parse(operationID); err != nil || nodeName != domain.WorkerNodeName(operationID) || !deadline.After(time.Now()) {
		return result, apperrors.ErrInvalidArgument
	}
	bundle, err := m.checkWorkerCluster(ctx, pool)
	if err != nil {
		return result, err
	}
	secrets := bundle.Typed.CoreV1().Secrets("kube-system")
	name := workerBootstrapSecretName(operationID)
	secret, err := secrets.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		value := make([]byte, 8)
		if _, err := rand.Read(value); err != nil {
			return result, err
		}
		expires := minTime(deadline, time.Now().Add(15*time.Minute))
		secret = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kube-system", Annotations: map[string]string{workerOperationAnnotation: operationID}}, Type: "bootstrap.kubernetes.io/token", Data: map[string][]byte{
			"token-id": []byte(strings.TrimPrefix(name, "bootstrap-token-")), "token-secret": []byte(hex.EncodeToString(value)),
			"expiration": []byte(expires.UTC().Format(time.RFC3339)), "usage-bootstrap-authentication": []byte("true"), "usage-bootstrap-signing": []byte("true"),
			"auth-extra-groups": []byte("system:bootstrappers:kubeadm:default-node-token"),
		}}
		secret, err = secrets.Create(ctx, secret, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			secret, err = secrets.Get(ctx, name, metav1.GetOptions{})
		}
	}
	if err != nil {
		return result, fmt.Errorf("%w: worker bootstrap secret could not be prepared", apperrors.ErrConflict)
	}
	if err := validateWorkerBootstrapSecret(secret, operationID); err != nil {
		return result, err
	}
	expires, _ := time.Parse(time.RFC3339, string(secret.Data["expiration"]))
	content, err := workerCloudInit(pool, nodeName, string(secret.Data["token-id"])+"."+string(secret.Data["token-secret"]))
	return domain.WorkerBootstrap{CloudInit: content, SecretName: name, SecretUID: string(secret.UID), ExpiresAt: expires}, err
}

func validateWorkerBootstrapSecret(secret *corev1.Secret, operationID string) error {
	expires, err := time.Parse(time.RFC3339, string(secret.Data["expiration"]))
	value, decodeErr := hex.DecodeString(string(secret.Data["token-secret"]))
	if secret.UID == "" || secret.Type != "bootstrap.kubernetes.io/token" || secret.Annotations[workerOperationAnnotation] != operationID || secret.Name != workerBootstrapSecretName(operationID) ||
		string(secret.Data["token-id"]) != strings.TrimPrefix(secret.Name, "bootstrap-token-") || decodeErr != nil || len(value) != 8 || err != nil || !expires.After(time.Now()) || expires.After(time.Now().Add(15*time.Minute+time.Second)) ||
		string(secret.Data["usage-bootstrap-authentication"]) != "true" || string(secret.Data["usage-bootstrap-signing"]) != "true" || string(secret.Data["auth-extra-groups"]) != "system:bootstrappers:kubeadm:default-node-token" {
		return fmt.Errorf("%w: original worker bootstrap credential is absent, changed or expired", apperrors.ErrConflict)
	}
	return nil
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func workerCloudInit(pool domain.WorkerPool, nodeName, token string) (string, error) {
	join, err := json.Marshal(map[string]any{
		"apiVersion": "kubeadm.k8s.io/v1beta4", "kind": "JoinConfiguration",
		"discovery":        map[string]any{"bootstrapToken": map[string]any{"apiServerEndpoint": pool.Identity.APIServerEndpoint, "token": token, "caCertHashes": []string{pool.Identity.CAHash}}, "tlsBootstrapToken": token},
		"nodeRegistration": map[string]any{"name": nodeName, "criSocket": "unix:///var/run/containerd/containerd.sock"},
	})
	if err != nil {
		return "", err
	}
	// Image preparation is operator-owned. Do not download packages or run model-supplied shell.
	script := fmt.Sprintf(`set -eu
umask 077
trap 'rm -f /run/soha-worker-join.json' EXIT
. /etc/os-release
[ "$ID" = ubuntu ] && [ "$VERSION_ID" = 24.04 ]
[ "$(uname -m)" = x86_64 ]
[ "$(kubeadm version -o short)" = %s ]
[ "$(kubelet --version)" = 'Kubernetes %s' ]
systemctl is-active --quiet containerd
timeout 900 kubeadm join --config /run/soha-worker-join.json > /var/log/soha-worker-join.log 2>&1
`, pool.Spec.KubernetesVersion, pool.Spec.KubernetesVersion)
	content, err := yaml.Marshal(map[string]any{"hostname": nodeName, "preserve_hostname": false, "write_files": []any{map[string]any{"path": "/run/soha-worker-join.json", "permissions": "0600", "owner": "root:root", "content": string(join)}}, "runcmd": []any{[]string{"/bin/sh", "-c", script}}})
	return "#cloud-config\n" + string(content), err
}

func (m *Manager) RevokeWorkerBootstrap(ctx context.Context, pool domain.WorkerPool, operationID, secretUID string) error {
	bundle, err := m.Bundle(ctx, pool.Spec.ClusterID)
	if err != nil {
		return err
	}
	// Revocation depends only on the original cluster, not daemon rollouts or
	// Kubernetes upgrades that may have blocked the worker's readiness check.
	if err := workerRevocationTarget(ctx, bundle, pool.Identity); err != nil {
		return err
	}
	secrets := bundle.Typed.CoreV1().Secrets("kube-system")
	secret, err := secrets.Get(ctx, workerBootstrapSecretName(operationID), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: worker bootstrap revocation could not be verified", apperrors.ErrConflict)
	}
	if secret.UID == "" || secretUID != "" && string(secret.UID) != secretUID || secret.Annotations[workerOperationAnnotation] != operationID || secret.Type != "bootstrap.kubernetes.io/token" || string(secret.Data["token-id"]) != strings.TrimPrefix(secret.Name, "bootstrap-token-") {
		return fmt.Errorf("%w: worker bootstrap identity changed before revocation", apperrors.ErrConflict)
	}
	// A crash may occur after Secret creation but before its UID checkpoint.
	// Recover only the deterministic name carrying this operation's ownership,
	// then still use UID and resourceVersion preconditions on deletion.
	uid, version := secret.UID, secret.ResourceVersion
	err = secrets.Delete(ctx, secret.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func workerRevocationTarget(ctx context.Context, bundle *Bundle, identity domain.WorkerPoolIdentity) error {
	invalid := fmt.Errorf("%w: original cluster identity is required for bootstrap revocation", apperrors.ErrConflict)
	if bundle.RESTConfig == nil || bundle.RESTConfig.Insecure {
		return invalid
	}
	namespace, err := bundle.Typed.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
	if err != nil || string(namespace.UID) != identity.ClusterUID {
		return invalid
	}
	info, err := bundle.Typed.CoreV1().ConfigMaps("kube-public").Get(ctx, "cluster-info", metav1.GetOptions{})
	if err != nil {
		return invalid
	}
	endpoint, hash, err := workerDiscoveryIdentity(info.Data["kubeconfig"], bundle.RESTConfig)
	if err != nil || endpoint != identity.APIServerEndpoint || hash != identity.CAHash {
		return invalid
	}
	return nil
}
