package cribrum

import (
	"net"
	"testing"
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
	_, err := New(Config{OutIface: ""})
	if err == nil {
		t.Fatal("expected error when OutIface is empty, got nil")
	}

	s, err := New(Config{OutIface: "tailscale0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.cfg.RouteTable != 100 {
		t.Errorf("default RouteTable = %d, want 100", s.cfg.RouteTable)
	}
}
