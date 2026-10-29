package networkgateway

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/opensoha/soha/internal/networkprotocol"
	"golang.org/x/net/dns/dnsmessage"
)

type siteDNSReporter interface {
	ReportDNS(context.Context, networkprotocol.DNSObservationBatch) error
}

func (c *ControlClient) ReportDNS(ctx context.Context, batch networkprotocol.DNSObservationBatch) error {
	_, raw, err := c.runtimeMessage(networkprotocol.MessageDNSObservations, batch)
	if err != nil {
		return err
	}
	return c.post(ctx, c.origin+"/api/network-control/v1/runtimes/"+url.PathEscape(c.runtimeID)+"/dns:observations", raw, "", http.StatusNoContent)
}

func (r *Runtime) observeDNS(ctx context.Context, desired networkprotocol.ConfigurationDesired) {
	if len(desired.DNSRequests) == 0 {
		return
	}
	reporter, ok := r.control.(siteDNSReporter)
	if !ok {
		return
	}
	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	observations := r.collectDNSObservations(queryCtx, desired.DNSRequests)
	cancel()
	if len(observations) == 0 {
		return
	}
	reportCtx, reportCancel := context.WithTimeout(ctx, 2*time.Second)
	defer reportCancel()
	batch := networkprotocol.DNSObservationBatch{ConfigurationVersion: desired.ConfigurationVersion, PolicyVersion: desired.PolicyVersion, Observations: observations}
	if err := reporter.ReportDNS(reportCtx, batch); err != nil {
		r.logger.Warn("site DNS report failed", "event", "network_gateway.dns.report_failed")
	}

}

func resolveSiteHostname(ctx context.Context, request networkprotocol.DNSResolutionRequest) ([]string, int, error) {
	if len(request.Resolvers) == 0 || len(request.Resolvers) > 8 || len(request.Hostname) > 253 {
		return nil, 0, fmt.Errorf("invalid site DNS request")
	}
	for _, resolver := range request.Resolvers {
		address, err := netip.ParseAddr(resolver)
		if err != nil || !address.IsGlobalUnicast() || address.Is4In6() {
			continue
		}
		addresses := []string{}
		ttl := uint32(300)
		valid := true
		for _, kind := range []dnsmessage.Type{dnsmessage.TypeA, dnsmessage.TypeAAAA} {
			values, lifetime, err := resolveSiteType(ctx, net.JoinHostPort(address.String(), "53"), strings.TrimSuffix(request.Hostname, ".")+".", kind, map[string]bool{})
			if err != nil {
				valid = false
				break
			}
			addresses = append(addresses, values...)
			ttl = min(ttl, lifetime)
		}
		if valid && len(addresses) > 0 && len(addresses) <= 32 && ttl > 0 {
			slices.Sort(addresses)
			return slices.Compact(addresses), int(ttl), nil
		}
	}
	return nil, 0, fmt.Errorf("site DNS resolution unavailable")
}

func resolveSiteType(ctx context.Context, resolver, hostname string, kind dnsmessage.Type, seen map[string]bool) ([]string, uint32, error) {
	hostname = strings.ToLower(hostname)
	if seen[hostname] || len(seen) >= 8 {
		return nil, 0, fmt.Errorf("site DNS alias cycle or limit")
	}
	seen[hostname] = true
	message, err := querySiteDNS(ctx, resolver, hostname, kind)
	if err != nil {
		return nil, 0, err
	}
	values := []string{}
	ttl := uint32(300)
	alias := ""
	for _, answer := range message.Answers {
		if !strings.EqualFold(answer.Header.Name.String(), hostname) || answer.Header.Class != dnsmessage.ClassINET {
			continue
		}
		switch body := answer.Body.(type) {
		case *dnsmessage.AResource:
			if kind == dnsmessage.TypeA {
				values = append(values, netip.AddrFrom4(body.A).String())
				ttl = min(ttl, answer.Header.TTL)
			}
		case *dnsmessage.AAAAResource:
			if kind == dnsmessage.TypeAAAA {
				values = append(values, netip.AddrFrom16(body.AAAA).String())
				ttl = min(ttl, answer.Header.TTL)
			}
		case *dnsmessage.CNAMEResource:
			if alias != "" && alias != body.CNAME.String() {
				return nil, 0, fmt.Errorf("ambiguous site DNS alias")
			}
			alias = body.CNAME.String()
			ttl = min(ttl, answer.Header.TTL)
		}
	}
	if alias != "" {
		if len(values) > 0 {
			return nil, 0, fmt.Errorf("ambiguous site DNS answer")
		}
		resolved, lifetime, err := resolveSiteType(ctx, resolver, alias, kind, seen)
		return resolved, min(ttl, lifetime), err
	}
	return values, ttl, nil
}

