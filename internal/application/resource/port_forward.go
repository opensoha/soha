package resource

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
	portforwardrepo "github.com/opensoha/soha/internal/repository/portforward"
)

type portForwardSession struct {
	view    domainresource.PortForwardSessionView
	stopCh  chan struct{}
	doneCh  chan struct{}
	lastErr string
	once    sync.Once
	cancel  context.CancelFunc
}

type portForwardTunnelClient interface {
	StreamPortForward(context.Context, string, net.Conn) error
}

var (
	portForwardRegistryMu sync.Mutex
	portForwardRegistry   = map[string]*portForwardSession{}
)

func toSessionView(rec portforwardrepo.Record) domainresource.PortForwardSessionView {
	return domainresource.PortForwardSessionView{
		SessionID:  rec.SessionID,
		ClusterID:  rec.ClusterID,
		Namespace:  rec.Namespace,
		TargetKind: rec.TargetKind,
		TargetName: rec.TargetName,
		LocalPort:  rec.LocalPort,
		RemotePort: rec.RemotePort,
		Status:     rec.Status,
		CreatedBy:  rec.CreatedBy,
		CreatedAt:  rec.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func fromSessionView(view domainresource.PortForwardSessionView, connectionMode, lastErr string) portforwardrepo.Record {
	createdAt, err := time.Parse(time.RFC3339, view.CreatedAt)
	if err != nil || createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return portforwardrepo.Record{
		SessionID:      view.SessionID,
		ClusterID:      view.ClusterID,
		Namespace:      view.Namespace,
		TargetKind:     view.TargetKind,
		TargetName:     view.TargetName,
		LocalPort:      view.LocalPort,
		RemotePort:     view.RemotePort,
		Status:         view.Status,
		ConnectionMode: connectionMode,
		LastError:      lastErr,
		CreatedBy:      view.CreatedBy,
		CreatedAt:      createdAt,
	}
}

// RestorePortForwards reads persisted sessions from the repository and best-effort
// re-establishes direct-mode forwards. Agent-mode sessions are recreated on the
// agent and rebound to their original Core local ports.
func (s *Service) RestorePortForwards(ctx context.Context) error {
	if s.portForwards == nil {
		return nil
	}
	records, err := s.portForwards.List(ctx)
	if err != nil {
		return err
	}
	for _, rec := range records {
		if rec.ConnectionMode != "direct" {
			if rec.ConnectionMode == "agent" {
				if err := s.restoreAgentPortForward(ctx, rec); err != nil {
					_ = s.portForwards.MarkStatus(ctx, rec.SessionID, "error", fmt.Sprintf("restore: %v", err))
				}
				continue
			}
			_ = s.portForwards.MarkStatus(ctx, rec.SessionID, "pending", "unsupported connection mode")
			continue
		}
		bundle, err := s.clusters.Bundle(ctx, rec.ClusterID)
		if err != nil {
			_ = s.portForwards.MarkStatus(ctx, rec.SessionID, "error", fmt.Sprintf("restore: cluster unavailable: %v", err))
			continue
		}
		session, err := startDirectPortForward(ctx, bundle, rec.Namespace, rec.TargetKind, rec.TargetName, rec.LocalPort, rec.RemotePort)
		if err != nil {
			_ = s.portForwards.MarkStatus(ctx, rec.SessionID, "error", fmt.Sprintf("restore: %v", err))
			continue
		}
		session.view.SessionID = rec.SessionID
		session.view.ClusterID = rec.ClusterID
		session.view.CreatedBy = rec.CreatedBy
		session.view.CreatedAt = rec.CreatedAt.UTC().Format(time.RFC3339)

		registerPortForwardSession(session)
		_ = s.portForwards.MarkStatus(ctx, rec.SessionID, "active", "")
	}
	return nil
}

func (s *Service) restoreAgentPortForward(ctx context.Context, rec portforwardrepo.Record) error {
	if s.resolver == nil {
		return fmt.Errorf("connection resolver unavailable")
	}
	connection, err := s.resolver.GetConnection(ctx, rec.ClusterID)
	if err != nil {
		return fmt.Errorf("connection unavailable: %w", err)
	}
	if connection.Summary.ConnectionMode != domaincluster.ConnectionModeAgent {
		return fmt.Errorf("connection mode is %s, want agent", connection.Summary.ConnectionMode)
	}
	client, err := s.agentClient(connection)
	if err != nil {
		return err
	}
	view, err := client.RegisterPortForward(ctx, domainresource.PortForwardRegisterInput{
		Namespace:  rec.Namespace,
		TargetKind: rec.TargetKind,
		TargetName: rec.TargetName,
		LocalPort:  rec.LocalPort,
		RemotePort: rec.RemotePort,
	})
	if err != nil {
		return fmt.Errorf("register agent session: %w", err)
	}
	if view.ClusterID == "" {
		view.ClusterID = rec.ClusterID
	}
	if view.CreatedBy == "" {
		view.CreatedBy = rec.CreatedBy
	}
	session, err := startAgentPortForwardTunnel(client, view)
	if err != nil {
		_ = client.StopPortForward(context.Background(), view.SessionID)
		return err
	}
	registerPortForwardSession(session)
	if rec.SessionID != session.view.SessionID {
		if err := s.portForwards.Delete(ctx, rec.SessionID); err != nil {
			cleanupRegisteredPortForwardSession(session)
			_ = client.StopPortForward(context.Background(), view.SessionID)
			return err
		}
	}
	if err := persistRegisteredPortForwardSession(ctx, s.portForwards, session, "agent"); err != nil {
		_ = client.StopPortForward(context.Background(), view.SessionID)
		return err
	}
	return nil
}

func (s *Service) ListPortForwards(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.PortForwardSessionView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, "", "PortForward", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	seen := map[string]domainresource.PortForwardSessionView{}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err := client.ListPortForwards(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		for _, item := range items {
			if item.ClusterID == "" {
				item.ClusterID = clusterID
			}
			seen[item.SessionID] = item
		}
	}
	portForwardRegistryMu.Lock()
	for _, session := range portForwardRegistry {
		if session.view.ClusterID == clusterID {
			seen[session.view.SessionID] = session.view
		}
	}
	portForwardRegistryMu.Unlock()
	if s.portForwards != nil {
		records, err := s.portForwards.List(ctx)
		if err == nil {
			for _, rec := range records {
				if rec.ClusterID != clusterID {
					continue
				}
				if _, ok := seen[rec.SessionID]; ok {
					continue
				}
				seen[rec.SessionID] = toSessionView(rec)
			}
		}
	}
	out := make([]domainresource.PortForwardSessionView, 0, len(seen))
	for _, view := range seen {
		out = append(out, view)
	}
	return out, nil
}

func (s *Service) RegisterPortForward(ctx context.Context, principal domainidentity.Principal, clusterID string, input domainresource.PortForwardRegisterInput) (domainresource.PortForwardSessionView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, input.Namespace, "PortForward", domainaccess.ActionUpdate)
	if err != nil {
		return domainresource.PortForwardSessionView{}, err
	}
	kind := strings.TrimSpace(input.TargetKind)
	if kind == "" {
		kind = "Pod"
	}
	if strings.TrimSpace(input.TargetName) == "" {
		return domainresource.PortForwardSessionView{}, fmt.Errorf("%w: targetName is required", apperrors.ErrInvalidArgument)
	}
	if input.LocalPort <= 0 || input.RemotePort <= 0 {
		return domainresource.PortForwardSessionView{}, fmt.Errorf("%w: localPort and remotePort must be positive", apperrors.ErrInvalidArgument)
	}
	namespace := strings.TrimSpace(input.Namespace)
	if namespace == "" {
		namespace = "default"
	}

	connectionMode := "direct"
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		connectionMode = "agent"
	}

	if connectionMode == "agent" {
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.PortForwardSessionView{}, err
		}
		view, err := client.RegisterPortForward(ctx, domainresource.PortForwardRegisterInput{
			Namespace:  namespace,
			TargetKind: kind,
			TargetName: strings.TrimSpace(input.TargetName),
			LocalPort:  input.LocalPort,
			RemotePort: input.RemotePort,
		})
		if err != nil {
			return domainresource.PortForwardSessionView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		if view.ClusterID == "" {
			view.ClusterID = clusterID
		}
		if view.CreatedBy == "" {
			view.CreatedBy = principal.UserID
		}
		session, err := startAgentPortForwardTunnel(client, view)
		if err != nil {
			_ = client.StopPortForward(context.Background(), view.SessionID)
			return domainresource.PortForwardSessionView{}, err
		}
		registerPortForwardSession(session)
		if s.portForwards != nil {
			if err := persistRegisteredPortForwardSession(ctx, s.portForwards, session, "agent"); err != nil {
				_ = client.StopPortForward(context.Background(), view.SessionID)
				return domainresource.PortForwardSessionView{}, err
			}
		}
		return session.view, nil
	}

	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return domainresource.PortForwardSessionView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	session, err := startDirectPortForward(ctx, bundle, namespace, kind, strings.TrimSpace(input.TargetName), input.LocalPort, input.RemotePort)
	if err != nil {
		return domainresource.PortForwardSessionView{}, err
	}
	session.view.SessionID = uuid.NewString()
	session.view.ClusterID = clusterID
	session.view.CreatedBy = principal.UserID
	session.view.CreatedAt = time.Now().UTC().Format(time.RFC3339)

	registerPortForwardSession(session)

	if s.portForwards != nil {
		if err := persistRegisteredPortForwardSession(ctx, s.portForwards, session, "direct"); err != nil {
			return domainresource.PortForwardSessionView{}, err
		}
	}
	return session.view, nil
}

