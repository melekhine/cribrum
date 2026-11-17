package cribrum

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Default configuration values.
const (
	DefaultListenAddr      = "127.0.0.1:5354"
	DefaultUpstreamDNS     = "127.0.1.1:53"
	DefaultRouteTable      = 100
	DefaultRouteProto      = 201
	DefaultMinTTL          = 5 * time.Minute
	DefaultMaxTTL          = 24 * time.Hour
	DefaultUpstreamTimeout = 3 * time.Second
	DefaultSyncInterval    = 30 * time.Second

	shutdownTimeout = 5 * time.Second
)

// Config holds configuration parameters for the Sieve instance.
// Zero-valued fields are replaced with the corresponding Default* value.
type Config struct {
	// ListenAddr is the local address served over both UDP and TCP.
	ListenAddr string
	// UpstreamDNS is the resolver queries are forwarded to (host:port).
	UpstreamDNS string
	// RouteTable is the kernel routing table routes are injected into.
	// Reserved tables (0, 253 default, 254 main, 255 local) are rejected.
	RouteTable int
	// RouteProto is the rtm_protocol value that marks routes owned by cribrum.
	// It lets cribrum recognise its own routes after a restart and never
	// touch routes created by anyone else. Values 0-4 are kernel-reserved.
	RouteProto int
	// OutIface is the egress interface. Empty means blackhole routes.
	OutIface string
	// MinTTL and MaxTTL clamp the DNS TTL into the route lifetime.
	MinTTL time.Duration
	MaxTTL time.Duration
	// AllowPrivate permits routes to RFC 1918, CGNAT (100.64/10) and ULA
	// addresses. Non-routable addresses (loopback, multicast, link-local,
	// unspecified, reserved) are always rejected.
	AllowPrivate bool
	// UpstreamTimeout bounds a single upstream exchange.
	UpstreamTimeout time.Duration
	// SyncInterval is how often expired routes are purged and the
	// kernel table is reconciled with the in-memory state.
	SyncInterval time.Duration
	// Logger defaults to slog.Default().
	Logger *slog.Logger
}

// DefaultConfig returns a Config populated with default values.
func DefaultConfig() Config {
	return Config{}.withDefaults()
}