func querySiteDNS(ctx context.Context, resolver, hostname string, kind dnsmessage.Type) (dnsmessage.Message, error) {
	var result dnsmessage.Message
	name, err := dnsmessage.NewName(hostname)
	if err != nil {
		return result, err
	}
	var random [2]byte
	if _, err := rand.Read(random[:]); err != nil {
		return result, err
	}
	id := binary.BigEndian.Uint16(random[:])
	query := dnsmessage.Message{Header: dnsmessage.Header{ID: id, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: name, Type: kind, Class: dnsmessage.ClassINET}}}
	raw, err := query.Pack()
	if err != nil {
		return result, err
	}
	conn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", resolver)
	if err != nil {
		return result, err
	}
	defer func() { _ = conn.Close() }()
	deadline := time.Now().Add(2 * time.Second)
	if value, ok := ctx.Deadline(); ok {
		deadline = minTimeDNS(deadline, value)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return result, err
	}
	packet := make([]byte, len(raw)+2)
	binary.BigEndian.PutUint16(packet, uint16(len(raw)))
	copy(packet[2:], raw)
	if _, err := io.Copy(conn, strings.NewReader(string(packet))); err != nil {
		return result, err
	}
	var size [2]byte
	if _, err := io.ReadFull(conn, size[:]); err != nil {
		return result, err
	}
	length := int(binary.BigEndian.Uint16(size[:]))
	if length < 12 || length > 16384 {
		return result, fmt.Errorf("site DNS response size invalid")
	}
	response := make([]byte, length)
	if _, err := io.ReadFull(conn, response); err != nil {
		return result, err
	}
	if err := result.Unpack(response); err != nil {
		return result, err
	}
	if result.ID != id || !result.Response || result.Truncated || result.RCode != dnsmessage.RCodeSuccess || len(result.Questions) != 1 || result.Questions[0] != query.Questions[0] {
		return result, fmt.Errorf("site DNS response mismatch")
	}
	return result, nil
}

func minTimeDNS(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (r *Runtime) collectDNSObservations(ctx context.Context, requests []networkprotocol.DNSResolutionRequest) []networkprotocol.DNSResolutionObservation {
	if len(requests) > 128 {
		return nil
	}
	jobs := make(chan networkprotocol.DNSResolutionRequest, len(requests))
	results := make(chan networkprotocol.DNSResolutionObservation, len(requests))
	for _, request := range requests {
		jobs <- request
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(16, len(requests)) {
		workers.Go(func() {
			for request := range jobs {
				if ctx.Err() != nil {
					return
				}
				lookup, cancel := context.WithTimeout(ctx, time.Second)
				addresses, ttl, err := resolveSiteHostname(lookup, request)
				cancel()
				if err == nil {
					results <- networkprotocol.DNSResolutionObservation{ResourceID: request.ResourceID, Hostname: request.Hostname, Addresses: addresses, TTLSeconds: ttl, ObservedAt: r.now().UTC()}
				}
			}
		})
	}
	workers.Wait()
	close(results)
	observations := []networkprotocol.DNSResolutionObservation{}
	for result := range results {
		observations = append(observations, result)
	}
	slices.SortFunc(observations, func(a, b networkprotocol.DNSResolutionObservation) int {
		return strings.Compare(a.ResourceID, b.ResourceID)
	})
	return observations
}
