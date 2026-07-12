package resourcebackend

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

type directPortForwardSession struct {
	stopCh  chan struct{}
	doneCh  chan struct{}
	once    sync.Once
	errMu   sync.RWMutex
	lastErr string
}

func (d *Direct) StartPortForward(ctx context.Context, clusterID string, view domainresource.PortForwardSessionView) (appresource.DirectPortForwardSession, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	podName, targetPort, err := resolvePortForwardTarget(ctx, bundle, view.Namespace, view.TargetKind, view.TargetName, view.RemotePort)
	if err != nil {
		return nil, err
	}
	serverURL, err := buildPortForwardURL(bundle.RESTConfig.Host, view.Namespace, podName)
	if err != nil {
		return nil, err
	}
	roundTripper, upgrader, err := spdy.RoundTripperFor(bundle.RESTConfig)
	if err != nil {
		return nil, fmt.Errorf("build spdy transport: %w", err)
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, serverURL)
	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	doneCh := make(chan struct{})
	errBuf := &bytes.Buffer{}
	forwarder, err := portforward.NewOnAddresses(
		dialer, []string{"127.0.0.1"}, []string{fmt.Sprintf("%d:%d", view.LocalPort, targetPort)},
		stopCh, readyCh, bytes.NewBuffer(nil), errBuf,
	)
	if err != nil {
		return nil, fmt.Errorf("build port forwarder: %w", err)
	}
	session := &directPortForwardSession{stopCh: stopCh, doneCh: doneCh}
	go func() {
		defer close(doneCh)
		if err := forwarder.ForwardPorts(); err != nil {
			session.setError(err)
		}
	}()
	select {
	case <-readyCh:
		return session, nil
	case <-time.After(10 * time.Second):
		session.Stop()
		return nil, fmt.Errorf("port forward did not become ready within 10s (stderr: %s)", strings.TrimSpace(errBuf.String()))
	case <-ctx.Done():
		session.Stop()
		return nil, ctx.Err()
	}
}

func (s *directPortForwardSession) Stop() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		close(s.stopCh)
		select {
		case <-s.doneCh:
		case <-time.After(5 * time.Second):
		}
	})
}

func (s *directPortForwardSession) LastError() string {
	if s == nil {
		return ""
	}
	s.errMu.RLock()
	defer s.errMu.RUnlock()
	return s.lastErr
}

func (s *directPortForwardSession) setError(err error) {
	if err == nil {
		return
	}
	s.errMu.Lock()
	s.lastErr = err.Error()
	s.errMu.Unlock()
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
		targetPort := port
		for _, servicePort := range svc.Spec.Ports {
			if int(servicePort.Port) == port && servicePort.TargetPort.IntValue() > 0 {
				targetPort = servicePort.TargetPort.IntValue()
				break
			}
		}
		podList, err := bundle.Typed.CoreV1().Pods(namespace).List(queryCtx, metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(svc.Spec.Selector).String(),
		})
		if err != nil {
			return "", 0, fmt.Errorf("list pods: %w", err)
		}
		for _, pod := range podList.Items {
			if pod.Status.Phase == corev1.PodRunning && isPodReady(pod) {
				return pod.Name, targetPort, nil
			}
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
