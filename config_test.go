package cribrum

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultConfigIsValid(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.OutIface != "" {
		t.Errorf("default OutIface = %q, want empty (blackhole)", cfg.OutIface)
	}
	if cfg.RouteTable != DefaultRouteTable {
		t.Errorf("default RouteTable = %d, want %d", cfg.RouteTable, DefaultRouteTable)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"listen without port", func(c *Config) { c.ListenAddr = "127.0.0.1" }, "listen"},
		{"listen hostname", func(c *Config) { c.ListenAddr = "localhost:53" }, "IP literal"},
		{"upstream without port", func(c *Config) { c.UpstreamDNS = "1.1.1.1" }, "upstream"},
		{"main table", func(c *Config) { c.RouteTable = 254 }, "reserved"},
		{"local table", func(c *Config) { c.RouteTable = 255 }, "reserved"},
		{"negative table", func(c *Config) { c.RouteTable = -1 }, "out of range"},
		{"kernel proto", func(c *Config) { c.RouteProto = 4 }, "protocol"},
		{"proto overflow", func(c *Config) { c.RouteProto = 256 }, "protocol"},
		{"sub-second ttl", func(c *Config) { c.MinTTL = time.Millisecond }, "min TTL"},
		{"max below min", func(c *Config) { c.MaxTTL = time.Second }, "max TTL"},
		{"large table ok", func(c *Config) { c.RouteTable = 1000 }, ""},
		{"ipv6 listen ok", func(c *Config) { c.ListenAddr = "[::1]:5354" }, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.mutate(&cfg)
			err := cfg.Validate()
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("Validate() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestBuildInfo(t *testing.T) {
	if got := BuildInfo(); !strings.HasPrefix(got, "cribrum ") {
		t.Fatalf("BuildInfo() = %q", got)
	}
}

func TestWriteCompletion(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		var b strings.Builder
		if err := WriteCompletion(&b, shell); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), "-max-ttl") {
			t.Errorf("%s completion misses -max-ttl", shell)
		}
	}
	if err := WriteCompletion(&strings.Builder{}, "fish"); err == nil {
		t.Error("expected error for unsupported shell")
	}
}
