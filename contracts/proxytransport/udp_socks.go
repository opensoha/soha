package proxytransport

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

func loopbackAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	n, e := strconv.Atoi(port)
	ip := net.ParseIP(host)
	return err == nil && e == nil && n > 0 && n <= 65535 && ip != nil && ip.IsLoopback()
}
func readSOCKSAddress(r io.Reader) (string, error) {
	var kind [1]byte
	if _, err := io.ReadFull(r, kind[:]); err != nil {
		return "", err
	}
	return readSOCKSAddressType(r, kind[0])
}
func readSOCKSAddressType(r io.Reader, kind byte) (string, error) {
	var host string
	switch kind {
	case 1:
		b := make([]byte, 4)
		if _, err := io.ReadFull(r, b); err != nil {
			return "", err
		}
		host = net.IP(b).String()
	case 4:
		b := make([]byte, 16)
		if _, err := io.ReadFull(r, b); err != nil {
			return "", err
		}
		host = net.IP(b).String()
	case 3:
		var size [1]byte
		if _, err := io.ReadFull(r, size[:]); err != nil {
			return "", err
		}
		if size[0] == 0 || size[0] > 253 {
			return "", errors.New("invalid SOCKS hostname")
		}
		b := make([]byte, int(size[0]))
		if _, err := io.ReadFull(r, b); err != nil {
			return "", err
		}
		host = string(b)
	default:
		return "", errors.New("invalid SOCKS address type")
	}
	var port [2]byte
	if _, err := io.ReadFull(r, port[:]); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(port[:])))), nil
}
func encodeSOCKSAddress(target string, allowZero bool) ([]byte, error) {
	host, port, err := net.SplitHostPort(target)
	n, e := strconv.Atoi(port)
	if err != nil || e != nil || n < 0 || n > 65535 || !allowZero && n == 0 || host == "" || len(host) > 253 {
		return nil, errors.New("invalid SOCKS target")
	}
	var b []byte
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			b = append([]byte{1}, ip4...)
		} else {
			b = append([]byte{4}, ip.To16()...)
		}
	} else {
		if !allowZero && packetTarget(target) != nil {
			return nil, errors.New("invalid SOCKS hostname")
		}
		b = append([]byte{3, byte(len(host))}, []byte(host)...)
	}
	b = append(b, byte(n>>8), byte(n))
	return b, nil
}
func encodeSOCKSDatagram(target string, payload []byte) ([]byte, error) {
	address, err := encodeSOCKSAddress(target, false)
	if err != nil {
		return nil, err
	}
	if 3+len(address)+len(payload) > MaxDatagramBytes {
		return nil, errors.New("SOCKS datagram exceeds total packet limit")
	}
	b := make([]byte, 3, 3+len(address)+len(payload))
	b = append(b, address...)
	return append(b, payload...), nil
}
func decodeSOCKSDatagram(packet []byte) (string, []byte, error) {
	if len(packet) < 4 || len(packet) > MaxDatagramBytes || packet[0] != 0 || packet[1] != 0 || packet[2] != 0 {
		return "", nil, errors.New("invalid or fragmented SOCKS datagram")
	}
	reader := &packetByteReader{data: packet[3:]}
	target, err := readSOCKSAddress(reader)
	if err != nil || packetTarget(target) != nil {
		return "", nil, errors.New("invalid SOCKS datagram target")
	}
	return target, reader.data, nil
}

type packetByteReader struct{ data []byte }

func (r *packetByteReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}
func socksClientAuth(c net.Conn, user, secret string) error {
	if user == "" || len(user) > 255 || len(secret) < 32 || len(secret) > 255 {
		return errors.New("invalid SOCKS credential size")
	}
	if err := writePacketAll(c, []byte{5, 1, 2}); err != nil {
		return err
	}
	var reply [2]byte
	if _, err := io.ReadFull(c, reply[:]); err != nil {
		return err
	}
	if reply != [2]byte{5, 2} {
		return errors.New("SOCKS password authentication required")
	}
	b := []byte{1, byte(len(user))}
	b = append(b, []byte(user)...)
	b = append(b, byte(len(secret)))
	b = append(b, []byte(secret)...)
	if err := writePacketAll(c, b); err != nil {
		return err
	}
	if _, err := io.ReadFull(c, reply[:]); err != nil {
		return err
	}
	if reply != [2]byte{1, 0} {
		return errors.New("SOCKS authentication rejected")
	}
	return nil
}
func socksReply(c net.Conn, code byte, address string) error {
	encoded, err := encodeSOCKSAddress(address, true)
	if err != nil {
		return err
	}
	return writePacketAll(c, append([]byte{5, code, 0}, encoded...))
}

