package resourcebackend

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/portforward"
)

func (d *Direct) StartPortForward(ctx context.Context, clusterID string, view domainresource.PortForwardSessionView) (appresource.DirectPortForwardSession, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return startDirectPortForward(ctx, func(attemptCtx context.Context, readyCh chan struct{}) (portForwardRunner, error) {
		// Resolve Services again after a disconnect so a replacement ready Pod can be selected.
		podName, targetPort, err := resolvePortForwardTarget(attemptCtx, bundle, view.Namespace, view.TargetKind, view.TargetName, view.RemotePort)
		if err != nil {
			return nil, err
		}
		serverURL, err := buildPortForwardURL(bundle.RESTConfig.Host, view.Namespace, podName)
		if err != nil {
			return nil, err
		}
		dialer, err := newDirectPortForwardDialer(attemptCtx, bundle.RESTConfig, serverURL)
		if err != nil {
			return nil, err
		}
		return portforward.NewOnAddresses(
			dialer, []string{"127.0.0.1"}, []string{fmt.Sprintf("%d:%d", view.LocalPort, targetPort)},
			attemptCtx.Done(), readyCh, io.Discard, io.Discard,
		)
	})
}

func buildPortForwardURL(restHost, namespace, podName string) (*url.URL, error) {
	parsed, err := url.Parse(restHost)
	if err != nil || parsed.Host == "" {
		parsed = &url.URL{Scheme: "https", Host: restHost}
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	parsed.Path = fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, podName)
	return parsed, nil
}

func resolvePortForwardTarget(ctx context.Context, bundle *k8sinfra.Bundle, namespace, kind, name string, port int) (string, int, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	switch strings.ToLower(kind) {
	case "service":
		svc, err := bundle.Typed.CoreV1().Services(namespace).Get(queryCtx, name, metav1.GetOptions{})
		if err != nil {
			return "", 0, fmt.Errorf("get service: %w", err)
		}
		if len(svc.Spec.Selector) == 0 {
			return "", 0, fmt.Errorf("%w: service %s has no pod selector", apperrors.ErrInvalidArgument, name)
		}
		var servicePort *corev1.ServicePort
		for i := range svc.Spec.Ports {
			if int(svc.Spec.Ports[i].Port) == port && (svc.Spec.Ports[i].Protocol == "" || svc.Spec.Ports[i].Protocol == corev1.ProtocolTCP) {
				servicePort = &svc.Spec.Ports[i]
				break
			}
		}
		if servicePort == nil {
			return "", 0, fmt.Errorf("service %s does not declare TCP port %d", name, port)
		}
		podList, err := bundle.Typed.CoreV1().Pods(namespace).List(queryCtx, metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(svc.Spec.Selector).String(),
		})
		if err != nil {
			return "", 0, fmt.Errorf("list pods: %w", err)
		}
		var targetError error
		for _, pod := range podList.Items {
			if pod.Status.Phase == corev1.PodRunning && isPodReady(pod) {
				targetPort, err := resolveTCPServiceTargetPort(svc, *servicePort, &pod)
				if err != nil {
					targetError = err
					continue
				}
				return pod.Name, int(targetPort), nil
			}
		}
		if targetError != nil {
			return "", 0, fmt.Errorf("resolve service target port: %w", targetError)
		}
		return "", 0, fmt.Errorf("%w: no ready pod found for service %s", apperrors.ErrNotFound, name)
	case "pod":
		if _, err := bundle.Typed.CoreV1().Pods(namespace).Get(queryCtx, name, metav1.GetOptions{}); err != nil {
			return "", 0, fmt.Errorf("get pod: %w", err)
		}
		return name, port, nil
	default:
		return "", 0, fmt.Errorf("%w: unsupported target kind %s", apperrors.ErrInvalidArgument, kind)
	}
}

func resolveTCPServiceTargetPort(service *corev1.Service, port corev1.ServicePort, pod *corev1.Pod) (int32, error) {
	if service.Spec.ClusterIP == corev1.ClusterIPNone {
		return port.Port, nil
	}
	if port.TargetPort.Type == intstr.Int {
		if port.TargetPort.IntVal == 0 {
			return port.Port, nil
		}
		return port.TargetPort.IntVal, nil
	}
	name := port.TargetPort.StrVal
	for _, containers := range [][]corev1.Container{pod.Spec.Containers, pod.Spec.InitContainers} {
		for _, container := range containers {
			for _, candidate := range container.Ports {
				if candidate.Name == name && (candidate.Protocol == "" || candidate.Protocol == corev1.ProtocolTCP) {
					return candidate.ContainerPort, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("pod %s has no TCP container port named %s", pod.Name, name)
}

func isPodReady(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

var (
	_ appresource.DirectPortForwardStarter = (*Direct)(nil)
	_ appresource.DirectPortForwardSession = (*directPortForwardSession)(nil)
)
