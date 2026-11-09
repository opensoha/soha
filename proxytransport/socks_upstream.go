package proxytransport

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync"
	"time"
)

// UpstreamConnect returns a connected TCP/TLS connection to a source IP already
// resolved, policy-checked and pinned by the caller. TLS identity verification is
// the caller's responsibility and must finish before returning a TLS connection.
// The shared client independently checks RemoteAddr against approvedUpstreamIP.
// No DNS, source-policy bypass, target-direct fallback or secret logging occurs.
type UpstreamConnect func(context.Context) (net.Conn, error)

func normalizedUpstreamIP(ip netip.Addr) (netip.Addr, error) {
	if !ip.IsValid() || ip.IsUnspecified() || ip.Zone() != "" {
		return netip.Addr{}, errors.New("invalid approved SOCKS source IP")
	}
	return ip.Unmap(), nil
}
func pinnedSOCKSTarget(target string) (string, error) {
	if packetTarget(target) != nil {
		return "", errors.New("invalid pinned SOCKS target")
	}
	host, port, _ := net.SplitHostPort(target)
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Zone() != "" || ip.IsUnspecified() {
		return "", errors.New("SOCKS target must be a pinned literal IP")
	}
	return net.JoinHostPort(ip.Unmap().String(), port), nil
}
func actualSOCKSPeer(c net.Conn, approved netip.Addr) bool {
	if c == nil || c.RemoteAddr() == nil {
		return false
	}
	host, port, err := net.SplitHostPort(c.RemoteAddr().String())
	ip, e := netip.ParseAddr(host)
	p, pe := strconv.Atoi(port)
	return err == nil && e == nil && pe == nil && p > 0 && p <= 65535 && ip.Zone() == "" && ip.Unmap() == approved
}
func connectSOCKSUpstream(ctx context.Context, connect UpstreamConnect, approved netip.Addr, user, secret string) (net.Conn, error) {
	c, err := connect(ctx)
	if err != nil {
		if c != nil {
			_ = c.Close()
		}
		return nil, errors.New("SOCKS source connection failed")
	}
	if !actualSOCKSPeer(c, approved) {
		if c != nil {
			_ = c.Close()
		}
		return nil, errors.New("SOCKS source peer does not match approved IP")
	}
	if d, ok := ctx.Deadline(); ok {
		if c.SetDeadline(d) != nil {
			_ = c.Close()
			return nil, errors.New("SOCKS source deadline failed")
		}
	}
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	if err := upstreamSOCKSAuth(c, user, secret); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}
func upstreamSOCKSAuth(c net.Conn, user, secret string) error {
	// Empty user is an explicit NoAuth choice, not password negotiation fallback.
	method := byte(2)
	if user == "" {
		if secret != "" {
			return errors.New("SOCKS secret requires a username")
		}
		method = 0
	} else if len(user) > 255 || len(secret) == 0 || len(secret) > 255 {
		return errors.New("invalid upstream SOCKS credential size")
	}
	if writePacketAll(c, []byte{5, 1, method}) != nil {
		return errors.New("SOCKS authentication handshake failed")
	}
	var reply [2]byte
	if _, err := io.ReadFull(c, reply[:]); err != nil || reply != [2]byte{5, method} {
		return errors.New("SOCKS authentication method rejected")
	}
	if method == 0 {
		return nil
	}
	request := []byte{1, byte(len(user))}
	request = append(request, []byte(user)...)
	request = append(request, byte(len(secret)))
	request = append(request, []byte(secret)...)
	if writePacketAll(c, request) != nil {
		return errors.New("SOCKS authentication exchange failed")
	}
	if _, err := io.ReadFull(c, reply[:]); err != nil || reply != [2]byte{1, 0} {
		return errors.New("SOCKS authentication rejected")
	}
	return nil
}
func upstreamSOCKSRequest(c net.Conn, command byte, target string) error {
	address, err := encodeSOCKSAddress(target, command == 3)
	if err != nil {
		return errors.New("invalid SOCKS request target")
	}
	if writePacketAll(c, append([]byte{5, command, 0}, address...)) != nil {
		return errors.New("SOCKS request failed")
	}
	var reply [3]byte
	if _, err := io.ReadFull(c, reply[:]); err != nil || reply[0] != 5 || reply[2] != 0 {
		return errors.New("invalid SOCKS reply")
	}
	if reply[1] == 7 {
		return ErrPacketUnsupported
	}
	if reply[1] != 0 {
		return errors.New("SOCKS request rejected")
	}
	return nil
}
func upstreamSetup(ctx context.Context, connect UpstreamConnect, approved netip.Addr, target string, deadline time.Time) (context.Context, context.CancelFunc, netip.Addr, string, error) {
	if ctx == nil || connect == nil || !validPacketLease(deadline) {
		return nil, nil, netip.Addr{}, "", errors.New("invalid upstream SOCKS dependencies or lease")
	}
	approved, err := normalizedUpstreamIP(approved)
	if err != nil {
		return nil, nil, netip.Addr{}, "", err
	}
	target, err = pinnedSOCKSTarget(target)
	if err != nil {
		return nil, nil, netip.Addr{}, "", err
	}
	setup, cancel := context.WithDeadline(ctx, earlier(deadline, time.Now().Add(5*time.Second)))
	return setup, cancel, approved, target, nil
}

