package kubernetes

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func TestSecretCertificateReadsExactObjectWithoutSerializingSecretData(t *testing.T) {
	clientset := kubernetesfake.NewClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "web-tls", Namespace: "team-a"}, Type: corev1.SecretTypeTLS, Data: map[string][]byte{"tls.crt": []byte("invalid certificate"), "tls.key": []byte("private-key-sentinel")}})
	client := &Client{typed: clientset}
	view, err := client.GetSecretCertificate(context.Background(), "team-a", "web-tls")
	if err != nil || view.Status != "invalid" || view.Name != "web-tls" || view.Namespace != "team-a" {
		t.Fatalf("view=%+v error=%v", view, err)
	}
	encoded, _ := json.Marshal(view)
	for _, value := range []string{"tls.key", "tls.crt", "private-key-sentinel", "invalid certificate"} {
		if strings.Contains(string(encoded), value) {
			t.Fatalf("response leaked %q", value)
		}
	}
	actions := clientset.Actions()
	if len(actions) != 1 || actions[0].GetVerb() != "get" || actions[0].GetNamespace() != "team-a" {
		t.Fatalf("unexpected reads: %v", actions)
	}
	_, err = client.GetSecretCertificate(context.Background(), "team-a", "absent")
	if !apierrors.IsNotFound(err) {
		t.Fatalf("missing secret = %v", err)
	}
	before := len(clientset.Actions())
	_, err = client.GetSecretCertificate(context.Background(), "", "web-tls")
	if !apierrors.IsBadRequest(err) || len(clientset.Actions()) != before {
		t.Fatal("empty namespace reached Kubernetes")
	}
}