func (s *Service) StopPortForward(ctx context.Context, principal domainidentity.Principal, clusterID, sessionID string) error {
	connection, _, err := s.authorize(ctx, principal, clusterID, "", "PortForward", domainaccess.ActionDelete)
	if err != nil {
		return err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := s.agentClient(connection)
		if err != nil {
			return err
		}
		portForwardRegistryMu.Lock()
		session, ok := portForwardRegistry[sessionID]
		if ok && session.view.ClusterID != clusterID {
			portForwardRegistryMu.Unlock()
			return fmt.Errorf("%w: port forward session not found", apperrors.ErrNotFound)
		}
		if ok {
			delete(portForwardRegistry, sessionID)
		}
		portForwardRegistryMu.Unlock()
		if ok {
			stopPortForwardSession(session)
		}
		if err := client.StopPortForward(ctx, sessionID); err != nil {
			return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		if s.portForwards != nil {
			if err := s.portForwards.Delete(ctx, sessionID); err != nil {
				return err
			}
		}
		return nil
	}
	portForwardRegistryMu.Lock()
	session, ok := portForwardRegistry[sessionID]
	if ok && session.view.ClusterID != clusterID {
		portForwardRegistryMu.Unlock()
		return fmt.Errorf("%w: port forward session not found", apperrors.ErrNotFound)
	}
	if ok {
		delete(portForwardRegistry, sessionID)
	}
	portForwardRegistryMu.Unlock()
	if ok {
		stopPortForwardSession(session)
	}
	if s.portForwards != nil {
		if err := s.portForwards.Delete(ctx, sessionID); err != nil {
			return err
		}
	} else if !ok {
		return fmt.Errorf("%w: port forward session not found", apperrors.ErrNotFound)
	}
	return nil
}

func stopPortForwardSession(session *portForwardSession) {
	if session == nil {
		return
	}
	session.once.Do(func() {
		if session.cancel != nil {
			session.cancel()
		}
		close(session.stopCh)
		select {
		case <-session.doneCh:
		case <-time.After(5 * time.Second):
		}
	})
}

func registerPortForwardSession(session *portForwardSession) {
	if session == nil {
		return
	}
	portForwardRegistryMu.Lock()
	portForwardRegistry[session.view.SessionID] = session
	portForwardRegistryMu.Unlock()
}

func cleanupRegisteredPortForwardSession(session *portForwardSession) {
	if session == nil {
		return
	}
	portForwardRegistryMu.Lock()
	delete(portForwardRegistry, session.view.SessionID)
	portForwardRegistryMu.Unlock()
	stopPortForwardSession(session)
}

func persistRegisteredPortForwardSession(ctx context.Context, repo PortForwardRepository, session *portForwardSession, connectionMode string) error {
	if repo == nil {
		return nil
	}
	if err := repo.Upsert(ctx, fromSessionView(session.view, connectionMode, "")); err != nil {
		cleanupRegisteredPortForwardSession(session)
		return err
	}
	return nil
}

func startAgentPortForwardTunnel(client portForwardTunnelClient, view domainresource.PortForwardSessionView) (*portForwardSession, error) {
	if client == nil {
		return nil, fmt.Errorf("agent port-forward client unavailable")
	}
	if strings.TrimSpace(view.SessionID) == "" {
		return nil, fmt.Errorf("agent port-forward session id is missing")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(view.LocalPort)))
	if err != nil {
		return nil, fmt.Errorf("listen on 127.0.0.1:%d: %w", view.LocalPort, err)
	}
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	sessionCtx, cancel := context.WithCancel(context.Background())
	if view.Status == "" || view.Status == "registered" {
		view.Status = "active"
	}
	session := &portForwardSession{
		view:   view,
		stopCh: stopCh,
		doneCh: doneCh,
		cancel: cancel,
	}
	go runAgentPortForwardListener(sessionCtx, listener, client, session)
	return session, nil
}

