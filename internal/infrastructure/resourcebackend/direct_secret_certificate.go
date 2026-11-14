package resourcebackend

import (
	"context"

	contractruntime "github.com/opensoha/soha/contracts/resource/runtime"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (d *Direct) GetSecretCertificate(ctx context.Context, clusterID, namespace, name string) (domainresource.SecretCertificateView, error) {
	bundle, queryCtx, cancel, err := d.configurationQuery(ctx, clusterID)
	if err != nil {
		return domainresource.SecretCertificateView{}, err
	}
	defer cancel()
	item, err := bundle.Typed.CoreV1().Secrets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.SecretCertificateView{}, err
	}
	return contractruntime.SecretCertificateMetadata(item.Name, item.Namespace, string(item.Type), item.Data[corev1.TLSCertKey]), nil
}
