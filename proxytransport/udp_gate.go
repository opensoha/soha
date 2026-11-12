package proxytransport

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// SOCKS5Gate binds only loopback. The TCP password exchange authenticates a
// dedicated UDP relay socket. It is not isolation from malicious local processes
// able to race/spoof the first UDP source port; callers own the local host boundary.
type SOCKS5Gate struct {
	mu              sync.Mutex
	credentials     map[string][32]byte
	dial            Dial
	dialPacket      DialPacket
	authorize       PacketAuthorize
	until           time.Time
	noNew, closed   bool
	listener        net.Listener
	sessions        map[*socksGateSession]struct{}
	active, pending int
}
type socksGateSession struct {
	mu         sync.Mutex
	gate       *SOCKS5Gate
	control    net.Conn
	tcpBackend net.Conn
	udp        *net.UDPConn
	peer       *net.UDPAddr
	user       string
	closed     bool
	activity   time.Time
	inners     map[string]net.Conn
}

func NewSOCKS5Gate(credentials map[string]string, dial Dial, dialPacket DialPacket, authorize PacketAuthorize, deadline time.Time) (*SOCKS5Gate, error) {
	c, err := packetCredentials(credentials, 255)
	if err != nil {
		return nil, err
	}
	for _, p := range credentials {
		if len(p) > 255 {
			return nil, errors.New("SOCKS password exceeds 255 bytes")
		}
	}
	if dial == nil || dialPacket == nil || authorize == nil || !validPacketLease(deadline) {
		return nil, errors.New("invalid SOCKS gate dependencies or lease")
	}
	return &SOCKS5Gate{credentials: c, dial: dial, dialPacket: dialPacket, authorize: authorize, until: deadline, sessions: map[*socksGateSession]struct{}{}}, nil
}
func (g *SOCKS5Gate) NoNew() { g.mu.Lock(); g.noNew = true; g.mu.Unlock() }