// DialSOCKS5UpstreamTCP sends CONNECT for exactly pinnedTarget, after checking
// the factory's real TCP/TLS peer. A SOCKS reply never changes the target.
func DialSOCKS5UpstreamTCP(ctx context.Context, connect UpstreamConnect, approvedUpstreamIP netip.Addr, user, secret, pinnedTarget string, deadline time.Time) (net.Conn, error) {
	setup, cancel, approved, target, err := upstreamSetup(ctx, connect, approvedUpstreamIP, pinnedTarget, deadline)
	if err != nil {
		return nil, err
	}
	defer cancel()
	control, err := connectSOCKSUpstream(setup, connect, approved, user, secret)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = control.Close()
		}
	}()
	stop := context.AfterFunc(setup, func() { _ = control.Close() })
	defer stop()
	if err := upstreamSOCKSRequest(control, 1, target); err != nil {
		return nil, err
	}
	if _, err := readSOCKSAddress(control); err != nil {
		return nil, errors.New("invalid SOCKS CONNECT bound address")
	}
	if setup.Err() != nil {
		return nil, errors.New("SOCKS setup expired")
	}
	stream := &upstreamStreamConn{conn: control, until: deadline}
	if stream.applyLocked() != nil {
		return nil, errors.New("SOCKS stream deadline failed")
	}
	success = true
	return stream, nil
}

// DialSOCKS5UpstreamUDP permits only the already approved source IP as relay.
// An unspecified BND.ADDR is replaced by that IP. Hostnames and different IPs
// (including other loopback/protected addresses) are rejected before any UDP
// socket is opened. Only approved source IP:validated relay port is dialled;
// the final target always travels inside SOCKS, never through an OS-direct dial.
func DialSOCKS5UpstreamUDP(ctx context.Context, connect UpstreamConnect, approvedUpstreamIP netip.Addr, user, secret, pinnedTarget string, deadline time.Time) (net.Conn, error) {
	return DialSOCKS5UpstreamUDPWithDial(ctx, connect, func(_ context.Context, relay netip.AddrPort) (*net.UDPConn, error) {
		return net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(relay))
	}, approvedUpstreamIP, user, secret, pinnedTarget, deadline)
}

// UpstreamUDPConnect opens a connected UDP socket to exactly the approved literal
// relay endpoint. It is a trusted process dependency, never user configuration.
// The callback must honor ctx and must not send application packets during setup.
type UpstreamUDPConnect func(context.Context, netip.AddrPort) (*net.UDPConn, error)