func runAgentPortForwardListener(ctx context.Context, listener net.Listener, client portForwardTunnelClient, session *portForwardSession) {
	defer close(session.doneCh)
	defer listener.Close()
	go func() {
		select {
		case <-session.stopCh:
			_ = listener.Close()
		case <-ctx.Done():
			_ = listener.Close()
		}
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-session.stopCh:
				return
			case <-ctx.Done():
				return
			default:
				session.view.Status = "error"
				session.lastErr = err.Error()
				return
			}
		}
		go func(conn net.Conn) {
			defer conn.Close()
			if err := client.StreamPortForward(ctx, session.view.SessionID, conn); err != nil {
				session.lastErr = err.Error()
			}
		}(conn)
	}
}

func startDirectPortForward(ctx context.Context, bundle *k8sinfra.Bundle, namespace, kind, name string, localPort, remotePort int) (*portForwardSession, error) {
	podName, targetPort, err := resolvePortForwardTarget(ctx, bundle, namespace, kind, name, remotePort)
	if err != nil {
		return nil, err
	}
	serverURL, err := buildPortForwardURL(bundle.RESTConfig.Host, namespace, podName)
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
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}

	forwarder, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, []string{fmt.Sprintf("%d:%d", localPort, targetPort)}, stopCh, readyCh, outBuf, errBuf)
	if err != nil {
		return nil, fmt.Errorf("build port forwarder: %w", err)
	}

	session := &portForwardSession{
		view: domainresource.PortForwardSessionView{
			Namespace:  namespace,
			TargetKind: kind,
			TargetName: name,
			LocalPort:  localPort,
			RemotePort: remotePort,
			Status:     "starting",
		},
		stopCh: stopCh,
		doneCh: doneCh,
	}

	go func() {
		defer close(doneCh)
		if err := forwarder.ForwardPorts(); err != nil {
			session.lastErr = err.Error()
		}
	}()

	select {
	case <-readyCh:
		session.view.Status = "active"
	case <-time.After(10 * time.Second):
		close(stopCh)
		<-doneCh
		return nil, fmt.Errorf("port forward did not become ready within 10s (stderr: %s)", strings.TrimSpace(errBuf.String()))
	}
	return session, nil
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
		for _, p := range svc.Spec.Ports {
			if int(p.Port) == port {
				if p.TargetPort.IntValue() > 0 {
					targetPort = p.TargetPort.IntValue()
				}
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
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
