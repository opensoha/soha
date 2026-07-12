package resource

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type portForwardSession struct {
	view    domainresource.PortForwardSessionView
	stopCh  chan struct{}
	doneCh  chan struct{}
	lastErr string
	once    sync.Once
	cancel  context.CancelFunc
	direct  DirectPortForwardSession
}

type portForwardTunnelClient interface {
	StreamPortForward(context.Context, string, net.Conn) error
}

var (
	portForwardRegistryMu sync.Mutex
	portForwardRegistry   = map[string]*portForwardSession{}
)

func toSessionView(rec PortForwardRecord) domainresource.PortForwardSessionView {
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

func fromSessionView(view domainresource.PortForwardSessionView, connectionMode, lastErr string) PortForwardRecord {
	createdAt, err := time.Parse(time.RFC3339, view.CreatedAt)
	if err != nil || createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return PortForwardRecord{
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
func (p *PortForwards) RestorePortForwards(ctx context.Context) error {
	s := p
	if s.repository == nil {
		return nil
	}
	records, err := s.repository.List(ctx)
	if err != nil {
		return err
	}
	for _, rec := range records {
		if rec.ConnectionMode != "direct" {
			if rec.ConnectionMode == "agent" {
				if err := s.restoreAgentPortForward(ctx, rec); err != nil {
					_ = s.repository.MarkStatus(ctx, rec.SessionID, "error", fmt.Sprintf("restore: %v", err))
				}
				continue
			}
			_ = s.repository.MarkStatus(ctx, rec.SessionID, "pending", "unsupported connection mode")
			continue
		}
		if s.direct == nil {
			_ = s.repository.MarkStatus(ctx, rec.SessionID, "error", "restore: direct port-forward starter unavailable")
			continue
		}
		view := toSessionView(rec)
		view.Status = "starting"
		handle, err := s.direct.StartPortForward(ctx, rec.ClusterID, view)
		if err != nil {
			_ = s.repository.MarkStatus(ctx, rec.SessionID, "error", fmt.Sprintf("restore: %v", err))
			continue
		}
		view.Status = "active"
		session := &portForwardSession{view: view, direct: handle}

		registerPortForwardSession(session)
		_ = s.repository.MarkStatus(ctx, rec.SessionID, "active", "")
	}
	return nil
}

func (s *PortForwards) restoreAgentPortForward(ctx context.Context, rec PortForwardRecord) error {
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
	client, err := s.portForwardAgentClient(connection)
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
		if err := s.repository.Delete(ctx, rec.SessionID); err != nil {
			cleanupRegisteredPortForwardSession(session)
			_ = client.StopPortForward(context.Background(), view.SessionID)
			return err
		}
	}
	if err := persistRegisteredPortForwardSession(ctx, s.repository, session, "agent"); err != nil {
		_ = client.StopPortForward(context.Background(), view.SessionID)
		return err
	}
	return nil
}

func (p *PortForwards) ListPortForwards(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.PortForwardSessionView, error) {
	s := p
	connection, _, err := s.authorize(ctx, principal, clusterID, "", "PortForward", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	seen := map[string]domainresource.PortForwardSessionView{}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := s.portForwardAgentClient(connection)
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
	if s.repository != nil {
		records, err := s.repository.List(ctx)
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

func (p *PortForwards) RegisterPortForward(ctx context.Context, principal domainidentity.Principal, clusterID string, input domainresource.PortForwardRegisterInput) (domainresource.PortForwardSessionView, error) {
	s := p
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
		client, err := s.portForwardAgentClient(connection)
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
		if s.repository != nil {
			if err := persistRegisteredPortForwardSession(ctx, s.repository, session, "agent"); err != nil {
				_ = client.StopPortForward(context.Background(), view.SessionID)
				return domainresource.PortForwardSessionView{}, err
			}
		}
		return session.view, nil
	}

	if s.direct == nil {
		return domainresource.PortForwardSessionView{}, fmt.Errorf("%w: direct port-forward starter is not configured", apperrors.ErrClusterUnready)
	}
	view := domainresource.PortForwardSessionView{
		SessionID: uuid.NewString(), ClusterID: clusterID, Namespace: namespace,
		TargetKind: kind, TargetName: strings.TrimSpace(input.TargetName),
		LocalPort: input.LocalPort, RemotePort: input.RemotePort, Status: "starting",
		CreatedBy: principal.UserID, CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	handle, err := s.direct.StartPortForward(ctx, clusterID, view)
	if err != nil {
		return domainresource.PortForwardSessionView{}, err
	}
	view.Status = "active"
	session := &portForwardSession{view: view, direct: handle}

	registerPortForwardSession(session)

	if s.repository != nil {
		if err := persistRegisteredPortForwardSession(ctx, s.repository, session, "direct"); err != nil {
			return domainresource.PortForwardSessionView{}, err
		}
	}
	return session.view, nil
}

func (p *PortForwards) StopPortForward(ctx context.Context, principal domainidentity.Principal, clusterID, sessionID string) error {
	s := p
	connection, _, err := s.authorize(ctx, principal, clusterID, "", "PortForward", domainaccess.ActionDelete)
	if err != nil {
		return err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := s.portForwardAgentClient(connection)
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
		if s.repository != nil {
			if err := s.repository.Delete(ctx, sessionID); err != nil {
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
	if s.repository != nil {
		if err := s.repository.Delete(ctx, sessionID); err != nil {
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
		if session.direct != nil {
			session.direct.Stop()
			session.lastErr = session.direct.LastError()
			return
		}
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
	defer func() { _ = listener.Close() }()
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
			defer func() { _ = conn.Close() }()
			if err := client.StreamPortForward(ctx, session.view.SessionID, conn); err != nil {
				session.lastErr = err.Error()
			}
		}(conn)
	}
}
