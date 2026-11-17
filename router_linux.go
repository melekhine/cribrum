//go:build linux

package cribrum

import (
	"errors"
	"fmt"
	"net"
	"net/netip"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// netlinkRouter installs /32 and /128 routes into one kernel table, tagged
// with a dedicated rtm_protocol so only cribrum's own routes are touched.
type netlinkRouter struct {
	table int
	proto netlink.RouteProtocol
	iface string // "" means blackhole
}

func newRouter(cfg Config) (router, error) {
	return &netlinkRouter{
		table: cfg.RouteTable,
		proto: netlink.RouteProtocol(cfg.RouteProto),
		iface: cfg.OutIface,
	}, nil
}

// route returns the key fields shared by add, delete and list requests.
func (r *netlinkRouter) route(addr netip.Addr) *netlink.Route {
	bits := addr.BitLen()
	return &netlink.Route{
		Dst:      &net.IPNet{IP: addr.AsSlice(), Mask: net.CIDRMask(bits, bits)},
		Table:    r.table,
		Protocol: r.proto,
	}
}

func (r *netlinkRouter) Replace(addr netip.Addr) error {
	rt := r.route(addr)
	if r.iface == "" {
		rt.Type = unix.RTN_BLACKHOLE
	} else {
		// Resolved on every call: the interface index changes whenever a
		// tunnel is recreated, and this only runs once per new address.
		link, err := netlink.LinkByName(r.iface)
		if err != nil {
			return fmt.Errorf("lookup %s: %w", r.iface, err)
		}
		rt.LinkIndex = link.Attrs().Index
	}
	if err := netlink.RouteReplace(rt); err != nil {
		return fmt.Errorf("replace %s: %w", rt.Dst, err)
	}
	return nil
}

func (r *netlinkRouter) Delete(addr netip.Addr) error {
	rt := r.route(addr)
	// The kernel matches rtm_protocol on delete, so foreign routes survive.
	if err := netlink.RouteDel(rt); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("delete %s: %w", rt.Dst, err)
	}
	return nil
}

func (r *netlinkRouter) List() ([]netip.Addr, error) {
	filter := &netlink.Route{Table: r.table, Protocol: r.proto}
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_ALL, filter,
		netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)
	if err != nil {
		return nil, fmt.Errorf("list table %d: %w", r.table, err)
	}
	addrs := make([]netip.Addr, 0, len(routes))
	for _, rt := range routes {
		if rt.Dst == nil {
			continue
		}
		if ones, bits := rt.Dst.Mask.Size(); ones != bits {
			continue
		}
		if addr, ok := netip.AddrFromSlice(rt.Dst.IP); ok {
			addrs = append(addrs, addr.Unmap())
		}
	}
	return addrs, nil
}