// DialSOCKS5UpstreamUDPWithDial uses dialUDP only after validating the SOCKS
// relay. No callback failure or invalid socket falls back to another dial path.
// Ownership of any returned socket transfers here, including on callback error.
func DialSOCKS5UpstreamUDPWithDial(ctx context.Context, connect UpstreamConnect, dialUDP UpstreamUDPConnect, approvedUpstreamIP netip.Addr, user, secret, pinnedTarget string, deadline time.Time) (net.Conn, error) {
	if dialUDP == nil {
		return nil, errors.New("SOCKS UDP relay dialer unavailable")
	}
	setup, cancel, approved, target, err := upstreamSetup(ctx, connect, approvedUpstreamIP, pinnedTarget, deadline)
	if err != nil {
		return nil, err
	}
	defer cancel()
	control, err := connectSOCKSUpstream(setup, connect, approved, user, secret)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = control.Close()
		}
	}()
	stop := context.AfterFunc(setup, func() { _ = control.Close() })
	defer stop()
	if err := upstreamSOCKSRequest(control, 3, "0.0.0.0:0"); err != nil {
		return nil, err
	}
	var kind [1]byte
	if _, err := io.ReadFull(control, kind[:]); err != nil || kind[0] != 1 && kind[0] != 4 {
		return nil, errors.New("SOCKS UDP relay must be a literal approved IP")
	}
	bound, err := readSOCKSAddressType(control, kind[0])
	if err != nil {
		return nil, errors.New("invalid SOCKS UDP relay reply")
	}
	host, port, _ := net.SplitHostPort(bound)
	ip, err := netip.ParseAddr(host)
	number, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || number < 1 || number > 65535 {
		return nil, errors.New("invalid SOCKS UDP relay address or port")
	}
	if ip.IsUnspecified() {
		ip = approved
	}
	if ip.Unmap() != approved {
		return nil, errors.New("SOCKS UDP relay changed approved source IP")
	}
	if setup.Err() != nil {
		return nil, errors.New("SOCKS setup expired")
	}
	relay := netip.AddrPortFrom(approved, uint16(number))
	udp, err := dialUDP(setup, relay)
	if err != nil || udp == nil {
		if udp != nil {
			_ = udp.Close()
		}
		return nil, errors.New("SOCKS UDP relay socket unavailable")
	}
	remote, remoteOK := udp.RemoteAddr().(*net.UDPAddr)
	local, localOK := udp.LocalAddr().(*net.UDPAddr)
	if setup.Err() != nil || !remoteOK || !localOK || remote == nil || local == nil ||
		remote.AddrPort().Addr().Zone() != "" || remote.AddrPort().Addr().Unmap() != approved || remote.Port != int(relay.Port()) ||
		!local.AddrPort().IsValid() || local.AddrPort().Addr().Unmap().Is4() != approved.Is4() {
		_ = udp.Close()
		return nil, errors.New("invalid SOCKS UDP relay socket")
	}
	transport := &socksUDPTransport{udp: udp, control: control, target: target}
	safeTransport := &upstreamPacketTransport{socksUDPTransport: transport}
	packet, err := newMessageConn(safeTransport, deadline, safeTransport.Read, safeTransport.Write)
	if err != nil {
		_ = transport.Close()
		return nil, errors.New("SOCKS UDP lease unavailable")
	}
	if setup.Err() != nil {
		_ = packet.Close()
		return nil, errors.New("SOCKS setup expired")
	}
	success = true
	go func() { _, _ = io.Copy(io.Discard, control); _ = packet.Close() }()
	return packet, nil
}

// upstreamStreamConn is a leased byte stream (not PacketConn framing). Renewal
// never revives an expired/closed stream, and user deadlines cannot exceed lease.
type upstreamStreamConn struct {
	conn                               net.Conn
	mu                                 sync.Mutex
	until, readDeadline, writeDeadline time.Time
	closed                             bool
	once                               sync.Once
	err                                error
}

