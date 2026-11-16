package cribrum

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Config holds configuration parameters for the Sieve instance.
type Config struct {
	ListenAddr  string
	UpstreamDNS string
	RouteTable  int
	OutIface    string
	MinTTL      uint32
}

// Sieve represents the core DNS-to-Netlink route injector.
type Sieve struct {
	cfg        Config
	dnsClient  *dns.Client
	routeCache map[string]time.Time
	mu         sync.Mutex
}

// New initializes and returns a new Sieve instance.
func New(cfg Config) (*Sieve, error) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:5353"
	}
	if cfg.UpstreamDNS == "" {
		cfg.UpstreamDNS = "1.1.1.1:53"
	}
	if cfg.RouteTable == 0 {
		cfg.RouteTable = 100
	}
	if cfg.OutIface == "" {
		return nil, fmt.Errorf("cribrum: egress interface must be specified")
	}
	if cfg.MinTTL == 0 {
		cfg.MinTTL = 300
	}

	return &Sieve{
		cfg: cfg,
		dnsClient: &dns.Client{
			Timeout: 3 * time.Second,
		},
		routeCache: make(map[string]time.Time),
	}, nil
}

// Run starts the DNS proxy server and the background route cleaner until ctx is canceled.
func (s *Sieve) Run(ctx context.Context) error {
	mux := dns.NewServeMux()
	mux.HandleFunc(".", s.handleDNS)

	server := &dns.Server{
		Addr:    s.cfg.ListenAddr,
		Net:     "udp",
		Handler: mux,
	}

	go s.startCleaner(ctx)

	errCh := make(chan error, 1)
	go func() {
		log.Printf("cribrum %s active: %s -> %s (table %d, dev %s)",
			BuildInfo(), s.cfg.ListenAddr, s.cfg.UpstreamDNS, s.cfg.RouteTable, s.cfg.OutIface)
		if err := server.ListenAndServe(); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Println("cribrum: shutting down server...")
		err := server.Shutdown()
		s.Flush()
		return err
	case err := <-errCh:
		s.Flush()
		return err
	}
}