// ActiveCount is the number of live destination-specific UDP inner associations.
// TCP controls and pending dials are separately bounded, not reported as active UDP.
func (g *SOCKS5Gate) ActiveCount() int { g.mu.Lock(); defer g.mu.Unlock(); return g.active }
func (g *SOCKS5Gate) lease() (time.Time, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.until, !g.closed && g.until.After(time.Now())
}
func (g *SOCKS5Gate) Refresh(until time.Time) error {
	g.mu.Lock()
	if g.closed || !g.until.After(time.Now()) {
		g.mu.Unlock()
		return net.ErrClosed
	}
	if !validPacketLease(until) {
		g.mu.Unlock()
		return errors.New("invalid SOCKS gate renewal")
	}
	g.until = until
	sessions := make([]*socksGateSession, 0, len(g.sessions))
	for s := range g.sessions {
		sessions = append(sessions, s)
	}
	g.mu.Unlock()
	for _, s := range sessions {
		if err := s.refresh(until); err != nil && !errors.Is(err, net.ErrClosed) {
			return err
		}
	}
	_, live := g.lease()
	if !live {
		return net.ErrClosed
	}
	return nil
}
func (g *SOCKS5Gate) Close() error {
	g.mu.Lock()
	g.closed = true
	g.noNew = true
	listener := g.listener
	sessions := make([]*socksGateSession, 0, len(g.sessions))
	for s := range g.sessions {
		sessions = append(sessions, s)
	}
	g.mu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
	for _, s := range sessions {
		s.close()
	}
	return nil
}
func (g *SOCKS5Gate) Serve(listener net.Listener) error {
	if listener == nil || !loopbackAddress(listener.Addr().String()) {
		return errors.New("SOCKS gate requires loopback listener")
	}
	g.mu.Lock()
	if g.closed || g.listener != nil {
		g.mu.Unlock()
		return net.ErrClosed
	}
	g.listener = listener
	g.mu.Unlock()
	for {
		c, err := listener.Accept()
		if err != nil {
			return err
		}
		g.mu.Lock()
		allowed := !g.closed && !g.noNew && g.until.After(time.Now()) && len(g.sessions) < MaxPacketAssociations && loopbackAddress(c.RemoteAddr().String())
		if allowed {
			s := &socksGateSession{gate: g, control: c, activity: time.Now(), inners: map[string]net.Conn{}}
			g.sessions[s] = struct{}{}
			g.mu.Unlock()
			go s.serve()
		} else {
			g.mu.Unlock()
			_ = c.Close()
		}
	}
}
func (s *socksGateSession) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	inners := make([]net.Conn, 0, len(s.inners))
	for _, c := range s.inners {
		inners = append(inners, c)
	}
	s.inners = map[string]net.Conn{}
	udp := s.udp
	tcpBackend := s.tcpBackend
	s.mu.Unlock()
	_ = s.control.Close()
	if tcpBackend != nil {
		_ = tcpBackend.Close()
	}
	if udp != nil {
		_ = udp.Close()
	}
	for _, c := range inners {
		_ = c.Close()
	}
	s.gate.mu.Lock()
	s.gate.active -= len(inners)
	delete(s.gate.sessions, s)
	s.gate.mu.Unlock()
}
func (s *socksGateSession) refresh(until time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return net.ErrClosed
	}
	deadline := until
	if s.udp != nil {
		deadline = earlier(until, s.activity.Add(PacketIdleTimeout))
	}
	if err := s.control.SetDeadline(deadline); err != nil {
		return err
	}
	if s.tcpBackend != nil {
		if err := s.tcpBackend.SetDeadline(until); err != nil && !errors.Is(err, net.ErrClosed) {
			return err
		}
	}
	if s.udp != nil {
		if err := s.udp.SetDeadline(deadline); err != nil {
			return err
		}
	}
	for target, c := range s.inners {
		if err := refreshPacketConn(c, until); err != nil {
			if errors.Is(err, net.ErrClosed) {
				_ = c.Close()
				delete(s.inners, target)
				s.gate.mu.Lock()
				s.gate.active--
				s.gate.mu.Unlock()
				continue
			}
			return err
		}
	}
	return nil
}
func (s *socksGateSession) touch(until time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !until.After(time.Now()) {
		return net.ErrClosed
	}
	s.activity = time.Now()
	d := earlier(until, s.activity.Add(PacketIdleTimeout))
	return errors.Join(s.udp.SetDeadline(d), s.control.SetDeadline(d))
}
func (s *socksGateSession) serve() {
	defer s.close()
	until, live := s.gate.lease()
	if !live {
		return
	}
	_ = s.control.SetDeadline(earlier(until, time.Now().Add(5*time.Second)))
	if s.authenticate() != nil {
		return
	}
	var request [3]byte
	if _, err := io.ReadFull(s.control, request[:]); err != nil {
		return
	}
	if request[0] != 5 || request[2] != 0 {
		_ = socksReply(s.control, 1, "0.0.0.0:0")
		return
	}
	target, err := readSOCKSAddress(s.control)
	if err != nil {
		return
	}
	switch request[1] {
	case 1:
		s.tcp(target)
	case 3:
		s.udpAssociate(target)
	default:
		_ = socksReply(s.control, 7, "0.0.0.0:0")
	}
}
func (s *socksGateSession) authenticate() error {
	var hello [2]byte
	if _, err := io.ReadFull(s.control, hello[:]); err != nil {
		return err
	}
	if hello[0] != 5 || hello[1] == 0 {
		return errors.New("invalid SOCKS greeting")
	}
	methods := make([]byte, int(hello[1]))
	if _, err := io.ReadFull(s.control, methods); err != nil {
		return err
	}
	offered := false
	for _, m := range methods {
		offered = offered || m == 2
	}
	if !offered {
		_ = writePacketAll(s.control, []byte{5, 255})
		return errors.New("SOCKS password required")
	}
	if err := writePacketAll(s.control, []byte{5, 2}); err != nil {
		return err
	}
	var auth [2]byte
	if _, err := io.ReadFull(s.control, auth[:]); err != nil {
		return err
	}
	if auth[0] != 1 || auth[1] == 0 {
		return errors.New("invalid SOCKS authentication")
	}
	user := make([]byte, int(auth[1]))
	if _, err := io.ReadFull(s.control, user); err != nil {
		return err
	}
	var size [1]byte
	if _, err := io.ReadFull(s.control, size[:]); err != nil {
		return err
	}
	password := make([]byte, int(size[0]))
	if _, err := io.ReadFull(s.control, password); err != nil {
		return err
	}
	if !authenticatePacket(s.gate.credentials, string(user), string(password)) {
		_ = writePacketAll(s.control, []byte{1, 1})
		return errors.New("SOCKS authentication rejected")
	}
	s.user = string(user)
	return writePacketAll(s.control, []byte{1, 0})
}
func (s *socksGateSession) tcp(target string) {
	if packetTarget(target) != nil {
		_ = socksReply(s.control, 8, "0.0.0.0:0")
		return
	}
	s.gate.mu.Lock()
	allowed := !s.gate.noNew && !s.gate.closed
	until := s.gate.until
	s.gate.mu.Unlock()
	if !allowed || !until.After(time.Now()) {
		return
	}
	ctx, cancel := context.WithDeadline(context.Background(), earlier(until, time.Now().Add(5*time.Second)))
	back, err := s.gate.dial(ctx, s.user, target)
	cancel()
	if err != nil || back == nil {
		_ = socksReply(s.control, 5, "0.0.0.0:0")
		return
	}
	defer func() { _ = back.Close() }()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.tcpBackend = back
	s.mu.Unlock()
	_ = s.control.SetDeadline(until)
	if back.SetDeadline(until) != nil {
		return
	}
	if socksReply(s.control, 0, "0.0.0.0:0") != nil {
		return
	}
	done := make(chan struct{})
	go func() { _, _ = io.Copy(back, s.control); _ = back.Close(); close(done) }()
	_, _ = io.Copy(s.control, back)
	_ = s.control.Close()
	_ = back.Close()
	<-done
}
func (s *socksGateSession) udpAssociate(source string) {
	sourceHost, sourcePort, err := net.SplitHostPort(source)
	if err != nil {
		return
	}
	sourceIP := net.ParseIP(sourceHost)
	port, err := strconv.Atoi(sourcePort)
	remoteHost, _, remoteErr := net.SplitHostPort(s.control.RemoteAddr().String())
	remoteIP := net.ParseIP(remoteHost)
	if err != nil || remoteErr != nil || sourceIP == nil || port < 0 || port > 65535 || !sourceIP.IsUnspecified() && !sourceIP.Equal(remoteIP) {
		_ = socksReply(s.control, 2, "0.0.0.0:0")
		return
	}
	bindIP := net.ParseIP("127.0.0.1")
	if remoteIP.To4() == nil {
		bindIP = net.IPv6loopback
	}
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: bindIP})
	if err != nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = udp.Close()
		return
	}
	s.udp = udp
	s.activity = time.Now()
	if port != 0 {
		s.peer = &net.UDPAddr{IP: remoteIP, Port: port}
	}
	s.mu.Unlock()
	until, live := s.gate.lease()
	if !live || s.refresh(until) != nil {
		return
	}
	if socksReply(s.control, 0, udp.LocalAddr().String()) != nil {
		return
	}
	go func() { _, _ = io.Copy(io.Discard, s.control); s.close() }()
	buffer := make([]byte, 65536)
	for {
		n, from, err := udp.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		if !from.IP.Equal(remoteIP) {
			continue
		}
		target, payload, err := decodeSOCKSDatagram(buffer[:n])
		if err != nil {
			continue
		}
		s.mu.Lock()
		if s.peer == nil {
			s.peer = &net.UDPAddr{IP: append(net.IP(nil), from.IP...), Port: from.Port, Zone: from.Zone}
		}
		peer := s.peer
		validPeer := peer.Port == from.Port && peer.IP.Equal(from.IP)
		s.mu.Unlock()
		if !validPeer {
			continue
		}
		until, live = s.gate.lease()
		if !live || authorizePacket(s.gate.authorize, s.user, target, until) != nil {
			if !live {
				return
			}
			s.dropTarget(target)
			continue
		}
		if s.touch(until) != nil {
			return
		}
		c, err := s.inner(target, until)
		if err != nil {
			continue
		}
		if _, err := c.Write(payload); err != nil {
			s.remove(target, c)
			_ = c.Close()
		}
	}
}
func (s *socksGateSession) dropTarget(target string) {
	s.mu.Lock()
	c := s.inners[target]
	s.mu.Unlock()
	if c != nil {
		s.remove(target, c)
		_ = c.Close()
	}
}
func (s *socksGateSession) remove(target string, c net.Conn) {
	s.mu.Lock()
	if s.inners[target] != c {
		s.mu.Unlock()
		return
	}
	delete(s.inners, target)
	s.mu.Unlock()
	s.gate.mu.Lock()
	s.gate.active--
	s.gate.mu.Unlock()
}
func (s *socksGateSession) inner(target string, until time.Time) (net.Conn, error) {
	s.mu.Lock()
	existing := s.inners[target]
	full := s.closed || len(s.inners) >= MaxPacketTargets
	s.mu.Unlock()
	if existing != nil {
		return existing, nil
	}
	if full {
		return nil, errors.New("SOCKS target limit reached")
	}
	g := s.gate
	g.mu.Lock()
	allowed := !g.closed && !g.noNew && g.until.After(time.Now()) && g.active+g.pending < MaxPacketAssociations
	if allowed {
		g.pending++
	}
	g.mu.Unlock()
	if !allowed {
		return nil, errors.New("SOCKS new target disabled")
	}
	defer func() { g.mu.Lock(); g.pending--; g.mu.Unlock() }()
	ctx, cancel := context.WithDeadline(context.Background(), earlier(until, time.Now().Add(5*time.Second)))
	c, err := g.dialPacket(ctx, s.user, target)
	cancel()
	if err != nil || c == nil {
		return nil, errors.New("datagram target unavailable")
	}
	if err := refreshPacketConn(c, until); err != nil {
		_ = c.Close()
		return nil, err
	}
	s.mu.Lock()
	g.mu.Lock()
	if s.closed || g.closed || g.noNew || !g.until.After(time.Now()) {
		g.mu.Unlock()
		s.mu.Unlock()
		_ = c.Close()
		return nil, net.ErrClosed
	}
	s.inners[target] = c
	g.active++
	g.mu.Unlock()
	s.mu.Unlock()
	go s.readTarget(target, c)
	return c, nil
}
func (s *socksGateSession) readTarget(target string, c net.Conn) {
	defer func() { s.remove(target, c); _ = c.Close() }()
	buffer := make([]byte, MaxDatagramBytes)
	for {
		n, err := c.Read(buffer)
		if err != nil {
			return
		}
		until, live := s.gate.lease()
		if !live || authorizePacket(s.gate.authorize, s.user, target, until) != nil {
			return
		}
		packet, err := encodeSOCKSDatagram(target, buffer[:n])
		if err != nil {
			return
		}
		s.mu.Lock()
		udp, peer, closed := s.udp, s.peer, s.closed
		s.mu.Unlock()
		if closed || peer == nil || udp == nil {
			return
		}
		if s.touch(until) != nil {
			return
		}
		if _, err := udp.WriteToUDP(packet, peer); err != nil {
			return
		}
	}
}