func (c *upstreamStreamConn) checkLocked() error {
	if c.closed || !c.until.After(time.Now()) {
		return net.ErrClosed
	}
	return nil
}
func (c *upstreamStreamConn) check() error { c.mu.Lock(); defer c.mu.Unlock(); return c.checkLocked() }
func (c *upstreamStreamConn) applyLocked() error {
	if err := c.conn.SetReadDeadline(earlier(c.until, c.readDeadline)); err != nil {
		return upstreamIOError(err)
	}
	return upstreamIOError(c.conn.SetWriteDeadline(earlier(c.until, c.writeDeadline)))
}
func (c *upstreamStreamConn) Read(p []byte) (int, error) {
	if err := c.check(); err != nil {
		_ = c.Close()
		return 0, err
	}
	n, err := c.conn.Read(p)
	if leaseErr := c.check(); leaseErr != nil {
		_ = c.Close()
		return 0, leaseErr
	}
	if err != nil {
		_ = c.Close()
	}
	return n, upstreamIOError(err)
}
func (c *upstreamStreamConn) Write(p []byte) (int, error) {
	if err := c.check(); err != nil {
		_ = c.Close()
		return 0, err
	}
	n, err := c.conn.Write(p)
	if leaseErr := c.check(); leaseErr != nil {
		_ = c.Close()
		return n, leaseErr
	}
	if err != nil {
		_ = c.Close()
	}
	return n, upstreamIOError(err)
}
func (c *upstreamStreamConn) Refresh(deadline time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.checkLocked(); err != nil {
		return err
	}
	if !validPacketLease(deadline) {
		return errors.New("invalid upstream stream renewal")
	}
	c.until = deadline
	return c.applyLocked()
}
func (c *upstreamStreamConn) Close() error {
	c.once.Do(func() { c.mu.Lock(); c.closed = true; c.mu.Unlock(); c.err = upstreamIOError(c.conn.Close()) })
	return c.err
}
func (c *upstreamStreamConn) LocalAddr() net.Addr  { return c.conn.LocalAddr() }
func (c *upstreamStreamConn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }
func (c *upstreamStreamConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.checkLocked(); err != nil {
		return err
	}
	c.readDeadline = t
	c.writeDeadline = t
	return c.applyLocked()
}
func (c *upstreamStreamConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.checkLocked(); err != nil {
		return err
	}
	c.readDeadline = t
	return c.applyLocked()
}
func (c *upstreamStreamConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.checkLocked(); err != nil {
		return err
	}
	c.writeDeadline = t
	return c.applyLocked()
}

// Static errors retain only lifecycle/timeout categories, never upstream
// addresses, credentials or server-controlled error text.
func upstreamIOError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, net.ErrClosed) {
		return net.ErrClosed
	}
	if errors.Is(err, io.EOF) {
		return io.EOF
	}
	if errors.Is(err, io.ErrShortBuffer) {
		return io.ErrShortBuffer
	}
	if errors.Is(err, ErrPacketUnsupported) {
		return ErrPacketUnsupported
	}
	if e, ok := err.(net.Error); ok && e.Timeout() {
		return os.ErrDeadlineExceeded
	}
	return errors.New("SOCKS upstream transport failed")
}

type upstreamPacketTransport struct{ *socksUDPTransport }

func (c *upstreamPacketTransport) Read(p []byte) (int, error) {
	packet := make([]byte, 65536)
	n, err := c.udp.Read(packet)
	if err != nil {
		return 0, upstreamIOError(err)
	}
	if n < 4 || packet[3] != 1 && packet[3] != 4 {
		return 0, errors.New("SOCKS upstream reply requires literal pinned source")
	}
	target, payload, err := decodeSOCKSDatagram(packet[:n])
	if err != nil || target != c.target {
		return 0, errors.New("SOCKS upstream reply changed pinned target")
	}
	if len(payload) > len(p) {
		return 0, io.ErrShortBuffer
	}
	return copy(p, payload), nil
}
func (c *upstreamPacketTransport) Write(p []byte) (int, error) {
	n, err := c.socksUDPTransport.Write(p)
	return n, upstreamIOError(err)
}
func (c *upstreamPacketTransport) Close() error { return upstreamIOError(c.socksUDPTransport.Close()) }
func (c *upstreamPacketTransport) SetDeadline(t time.Time) error {
	return upstreamIOError(c.socksUDPTransport.SetDeadline(t))
}
func (c *upstreamPacketTransport) SetReadDeadline(t time.Time) error {
	return upstreamIOError(c.socksUDPTransport.SetReadDeadline(t))
}
func (c *upstreamPacketTransport) SetWriteDeadline(t time.Time) error {
	return upstreamIOError(c.socksUDPTransport.SetWriteDeadline(t))
}
