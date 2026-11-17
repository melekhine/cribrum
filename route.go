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

	s.mu.Lock()
	defer s.mu.Unlock()
	existingExpire, exists := s.routeCache[ipStr]
	if exists && existingExpire.After(expireAt) {
		return
	}
	s.routeCache[ipStr] = expireAt

	if exists {
		return
	}

	link, err := netlink.LinkByName(s.cfg.OutIface)
	if err != nil {
		log.Printf("cribrum: interface lookup failed (%s): %v", s.cfg.OutIface, err)
		return
	}

	mask := hostMask(ip)
	ones, _ := mask.Size()

	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       &net.IPNet{IP: ip, Mask: mask},
		Table:     s.cfg.RouteTable,
		Protocol:  unix.RTPROT_STATIC,
	}

	if err := netlink.RouteReplace(route); err != nil {
		log.Printf("cribrum: route replace failed (%s/%d): %v", ipStr, ones, err)
		return
	}

	log.Printf("[+] %s/%d -> %s (table %d, ttl %ds)", ipStr, ones, s.cfg.OutIface, s.cfg.RouteTable, ttl)
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

	if err := netlink.RouteDel(route); err == nil {
		log.Printf("[-] %s/%d removed (table %d)", ipStr, ones, s.cfg.RouteTable)
	}
}

// Flush removes all active routes tracked by this instance from the kernel table.
func (s *Sieve) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	active := make([]string, 0, len(s.routeCache))
	for ipStr := range s.routeCache {
		active = append(active, ipStr)
		delete(s.routeCache, ipStr)
	}

	if len(active) == 0 {
		return
	}

	log.Printf("cribrum: flushing %d active routes from table %d...", len(active), s.cfg.RouteTable)
	for _, ipStr := range active {
		s.removeRoute(ipStr)
	}
}