func (c Config) withDefaults() Config {
	if c.ListenAddr == "" {
		c.ListenAddr = DefaultListenAddr
	}
	if c.UpstreamDNS == "" {
		c.UpstreamDNS = DefaultUpstreamDNS
	}
	if c.RouteTable == 0 {
		c.RouteTable = DefaultRouteTable
	}
	if c.RouteProto == 0 {
		c.RouteProto = DefaultRouteProto
	}
	if c.MinTTL == 0 {
		c.MinTTL = DefaultMinTTL
	}
	if c.MaxTTL == 0 {
		c.MaxTTL = max(DefaultMaxTTL, c.MinTTL)
	}
	if c.UpstreamTimeout == 0 {
		c.UpstreamTimeout = DefaultUpstreamTimeout
	}
	if c.SyncInterval == 0 {
		c.SyncInterval = DefaultSyncInterval
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// Validate reports the first invalid field of c.
func (c Config) Validate() error {
	host, port, err := net.SplitHostPort(c.ListenAddr)
	if err != nil {
		return fmt.Errorf("cribrum: invalid listen address %q: %w", c.ListenAddr, err)
	}
	if _, err := netip.ParseAddr(host); err != nil {
		return fmt.Errorf("cribrum: listen address %q must be an IP literal", c.ListenAddr)
	}
	if port == "" {
		return fmt.Errorf("cribrum: listen address %q has no port", c.ListenAddr)
	}
	if host, port, err := net.SplitHostPort(c.UpstreamDNS); err != nil || host == "" || port == "" {
		return fmt.Errorf("cribrum: invalid upstream address %q (want host:port)", c.UpstreamDNS)
	}
	switch {
	case c.RouteTable <= 0 || int64(c.RouteTable) > math.MaxUint32:
		return fmt.Errorf("cribrum: route table %d out of range", c.RouteTable)
	case c.RouteTable >= 253 && c.RouteTable <= 255:
		return fmt.Errorf("cribrum: route table %d is reserved (253 default, 254 main, 255 local)", c.RouteTable)
	case c.RouteProto <= 4 || c.RouteProto > 255:
		return fmt.Errorf("cribrum: route protocol %d out of range (5-255)", c.RouteProto)
	case c.MinTTL < time.Second:
		return fmt.Errorf("cribrum: min TTL %v must be at least 1s", c.MinTTL)
	case c.MaxTTL < c.MinTTL:
		return fmt.Errorf("cribrum: max TTL %v is below min TTL %v", c.MaxTTL, c.MinTTL)
	case c.UpstreamTimeout <= 0:
		return fmt.Errorf("cribrum: upstream timeout must be positive")
	case c.SyncInterval <= 0:
		return fmt.Errorf("cribrum: sync interval must be positive")
	}
	return nil
}

// Sieve is the DNS proxy that turns A/AAAA answers into kernel host routes.
type Sieve struct {
	cfg       Config
	log       *slog.Logger
	routes    *registry
	udpClient *dns.Client
	tcpClient *dns.Client

	// onListen, if set, receives the bound address once Run is serving.
	onListen func(addr string)
}

// New validates cfg and returns a Sieve backed by the kernel routing table.
func New(cfg Config) (*Sieve, error) {
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	rt, err := newRouter(cfg)
	if err != nil {
		return nil, err
	}
	return newSieve(cfg, rt), nil
}

// newSieve wires a Sieve around an arbitrary router; cfg must be validated.
func newSieve(cfg Config, rt router) *Sieve {
	return &Sieve{
		cfg:       cfg,
		log:       cfg.Logger,
		routes:    newRegistry(rt, cfg.Logger),
		udpClient: &dns.Client{Net: "udp", Timeout: cfg.UpstreamTimeout},
		tcpClient: &dns.Client{Net: "tcp", Timeout: cfg.UpstreamTimeout},
	}
}

func (s *Sieve) target() string {
	if s.cfg.OutIface == "" {
		return "blackhole"
	}
	return s.cfg.OutIface
}

// Run serves DNS on UDP and TCP until ctx is canceled or a server fails.
// Routes left over from a previous run are adopted on start, and every
// route owned by this instance is removed from the kernel before Run returns.
func (s *Sieve) Run(ctx context.Context) error {
	// Bind first: if another instance already owns the port, fail before
	// touching (and on exit flushing) the routes that instance manages.
	servers, err := s.listen()
	if err != nil {
		return err
	}

	adopted, err := s.routes.Adopt(s.cfg.MinTTL)
	if err != nil {
		for _, srv := range servers {
			closeServerConn(srv)
		}
		return fmt.Errorf("cribrum: adopt existing routes: %w", err)
	}
	defer s.routes.Close()

	var wg sync.WaitGroup
	errCh := make(chan error, len(servers))
	startedCh := make(chan struct{}, len(servers))
	for _, srv := range servers {
		srv.NotifyStartedFunc = func() { startedCh <- struct{}{} }
		wg.Go(func() {
			if err := srv.ActivateAndServe(); err != nil {
				errCh <- fmt.Errorf("cribrum: %s server: %w", serverNet(srv), err)
			}
		})
	}

	// Every server either reports started or fails; only then is Shutdown safe.
	var runErr error
	for range servers {
		select {
		case <-startedCh:
		case err := <-errCh:
			runErr = errors.Join(runErr, err)
		}
	}

	syncCtx, stopSync := context.WithCancel(ctx)
	if runErr == nil {
		wg.Go(func() { s.syncLoop(syncCtx) })

		s.log.Info("cribrum active",
			"build", BuildInfo(), "listen", s.cfg.ListenAddr, "upstream", s.cfg.UpstreamDNS,
			"table", s.cfg.RouteTable, "proto", s.cfg.RouteProto, "target", s.target(),
			"adopted", adopted)
		if s.onListen != nil {
			s.onListen(servers[0].PacketConn.LocalAddr().String())
		}
		if ip, err := netip.ParseAddr(hostOf(s.cfg.ListenAddr)); err == nil && !ip.IsLoopback() {
			s.log.Warn("listening on a non-loopback address: anyone who can reach it can inject routes",
				"listen", s.cfg.ListenAddr)
		}

		select {
		case <-ctx.Done():
			s.log.Info("shutting down")
		case err := <-errCh:
			runErr = err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	for _, srv := range servers {
		// A server that failed to start reports "not started"; closing its
		// socket explicitly releases it either way.
		_ = srv.ShutdownContext(shutdownCtx)
		closeServerConn(srv)
	}
	stopSync()
	wg.Wait()
	// Deferred s.routes.Close() flushes the routes after all handlers are gone.
	return runErr
}

// listen binds UDP first and then TCP on the same resolved port, so bind
// errors surface synchronously and ":0" works in tests.
func (s *Sieve) listen() ([]*dns.Server, error) {
	pc, err := net.ListenPacket("udp", s.cfg.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("cribrum: listen udp: %w", err)
	}
	ln, err := net.Listen("tcp", pc.LocalAddr().String())
	if err != nil {
		pc.Close()
		return nil, fmt.Errorf("cribrum: listen tcp: %w", err)
	}
	return []*dns.Server{
		{PacketConn: pc, Handler: s},
		{Listener: ln, Handler: s},
	}, nil
}

func (s *Sieve) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.SyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.routes.Expire()
			if err := s.routes.Reconcile(); err != nil {
				s.log.Warn("reconcile failed", "err", err)
			}
		}
	}
}

// Flush removes every route owned by this instance and stops accepting
// new ones. Run calls it automatically on exit.
func (s *Sieve) Flush() {
	s.routes.Close()
}

func closeServerConn(srv *dns.Server) {
	if srv.PacketConn != nil {
		_ = srv.PacketConn.Close()
	}
	if srv.Listener != nil {
		_ = srv.Listener.Close()
	}
}

func serverNet(srv *dns.Server) string {
	if srv.Listener != nil {
		return "tcp"
	}
	return "udp"
}

func hostOf(addr string) string {
	host, _, _ := net.SplitHostPort(addr)
	return host
}
