package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/melekhine/cribrum"
)

func main() {
	var (
		cfg         cribrum.Config
		minTTL      uint
		compShell   string
		showVersion bool
	)

	flag.StringVar(&cfg.ListenAddr, "listen", "127.0.0.1:5353", "Local UDP bind address")
	flag.StringVar(&cfg.UpstreamDNS, "upstream", "1.1.1.1:53", "Upstream DNS resolver")
	flag.IntVar(&cfg.RouteTable, "table", 100, "Kernel routing table ID")
	flag.StringVar(&cfg.OutIface, "iface", "tailscale0", "Egress tunnel interface")
	flag.UintVar(&minTTL, "ttl", 300, "Minimum route lifetime in seconds")
	flag.StringVar(&compShell, "completion", "", "Generate shell completion script (bash|zsh)")
	flag.BoolVar(&showVersion, "version", false, "Print version and build information")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "CRIBRUM — Dynamic DNS-to-Kernel Route Injector\n\nUsage:\n  cribrum [flags]\n\nFlags:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if showVersion {
		fmt.Println(cribrum.BuildInfo())
		return
	}

	if compShell != "" {
		if err := cribrum.WriteCompletion(os.Stdout, compShell); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	cfg.MinTTL = uint32(minTTL)

	sieve, err := cribrum.New(cfg)
	if err != nil {
		log.Fatalf("Initialization failed: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := sieve.Run(ctx); err != nil {
		log.Fatalf("Fatal: %v", err)
	}
}
