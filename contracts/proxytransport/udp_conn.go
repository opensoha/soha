package proxytransport

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

const MaxDatagramBytes = 65507
const MaxPacketAssociations = 4096
const MaxPacketTargets = 128
const PacketIdleTimeout = 30 * time.Second
const PacketTransportHeader = "Soha-Transport"
const PacketTransportVersion = "udp/1"

// ErrPacketUnsupported denotes a verified transport/backend that explicitly
// cannot carry UDP. It never authorizes an OS-direct or TCP fallback.
var ErrPacketUnsupported = errors.New("guarded UDP transport unsupported")

// DialPacket returns a message-oriented connection to one authorized, pinned
// destination. It must not directly dial an unguarded destination as a fallback.
type DialPacket func(ctx context.Context, user, target string) (net.Conn, error)

// PacketAuthorize is mandatory before each datagram in either direction. The
// caller checks current node/scope/epoch/protection/rules and its hard lease.
type PacketAuthorize func(ctx context.Context, user, target string) error

// PacketConn preserves one Read/Write per datagram. Refresh is permitted only
// before the current lease and idle window expire; callers cap renewal by the
// current configuration, certificate and credential hard limits.
type PacketConn struct {
	conn                                             net.Conn
	read                                             func([]byte) (int, error)
	write                                            func([]byte) (int, error)
	mu                                               sync.Mutex
	readMu, writeMu                                  sync.Mutex
	until, lastActivity, readDeadline, writeDeadline time.Time
	closed                                           bool
	closeOnce                                        sync.Once
	closeErr                                         error
}

func validPacketLease(until time.Time) bool {
	now := time.Now()
	return until.After(now) && !until.After(now.Add(5*time.Minute))
}
func earlier(a, b time.Time) time.Time {
	if b.IsZero() || !a.IsZero() && a.Before(b) {
		return a
	}
	return b
}

// NewPacketConn frames a stream as uint16 big-endian length followed by payload.
// Zero-length datagrams are valid. Malformed/truncated frames or short Read
// buffers close the association rather than truncating or joining messages.
func NewPacketConn(conn net.Conn, deadline time.Time) (*PacketConn, error) {
	if conn == nil {
		return nil, errors.New("packet stream missing")
	}
	read := func(p []byte) (int, error) {
		var header [2]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			return 0, err
		}
		n := int(binary.BigEndian.Uint16(header[:]))
		if n > MaxDatagramBytes {
			return 0, errors.New("oversized datagram")
		}
		if n > len(p) {
			return 0, io.ErrShortBuffer
		}
		_, err := io.ReadFull(conn, p[:n])
		return n, err
	}
	write := func(p []byte) (int, error) {
		var header [2]byte
		size := len(p)
		if size > MaxDatagramBytes {
			return 0, errors.New("oversized datagram")
		}
		binary.BigEndian.PutUint16(header[:], uint16(size))
		if err := writePacketAll(conn, header[:]); err != nil {
			return 0, err
		}
		if err := writePacketAll(conn, p); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	return newMessageConn(conn, deadline, read, write)
}
func newMessageConn(conn net.Conn, deadline time.Time, read, write func([]byte) (int, error)) (*PacketConn, error) {
	if !validPacketLease(deadline) {
		return nil, errors.New("invalid packet lease")
	}
	p := &PacketConn{conn: conn, read: read, write: write, until: deadline, lastActivity: time.Now()}
	p.mu.Lock()
	err := p.applyDeadlinesLocked()
	p.mu.Unlock()
	if err != nil {
		_ = p.Close()
		return nil, err
	}
	return p, nil
}
func writePacketAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
func (p *PacketConn) checkLocked() error {
	now := time.Now()
	if p.closed || !p.until.After(now) || !p.lastActivity.Add(PacketIdleTimeout).After(now) {
		return net.ErrClosed
	}
	return nil
}
func (p *PacketConn) applyDeadlinesLocked() error {
	base := earlier(p.until, p.lastActivity.Add(PacketIdleTimeout))
	if err := p.conn.SetReadDeadline(earlier(base, p.readDeadline)); err != nil {
		return err
	}
	return p.conn.SetWriteDeadline(earlier(base, p.writeDeadline))
}
func (p *PacketConn) before() error { p.mu.Lock(); defer p.mu.Unlock(); return p.checkLocked() }
func (p *PacketConn) after() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkLocked(); err != nil {
		return err
	}
	p.lastActivity = time.Now()
	return p.applyDeadlinesLocked()
}
func (p *PacketConn) Read(b []byte) (int, error) {
	p.readMu.Lock()
	defer p.readMu.Unlock()
	if err := p.before(); err != nil {
		_ = p.Close()
		return 0, err
	}
	n, err := p.read(b)
	if err == nil {
		err = p.after()
	}
	if err != nil {
		_ = p.Close()
		return 0, err
	}
	return n, nil
}
func (p *PacketConn) Write(b []byte) (int, error) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if len(b) > MaxDatagramBytes {
		_ = p.Close()
		return 0, errors.New("oversized datagram")
	}
	if err := p.before(); err != nil {
		_ = p.Close()
		return 0, err
	}
	n, err := p.write(b)
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = p.after()
	}
	if err != nil {
		_ = p.Close()
		return 0, err
	}
	return n, nil
}
func (p *PacketConn) Refresh(until time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkLocked(); err != nil {
		return err
	}
	if !validPacketLease(until) {
		return errors.New("invalid packet renewal")
	}
	p.until = until
	return p.applyDeadlinesLocked()
}
func (p *PacketConn) Close() error {
	p.closeOnce.Do(func() { p.mu.Lock(); p.closed = true; p.mu.Unlock(); p.closeErr = p.conn.Close() })
	return p.closeErr
}
func (p *PacketConn) LocalAddr() net.Addr  { return p.conn.LocalAddr() }
func (p *PacketConn) RemoteAddr() net.Addr { return p.conn.RemoteAddr() }
func (p *PacketConn) SetDeadline(t time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkLocked(); err != nil {
		return err
	}
	p.readDeadline = t
	p.writeDeadline = t
	return p.applyDeadlinesLocked()
}
func (p *PacketConn) SetReadDeadline(t time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkLocked(); err != nil {
		return err
	}
	p.readDeadline = t
	return p.applyDeadlinesLocked()
}
func (p *PacketConn) SetWriteDeadline(t time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkLocked(); err != nil {
		return err
	}
	p.writeDeadline = t
	return p.applyDeadlinesLocked()
}
