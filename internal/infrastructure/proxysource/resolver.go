package proxysource

import (
	"context"
	"net"
	"net/netip"
	"time"
)

type Resolver struct{}

func (Resolver) Resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}
