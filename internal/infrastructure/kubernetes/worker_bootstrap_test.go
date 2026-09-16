package kubernetes

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/yaml"
)

func workerFixture(t *testing.T) (*Manager, *fake.Clientset, domain.WorkerPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"gitVersion":"v1.35.2"}`))
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.StartTLS()
	t.Cleanup(server.Close)
	kubeconfig, err := clientcmd.Write(clientcmdapi.Config{Clusters: map[string]*clientcmdapi.Cluster{"cluster": {Server: server.URL, CertificateAuthorityData: ca}}})
	if err != nil {
		t.Fatal(err)
	}
	client := fake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system", UID: "cluster-uid"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "kubeadm-config", Namespace: "kube-system"}, Data: map[string]string{"ClusterConfiguration": "kind: ClusterConfiguration\nkubernetesVersion: v1.35.2\n"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cluster-info", Namespace: "kube-public"}, Data: map[string]string{"kubeconfig": string(kubeconfig)}},
		&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "kube-system", UID: "daemon-uid", Generation: 1}, Spec: appsv1.DaemonSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "network"}}}, Status: appsv1.DaemonSetStatus{ObservedGeneration: 1, DesiredNumberScheduled: 1, UpdatedNumberScheduled: 1}},
	)
	client.PrependReactor("create", "secrets", func(action clienttesting.Action) (bool, runtime.Object, error) {
		createAction, ok := action.(clienttesting.CreateAction)
		if !ok {
			t.Fatalf("action type = %T, want clienttesting.CreateAction", action)
		}
		secret, ok := createAction.GetObject().(*corev1.Secret)
		if !ok {
			t.Fatalf("secret type = %T, want *corev1.Secret", createAction.GetObject())
		}
		secret.UID = types.UID(uuid.NewString())
		return false, nil, nil
	})
	manager := NewManager(nil)
	manager.bundles["target"] = &Bundle{Typed: client, RESTConfig: &rest.Config{Host: server.URL, TLSClientConfig: rest.TLSClientConfig{CAData: ca}}}
	pool := domain.WorkerPool{VirtualizationWorkerPool: sohaapi.VirtualizationWorkerPool{ID: uuid.New(), Revision: 1, Spec: sohaapi.VirtualizationWorkerPoolSpec{Name: "workers", ConnectionID: "pve", ClusterID: "target", Owner: "soha-kubeadm", ImageID: "image", ProviderNode: "pve-a", Storage: "disk", Bridge: "vmbr0", SnippetStorage: "local", OsProfile: "ubuntu-24.04-amd64-containerd", KubernetesVersion: "v1.35.2", CPU: 4, MemoryMiB: 8192, DiskGiB: 80, MaxNodes: 2, Enabled: true, RequiredDaemonSets: []sohaapi.VirtualizationWorkerDaemonSet{{Namespace: "kube-system", Name: "network"}}}}}
	pool.Identity, err = manager.InspectWorkerCluster(context.Background(), pool.Spec)
	if err != nil {
		t.Fatal(err)
	}
	return manager, client, pool
}

func TestWorkerBootstrapSurvivesManagerRebuildAndRevokesOriginalSecret(t *testing.T) {
	manager, client, pool := workerFixture(t)
	ctx := context.Background()
	id := uuid.NewString()
	first, err := manager.PrepareWorkerBootstrap(ctx, pool, id, domain.WorkerNodeName(id), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if first.SecretUID == "" || !first.ExpiresAt.After(time.Now()) || first.ExpiresAt.After(time.Now().Add(15*time.Minute)) {
		t.Fatal("bootstrap did not get bounded persisted identity")
	}
	rebuilt := NewManager(nil)
	rebuilt.bundles = manager.bundles
	replayed, err := rebuilt.PrepareWorkerBootstrap(ctx, pool, id, domain.WorkerNodeName(id), time.Now().Add(time.Hour))
	if err != nil || replayed != first {
		t.Fatalf("original bootstrap was not reused: %v", err)
	}
	var config struct {
		WriteFiles []struct{ Path, Permissions, Content string } `json:"write_files"`
		RunCmd     [][]string                                    `json:"runcmd"`
	}
	if err := yaml.Unmarshal([]byte(first.CloudInit), &config); err != nil {
		t.Fatal(err)
	}
	secret, _ := client.CoreV1().Secrets("kube-system").Get(ctx, first.SecretName, metav1.GetOptions{})
	token := string(secret.Data["token-secret"])
	if len(config.WriteFiles) != 1 || config.WriteFiles[0].Permissions != "0600" || !strings.Contains(config.WriteFiles[0].Content, pool.Identity.CAHash) || !strings.Contains(config.WriteFiles[0].Content, token) || strings.Contains(config.RunCmd[0][2], token) || strings.Contains(config.WriteFiles[0].Content, "unsafeSkipCA") || strings.Contains(config.WriteFiles[0].Content, "controlPlane") {
		t.Fatal("bootstrap credential escaped its pinned worker configuration file")
	}
	if err := rebuilt.RevokeWorkerBootstrap(ctx, pool, id, "different-uid"); err == nil {
		t.Fatal("revoked a different credential")
	}
	if err := rebuilt.RevokeWorkerBootstrap(ctx, pool, id, first.SecretUID); err != nil {
		t.Fatal(err)
	}
	list, _ := client.CoreV1().Secrets("kube-system").List(ctx, metav1.ListOptions{})
	if len(list.Items) != 0 {
		t.Fatal("original bootstrap secret survived revocation")
	}
}

func TestWorkerBootstrapRefusesExpiredOrForeignCredentialAndChangedCluster(t *testing.T) {
	for _, scenario := range []string{"expired", "foreign", "cluster", "version", "untrusted-ca"} {
		t.Run(scenario, func(t *testing.T) {
			manager, client, pool := workerFixture(t)
			ctx := context.Background()
			id := uuid.NewString()
			first, err := manager.PrepareWorkerBootstrap(ctx, pool, id, domain.WorkerNodeName(id), time.Now().Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			secret, _ := client.CoreV1().Secrets("kube-system").Get(ctx, first.SecretName, metav1.GetOptions{})
			switch scenario {
			case "expired":
				secret.Data["expiration"] = []byte(time.Now().Add(-time.Minute).Format(time.RFC3339))
			case "foreign":
				secret.Annotations[workerOperationAnnotation] = uuid.NewString()
			case "cluster":
				pool.Identity.ClusterUID = "another-cluster"
			case "version":
				pool.Spec.KubernetesVersion = "v1.34.1"
			case "untrusted-ca":
				manager.bundles["target"].RESTConfig.CAData = []byte("invalid")
			}
			if _, err := client.CoreV1().Secrets("kube-system").Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			client.ClearActions()
			if _, err := manager.PrepareWorkerBootstrap(ctx, pool, id, domain.WorkerNodeName(id), time.Now().Add(time.Hour)); err == nil {
				t.Fatal("unsafe bootstrap accepted")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() == "create" || action.GetVerb() == "update" || action.GetVerb() == "delete" {
					t.Fatalf("rejected bootstrap changed the cluster: %s", action.GetVerb())
				}
			}
		})
	}
}
