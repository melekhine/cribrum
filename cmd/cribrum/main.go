package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/melekhine/cribrum"
)

// seconds is a flag.Value that reads a whole number of seconds into a Duration.
type seconds struct{ d *time.Duration }

func (s seconds) String() string {
	if s.d == nil {
		return "0"
	}
	return strconv.FormatInt(int64(*s.d/time.Second), 10)
}

func (s seconds) Set(v string) error {
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return fmt.Errorf("want whole seconds (0-%d)", uint32(1<<32-1))
	}
	*s.d = time.Duration(n) * time.Second
	return nil
}

func main() {
	os.Exit(run())
}

func run() int {
	var (
		cfg         = cribrum.DefaultConfig()
		compShell   string
		showVersion bool
		debug       bool
	)

	flag.StringVar(&cfg.ListenAddr, "listen", cfg.ListenAddr, "Local bind address, served over UDP and TCP")
	flag.StringVar(&cfg.UpstreamDNS, "upstream", cfg.UpstreamDNS, "Upstream DNS resolver (host:port)")
	flag.IntVar(&cfg.RouteTable, "table", cfg.RouteTable, "Kernel routing table ID")
	flag.IntVar(&cfg.RouteProto, "proto", cfg.RouteProto, "Route protocol ID marking routes owned by cribrum (5-255)")
	flag.StringVar(&cfg.OutIface, "iface", cfg.OutIface, "Egress tunnel interface (empty for blackhole routes)")
	flag.Var(seconds{&cfg.MinTTL}, "ttl", "Minimum route lifetime in `seconds`")
	flag.Var(seconds{&cfg.MaxTTL}, "max-ttl", "Maximum route lifetime in `seconds`")
	flag.BoolVar(&cfg.AllowPrivate, "allow-private", false, "Allow routes to RFC 1918, CGNAT and ULA addresses")
	flag.BoolVar(&debug, "debug", false, "Log every route change")
	flag.StringVar(&compShell, "completion", "", "Generate shell completion script (bash|zsh)")
	flag.BoolVar(&showVersion, "version", false, "Print version and build information")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(),
			"CRIBRUM — Dynamic DNS-to-Kernel Route Injector\n\nUsage:\n  cribrum [flags]\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if showVersion {
		fmt.Println(cribrum.BuildInfo())
		return 0
	}
	if compShell != "" {
		if err := cribrum.WriteCompletion(os.Stdout, compShell); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return 0
	}

	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	cfg.Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	sieve, err := cribrum.New(cfg)
	if err != nil {
		cfg.Logger.Error("initialization failed", "err", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := sieve.Run(ctx); err != nil {
		cfg.Logger.Error("fatal", "err", err)
		return 1
	}
	return 0
}
