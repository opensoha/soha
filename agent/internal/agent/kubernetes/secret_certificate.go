package kubernetes

import (
	"context"
	"strings"
	"time"

	domainresource "github.com/opensoha/soha/agent/internal/domain/resource"
	contractruntime "github.com/opensoha/soha/contracts/resource/runtime"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Client) GetSecretCertificate(ctx context.Context, namespace, name string) (domainresource.SecretCertificateView, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" {
		return domainresource.SecretCertificateView{}, apierrors.NewBadRequest("namespace and name are required")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.SecretCertificateView{}, err
	}
	return contractruntime.SecretCertificateMetadata(item.Name, item.Namespace, string(item.Type), item.Data[corev1.TLSCertKey]), nil
}
