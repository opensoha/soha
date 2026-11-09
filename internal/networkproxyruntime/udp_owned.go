package networkproxyruntime

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

// Registry includes connected-but-not-yet-admitted packet sockets. Closing a
// transport cannot report zero while an earlier dial may still create a socket.
type ownedPackets struct {
	mu          sync.Mutex
	closed      bool
	connections map[*ownedPacketConn]struct{}
	pending     sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	closeErr    error
}

func newOwnedPackets() *ownedPackets {
	ctx, cancel := context.WithCancel(context.Background())
	return &ownedPackets{connections: map[*ownedPacketConn]struct{}{}, ctx: ctx, cancel: cancel}
}

func (o *ownedPackets) dial(ctx context.Context, dial func(context.Context) (net.Conn, error), authorize func(context.Context) error) (net.Conn, error) {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return nil, net.ErrClosed
	}
	o.pending.Add(1)
	o.mu.Unlock()
	defer o.pending.Done()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(o.ctx, cancel)
	defer stop()
	c, err := dial(ctx)
	if err != nil {
		return nil, err
	}
	owned := &ownedPacketConn{Conn: c, owner: o, authorize: authorize}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		if closeErr := c.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			o.closeErr = errors.Join(o.closeErr, closeErr)
		}
		return nil, net.ErrClosed
	}
	o.connections[owned] = struct{}{}
	return owned, nil
}

func (o *ownedPackets) close() error {
	o.mu.Lock()
	o.closed = true
	o.cancel()
	connections := make([]*ownedPacketConn, 0, len(o.connections))
	for c := range o.connections {
		connections = append(connections, c)
	}
	o.mu.Unlock()
	for _, c := range connections {
		_ = c.Close()
	}
	o.pending.Wait()
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.closeErr
}

type ownedPacketConn struct {
	net.Conn
	owner     *ownedPackets
	authorize func(context.Context) error
	once      sync.Once
	closeErr  error
}

func (c *ownedPacketConn) check() error {
	ctx, cancel := context.WithTimeout(c.owner.ctx, 3*time.Second)
	defer cancel()
	return c.authorize(ctx)
}
func (c *ownedPacketConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 || err == nil {
		if denied := c.check(); denied != nil {
			_ = c.Close()
			return 0, denied
		}
	}
	if err != nil {
		_ = c.Close()
	}
	return n, err
}
func (c *ownedPacketConn) Write(p []byte) (int, error) {
	if err := c.check(); err != nil {
		_ = c.Close()
		return 0, err
	}
	return c.Conn.Write(p)
}
func (c *ownedPacketConn) Refresh(until time.Time) error {
	if r, ok := c.Conn.(interface{ Refresh(time.Time) error }); ok {
		return r.Refresh(until)
	}
	return c.Conn.SetDeadline(until)
}
func (c *ownedPacketConn) Close() error {
	c.once.Do(func() {
		c.closeErr = c.Conn.Close()
		c.owner.mu.Lock()
		delete(c.owner.connections, c)
		if c.closeErr != nil && !errors.Is(c.closeErr, net.ErrClosed) {
			c.owner.closeErr = errors.Join(c.owner.closeErr, c.closeErr)
		}
		c.owner.mu.Unlock()
	})
	return c.closeErr
}

func (o *ownedPackets) activeCount() int { o.mu.Lock(); defer o.mu.Unlock(); return len(o.connections) }
