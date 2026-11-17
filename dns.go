package cribrum

import (
	"context"
	"net"
	"net/netip"
	"time"

	"github.com/miekg/dns"
)

// ServeDNS forwards the query upstream, installs routes for the answer and
// only then replies, so the client never connects before its route exists.
func (s *Sieve) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	_, isTCP := w.RemoteAddr().(*net.TCPAddr)

	resp, err := s.exchange(req, isTCP)
	if err != nil {
		s.log.Warn("upstream exchange failed", "upstream", s.cfg.UpstreamDNS, "err", err)
		dns.HandleFailed(w, req)
		return
	}

	s.inject(req, resp)

	if !isTCP {
		// The upstream answer may exceed what the client can take over UDP
		// (it was possibly fetched over TCP); set TC so the client retries.
		size := dns.MinMsgSize
		if opt := req.IsEdns0(); opt != nil {
			size = int(opt.UDPSize())
		}
		resp.Truncate(size)
	}
	if err := w.WriteMsg(resp); err != nil {
		s.log.Debug("write reply failed", "err", err)
	}
}

// exchange queries upstream over the client's transport and falls back to
// TCP when a UDP answer is truncated, so no A/AAAA record is lost.
func (s *Sieve) exchange(req *dns.Msg, isTCP bool) (*dns.Msg, error) {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.UpstreamTimeout)
	defer cancel()

	if !isTCP {
		resp, _, err := s.udpClient.ExchangeContext(ctx, req, s.cfg.UpstreamDNS)
		if err != nil || !resp.Truncated {
			return resp, err
		}
	}
	resp, _, err := s.tcpClient.ExchangeContext(ctx, req, s.cfg.UpstreamDNS)
	return resp, err
}

func (s *Sieve) inject(req, resp *dns.Msg) {
	if resp.Rcode != dns.RcodeSuccess || len(req.Question) != 1 {
		return
	}
	for _, rec := range extractHosts(req.Question[0].Name, resp.Answer) {
		if !routable(rec.addr, s.cfg.AllowPrivate) {
			s.log.Debug("address skipped", "addr", rec.addr, "qname", req.Question[0].Name)
			continue
		}
		ttl := min(max(time.Duration(rec.ttl)*time.Second, s.cfg.MinTTL), s.cfg.MaxTTL)
		if err := s.routes.Ensure(rec.addr, ttl); err != nil {
			s.log.Warn("route add failed", "addr", rec.addr, "target", s.target(), "err", err)
		}
	}
}

type hostRecord struct {
	addr netip.Addr
	ttl  uint32
}

// extractHosts returns the A/AAAA records that belong to qname or to a name
// reachable from it through the CNAME chain in answer. Unrelated records an
// upstream might append are ignored. IPv4-mapped IPv6 addresses are unmapped.
func extractHosts(qname string, answer []dns.RR) []hostRecord {
	chain := map[string]bool{dns.CanonicalName(qname): true}
	for grown := true; grown; {
		grown = false
		for _, rr := range answer {
			c, ok := rr.(*dns.CNAME)
			if !ok || !chain[dns.CanonicalName(c.Hdr.Name)] {
				continue
			}
			if target := dns.CanonicalName(c.Target); !chain[target] {
				chain[target] = true
				grown = true
			}
		}
	}

	var out []hostRecord
	for _, rr := range answer {
		var ip net.IP
		switch rec := rr.(type) {
		case *dns.A:
			ip = rec.A
		case *dns.AAAA:
			ip = rec.AAAA
		default:
			continue
		}
		if !chain[dns.CanonicalName(rr.Header().Name)] {
			continue
		}
		if addr, ok := netip.AddrFromSlice(ip); ok {
			out = append(out, hostRecord{addr: addr.Unmap(), ttl: rr.Header().Ttl})
		}
	}
	return out
}

var (
	reservedPrefixes = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),   // "this network"
		netip.MustParsePrefix("240.0.0.0/4"), // reserved + limited broadcast
		netip.MustParsePrefix("100::/64"),    // IPv6 discard-only
	}
	sharedPrefixes = []netip.Prefix{
		netip.MustParsePrefix("100.64.0.0/10"), // CGNAT, also Tailscale's range
	}
)

// routable reports whether addr may become a host route. Answers such as
// 0.0.0.0 or :: from blocking resolvers, loopback or multicast must never
// reach the routing table or a BGP session.
func routable(addr netip.Addr, allowPrivate bool) bool {
	if !addr.IsValid() || addr.IsUnspecified() || addr.IsLoopback() || addr.IsMulticast() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsInterfaceLocalMulticast() {
		return false
	}
	for _, p := range reservedPrefixes {
		if p.Contains(addr) {
			return false
		}
	}
	if allowPrivate {
		return true
	}
	if addr.IsPrivate() {
		return false
	}
	for _, p := range sharedPrefixes {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}