type socksUDPTransport struct {
	udp     *net.UDPConn
	control net.Conn
	once    sync.Once
	err     error
	target  string
}

func (c *socksUDPTransport) Read(p []byte) (int, error) {
	b := make([]byte, 65536)
	n, err := c.udp.Read(b)
	if err != nil {
		return 0, err
	}
	target, payload, err := decodeSOCKSDatagram(b[:n])
	if err != nil {
		return 0, err
	}
	if target != c.target {
		return 0, errors.New("SOCKS UDP response changed pinned target")
	}
	if len(payload) > len(p) {
		return 0, io.ErrShortBuffer
	}
	return copy(p, payload), nil
}
func (c *socksUDPTransport) Write(p []byte) (int, error) {
	b, err := encodeSOCKSDatagram(c.target, p)
	if err != nil {
		return 0, err
	}
	n, err := c.udp.Write(b)
	if err != nil {
		return 0, err
	}
	if n != len(b) {
		return 0, io.ErrShortWrite
	}
	return len(p), nil
}
func (c *socksUDPTransport) Close() error {
	c.once.Do(func() { c.err = errors.Join(c.udp.Close(), c.control.Close()) })
	return c.err
}
func (c *socksUDPTransport) LocalAddr() net.Addr  { return c.udp.LocalAddr() }
func (c *socksUDPTransport) RemoteAddr() net.Addr { return c.udp.RemoteAddr() }
func (c *socksUDPTransport) SetDeadline(t time.Time) error {
	return errors.Join(c.udp.SetDeadline(t), c.control.SetDeadline(t))
}
func (c *socksUDPTransport) SetReadDeadline(t time.Time) error {
	return errors.Join(c.udp.SetReadDeadline(t), c.control.SetReadDeadline(t))
}
func (c *socksUDPTransport) SetWriteDeadline(t time.Time) error {
	return errors.Join(c.udp.SetWriteDeadline(t), c.control.SetWriteDeadline(t))
}

// DialSOCKS5UDP uses an installer-owned loopback SOCKS server. target MUST be a
// previously authorized literal pinned IP:port. Only the relay's loopback UDP
// socket is dialled by the OS; the actual target is reached by the owned engine.
func DialSOCKS5UDP(ctx context.Context, backendAddr, user, secret, target string, deadline time.Time) (net.Conn, error) {
	host, _, err := net.SplitHostPort(target)
	if err != nil || net.ParseIP(host) == nil || packetTarget(target) != nil || !loopbackAddress(backendAddr) || !validPacketLease(deadline) {
		return nil, errors.New("invalid owned SOCKS UDP endpoint or target")
	}
	until := earlier(deadline, time.Now().Add(5*time.Second))
	if d, ok := ctx.Deadline(); ok {
		until = earlier(until, d)
	}
	control, err := (&net.Dialer{Deadline: until}).DialContext(ctx, "tcp", backendAddr)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = control.Close()
		}
	}()
	stop := context.AfterFunc(ctx, func() { _ = control.Close() })
	defer stop()
	if err := control.SetDeadline(until); err != nil {
		return nil, err
	}
	if err := socksClientAuth(control, user, secret); err != nil {
		return nil, err
	}
	address, _ := encodeSOCKSAddress("0.0.0.0:0", true)
	if err := writePacketAll(control, append([]byte{5, 3, 0}, address...)); err != nil {
		return nil, err
	}
	var reply [3]byte
	if _, err := io.ReadFull(control, reply[:]); err != nil {
		return nil, err
	}
	if reply == [3]byte{5, 7, 0} {
		return nil, ErrPacketUnsupported
	}
	if reply != [3]byte{5, 0, 0} {
		return nil, errors.New("SOCKS UDP association rejected")
	}
	relay, err := readSOCKSAddress(control)
	if err != nil || !loopbackAddress(relay) {
		return nil, errors.New("SOCKS UDP relay is not loopback")
	}
	remote, err := net.ResolveUDPAddr("udp", relay)
	if err != nil {
		return nil, err
	}
	udp, err := net.DialUDP("udp", nil, remote)
	if err != nil {
		return nil, err
	}
	// Normalize literal spellings so a response cannot switch addresses/ports.
	canonical, _ := encodeSOCKSAddress(target, false)
	reader := &packetByteReader{data: canonical}
	target, err = readSOCKSAddress(reader)
	if err != nil {
		_ = udp.Close()
		return nil, err
	}
	transport := &socksUDPTransport{udp: udp, control: control, target: target}
	packet, err := newMessageConn(transport, deadline, transport.Read, transport.Write)
	if err != nil {
		_ = transport.Close()
		return nil, err
	}
	success = true
	go func() { _, _ = io.Copy(io.Discard, control); _ = packet.Close() }()
	return packet, nil
}
