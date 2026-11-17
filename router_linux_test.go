//go:build linux

package cribrum

import (
	"net"
	"net/netip"
	"os"
	"runtime"
	"slices"
	"testing"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

func TestNetlinkRouteKey(t *testing.T) {
	r := &netlinkRouter{table: 100, proto: 201}
	tests := []struct {
		addr     string
		wantOnes int
		wantBits int
	}{
		{"1.1.1.1", 32, 32},
		{"2606:4700:4700::1111", 128, 128},
	}
	for _, tt := range tests {
		rt := r.route(netip.MustParseAddr(tt.addr))
		ones, bits := rt.Dst.Mask.Size()
		if ones != tt.wantOnes || bits != tt.wantBits {
			t.Errorf("%s: mask /%d (bits %d), want /%d (bits %d)", tt.addr, ones, bits, tt.wantOnes, tt.wantBits)
		}
		if len(rt.Dst.IP) != tt.wantBits/8 {
			t.Errorf("%s: IP length %d, want %d", tt.addr, len(rt.Dst.IP), tt.wantBits/8)
		}
		if rt.Table != 100 || rt.Protocol != netlink.RouteProtocol(201) {
			t.Errorf("%s: table/proto = %d/%d, want 100/201", tt.addr, rt.Table, rt.Protocol)
		}
	}
}

// inNetns runs the test body inside a fresh network namespace so the host
// routing tables are never touched. Requires root (CAP_NET_ADMIN, CAP_SYS_ADMIN).
func inNetns(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root; CI runs it via sudo")
	}
	runtime.LockOSThread()
	orig, err := netns.Get()
	if err != nil {
		t.Fatal(err)
	}
	ns, err := netns.New() // also switches the current thread into it
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = netns.Set(orig)
		ns.Close()
		orig.Close()
		runtime.UnlockOSThread()
	})
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		t.Fatal(err)
	}
	if err := netlink.LinkSetUp(lo); err != nil {
		t.Fatal(err)
	}
}

func TestNetlinkRouterKernel(t *testing.T) {
	for _, iface := range []string{"", "lo"} {
		t.Run("iface="+iface, func(t *testing.T) {
			inNetns(t)

			cfg := DefaultConfig()
			cfg.OutIface = iface
			r, err := newRouter(cfg)
			if err != nil {
				t.Fatal(err)
			}
			v4 := netip.MustParseAddr("203.0.113.10")
			v6 := netip.MustParseAddr("2001:db8::10")

			// A foreign route to the same prefix (other protocol, other metric)
			// must stay invisible to List and survive cribrum's Delete.
			foreign := &netlink.Route{
				Dst:      &net.IPNet{IP: v4.AsSlice(), Mask: net.CIDRMask(32, 32)},
				Table:    cfg.RouteTable,
				Protocol: unix.RTPROT_STATIC,
				Type:     unix.RTN_BLACKHOLE,
				Priority: 50,
			}
			if err := netlink.RouteAdd(foreign); err != nil {
				t.Fatalf("add foreign route: %v", err)
			}

			for _, a := range []netip.Addr{v4, v6} {
				if err := r.Replace(a); err != nil {
					t.Fatalf("Replace(%s): %v", a, err)
				}
				if err := r.Replace(a); err != nil { // idempotent
					t.Fatalf("second Replace(%s): %v", a, err)
				}
			}
			got, err := r.List()
			if err != nil {
				t.Fatal(err)
			}
			slices.SortFunc(got, netip.Addr.Compare)
			if !slices.Equal(got, []netip.Addr{v4, v6}) {
				t.Fatalf("List() = %v, want [%s %s]", got, v4, v6)
			}

			for _, a := range []netip.Addr{v4, v6} {
				if err := r.Delete(a); err != nil {
					t.Fatalf("Delete(%s): %v", a, err)
				}
				if err := r.Delete(a); err != nil { // already gone is fine
					t.Fatalf("second Delete(%s): %v", a, err)
				}
			}
			if got, _ := r.List(); len(got) != 0 {
				t.Fatalf("List() after delete = %v", got)
			}
			left, err := netlink.RouteListFiltered(netlink.FAMILY_V4,
				&netlink.Route{Table: cfg.RouteTable, Protocol: unix.RTPROT_STATIC},
				netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)
			if err != nil || len(left) != 1 {
				t.Fatalf("foreign route lost: %v (err %v)", left, err)
			}
		})
	}
}
