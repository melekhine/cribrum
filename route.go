package cribrum

import (
	"log"
	"net"
	"time"

	"golang.org/x/sys/unix"

	"github.com/vishvananda/netlink"
)

// hostMask returns a /32 mask for IPv4 or /128 mask for IPv6 addresses.
func hostMask(ip net.IP) net.IPMask {
	if ip.To4() != nil {
		return net.CIDRMask(32, 32)
	}
	return net.CIDRMask(128, 128)
}

// injectRoute adds or updates a host route in the configured kernel routing table.
func (s *Sieve) injectRoute(ip net.IP, ttl uint32) {
	if ttl < s.cfg.MinTTL {
		ttl = s.cfg.MinTTL
	}
	expireAt := time.Now().Add(time.Duration(ttl) * time.Second)
	ipStr := ip.String()

	if s.trackRoute(ipStr, expireAt) {
		return
	}

	mask := hostMask(ip)
	ones, _ := mask.Size()

	route, target, err := s.buildRoute(ip)
	if err != nil {
		log.Printf("cribrum: build route failed (%s): %v", ipStr, err)
		s.untrackRoute(ipStr)
		return
	}

	if err := netlink.RouteReplace(route); err != nil {
		log.Printf("cribrum: route replace failed (%s/%d): %v", ipStr, ones, err)
		s.untrackRoute(ipStr)
		return
	}

	log.Printf("[+] %s/%d -> %s (table %d, ttl %ds)", ipStr, ones, target, s.cfg.RouteTable, ttl)
}

// removeRoute deletes an expired host route from the kernel routing table.
func (s *Sieve) removeRoute(ipStr string) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return
	}

	mask := hostMask(ip)
	ones, _ := mask.Size()

	route := &netlink.Route{
		Dst:   &net.IPNet{IP: ip, Mask: mask},
		Table: s.cfg.RouteTable,
	}
	if s.cfg.OutIface == "" {
		route.Type = unix.RTN_BLACKHOLE
	}

	if err := netlink.RouteDel(route); err == nil {
		log.Printf("[-] %s/%d removed (table %d)", ipStr, ones, s.cfg.RouteTable)
	}
}

func (s *Sieve) buildRoute(ip net.IP) (*netlink.Route, string, error) {
	mask := hostMask(ip)
	route := &netlink.Route{
		Dst:      &net.IPNet{IP: ip, Mask: mask},
		Table:    s.cfg.RouteTable,
		Protocol: unix.RTPROT_STATIC,
	}

	if s.cfg.OutIface == "" {
		route.Type = unix.RTN_BLACKHOLE
		return route, "blackhole", nil
	}

	link, err := netlink.LinkByName(s.cfg.OutIface)
	if err != nil {
		return nil, "", err
	}
	route.LinkIndex = link.Attrs().Index
	return route, s.cfg.OutIface, nil
}

func (s *Sieve) trackRoute(ipStr string, expireAt time.Time) (alreadyTracked bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existingExpire, exists := s.routeCache[ipStr]
	if exists && existingExpire.After(expireAt) {
		return true
	}
	s.routeCache[ipStr] = expireAt
	return exists
}

func (s *Sieve) untrackRoute(ipStr string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.routeCache, ipStr)
}

func (s *Sieve) drainCache() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	active := make([]string, 0, len(s.routeCache))
	for ipStr := range s.routeCache {
		active = append(active, ipStr)
	}
	clear(s.routeCache)
	return active
}

// Flush removes all active routes tracked by this instance from the kernel table.
func (s *Sieve) Flush() {
	active := s.drainCache()
	if len(active) == 0 {
		return
	}

	log.Printf("cribrum: flushing %d active routes from table %d...", len(active), s.cfg.RouteTable)
	for _, ipStr := range active {
		s.removeRoute(ipStr)
	}
}
