package cribrum

import (
	"net"
	"testing"

	"golang.org/x/sys/unix"
)

func TestHostMask(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		wantOnes int
		wantBits int
	}{
		{
			name:     "IPv4 address",
			ip:       "1.1.1.1",
			wantOnes: 32,
			wantBits: 32,
		},
		{
			name:     "IPv6 address",
			ip:       "2606:4700:4700::1111",
			wantOnes: 128,
			wantBits: 128,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := net.ParseIP(tt.ip)
			mask := hostMask(parsed)
			ones, bits := mask.Size()
			if ones != tt.wantOnes || bits != tt.wantBits {
				t.Errorf("hostMask(%s) = /%d (bits %d), want /%d (bits %d)",
					tt.ip, ones, bits, tt.wantOnes, tt.wantBits)
			}
		})
	}
}

func TestNewValidation(t *testing.T) {
	s, err := New(Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.cfg.RouteTable != 100 {
		t.Errorf("default RouteTable = %d, want 100", s.cfg.RouteTable)
	}
	if s.cfg.OutIface != "" {
		t.Errorf("default OutIface = %q, want empty (blackhole)", s.cfg.OutIface)
	}

	sCustom, err := New(Config{OutIface: "dummy0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sCustom.cfg.OutIface != "dummy0" {
		t.Errorf("OutIface = %q, want %q", sCustom.cfg.OutIface, "dummy0")
	}
}

func TestBuildRouteBlackhole(t *testing.T) {
	s, err := New(Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	route, target, err := s.buildRoute(net.ParseIP("104.18.12.34"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != "blackhole" {
		t.Errorf("target = %q, want %q", target, "blackhole")
	}
	if route.Type != unix.RTN_BLACKHOLE {
		t.Errorf("route.Type = %d, want %d (RTN_BLACKHOLE)", route.Type, unix.RTN_BLACKHOLE)
	}
	if route.LinkIndex != 0 {
		t.Errorf("route.LinkIndex = %d, want 0 for blackhole", route.LinkIndex)
	}
}
