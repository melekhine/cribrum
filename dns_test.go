package cribrum

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func rr(t *testing.T, s string) dns.RR {
	t.Helper()
	r, err := dns.NewRR(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestExtractHostsFollowsCNAMEChain(t *testing.T) {
	answer := []dns.RR{
		// Deliberately out of order.
		rr(t, "edge.cdn.example. 60 IN A 104.18.12.34"),
		rr(t, "WWW.Example.com. 300 IN CNAME alias.example.net."),
		rr(t, "alias.example.net. 300 IN CNAME edge.cdn.example."),
		rr(t, "edge.cdn.example. 60 IN AAAA ::ffff:104.18.12.35"),
		rr(t, "evil.example. 60 IN A 203.0.113.66"), // not in the chain
	}
	got := extractHosts("www.example.com.", answer)

	want := []hostRecord{
		{netip.MustParseAddr("104.18.12.34"), 60},
		{netip.MustParseAddr("104.18.12.35"), 60}, // unmapped
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("extractHosts = %v, want %v", got, want)
	}
}

func TestRoutable(t *testing.T) {
	tests := []struct {
		addr         string
		allowPrivate bool
		want         bool
	}{
		{"104.18.12.34", false, true},
		{"2606:4700::1111", false, true},
		{"0.0.0.0", true, false}, // blocking resolver answer
		{"::", true, false},
		{"0.1.2.3", true, false},
		{"127.0.0.1", true, false},
		{"::1", true, false},
		{"169.254.1.1", true, false},
		{"fe80::1", true, false},
		{"224.0.0.251", true, false},
		{"ff02::fb", true, false},
		{"255.255.255.255", true, false},
		{"100::1", true, false},
		{"10.1.2.3", false, false},
		{"10.1.2.3", true, true},
		{"100.100.100.100", false, false}, // CGNAT / Tailscale
		{"100.100.100.100", true, true},
		{"fd7a:115c:a1e0::1", false, false}, // ULA
		{"fd7a:115c:a1e0::1", true, true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/private=%v", tt.addr, tt.allowPrivate), func(t *testing.T) {
			if got := routable(netip.MustParseAddr(tt.addr), tt.allowPrivate); got != tt.want {
				t.Errorf("routable(%s) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

// startUpstream runs a fake resolver over UDP and TCP on one loopback port.
// Over UDP it truncates big answers, mimicking a real server.
func startUpstream(t *testing.T, h dns.HandlerFunc) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", pc.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	for _, srv := range []*dns.Server{{PacketConn: pc, Handler: h}, {Listener: ln, Handler: h}} {
		started := make(chan struct{})
		srv.NotifyStartedFunc = func() { close(started) }
		go func() { _ = srv.ActivateAndServe() }()
		<-started
		t.Cleanup(func() { _ = srv.Shutdown() })
	}
	return pc.LocalAddr().String()
}

// bigAnswer returns 40 A records: too large for a 512-byte UDP reply.
func bigAnswer(w dns.ResponseWriter, req *dns.Msg) {
	resp := new(dns.Msg).SetReply(req)
	for i := range 40 {
		resp.Answer = append(resp.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: req.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
			A:   net.IPv4(203, 0, 113, byte(i+1)).To4(),
		})
	}
	if _, udp := w.RemoteAddr().(*net.UDPAddr); udp {
		resp.Truncate(dns.MinMsgSize)
	}
	_ = w.WriteMsg(resp)
}

func testSieve(t *testing.T, upstream string, rt router) *Sieve {
	t.Helper()
	cfg := Config{
		ListenAddr:   "127.0.0.1:0",
		UpstreamDNS:  upstream,
		AllowPrivate: true,
		Logger:       discardLog,
	}.withDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return newSieve(cfg, rt)
}

func TestServeDNSTruncatedUpstreamFallsBackToTCP(t *testing.T) {
	upstream := startUpstream(t, bigAnswer)
	rt := newFakeRouter()
	s := testSieve(t, upstream, rt)

	// Serve the sieve itself on loopback and query it as dnsmasq would.
	addr := startUpstream(t, s.ServeDNS)
	q := new(dns.Msg).SetQuestion("many.example.", dns.TypeA)

	resp, _, err := (&dns.Client{Net: "udp"}).Exchange(q, addr)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Truncated {
		t.Error("UDP reply to a 512-byte client must carry TC")
	}
	if rt.size() != 40 {
		t.Fatalf("routes = %d, want all 40 records (upstream TCP fallback)", rt.size())
	}

	resp, _, err = (&dns.Client{Net: "tcp"}).Exchange(q, addr)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Truncated || len(resp.Answer) != 40 {
		t.Fatalf("TCP reply: truncated=%v answers=%d, want full answer", resp.Truncated, len(resp.Answer))
	}
}

func TestServeDNSUpstreamDown(t *testing.T) {
	// Grab a free port and close it so nothing answers there.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dead := pc.LocalAddr().String()
	pc.Close()

	rt := newFakeRouter()
	s := testSieve(t, dead, rt)
	s.udpClient.Timeout = 200 * time.Millisecond
	s.tcpClient.Timeout = 200 * time.Millisecond
	addr := startUpstream(t, s.ServeDNS)

	resp, _, err := (&dns.Client{Net: "udp"}).Exchange(new(dns.Msg).SetQuestion("x.example.", dns.TypeA), addr)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Rcode != dns.RcodeServerFailure {
		t.Fatalf("rcode = %s, want SERVFAIL", dns.RcodeToString[resp.Rcode])
	}
}

func TestRunServesAndFlushesOnShutdown(t *testing.T) {
	upstream := startUpstream(t, func(w dns.ResponseWriter, req *dns.Msg) {
		resp := new(dns.Msg).SetReply(req)
		resp.Answer = append(resp.Answer, rr(t, req.Question[0].Name+" 60 IN A 104.18.12.34"))
		_ = w.WriteMsg(resp)
	})

	stale := netip.MustParseAddr("198.51.100.9")
	rt := newFakeRouter(stale) // left over from a "previous run"
	s := testSieve(t, upstream, rt)

	listening := make(chan string, 1)
	s.onListen = func(addr string) { listening <- addr }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	var addr string
	select {
	case addr = <-listening:
	case err := <-done:
		t.Fatalf("Run exited early: %v", err)
	}

	q := new(dns.Msg).SetQuestion("www.example.com.", dns.TypeA)
	resp, _, err := (&dns.Client{Net: "tcp"}).Exchange(q, addr)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Answer) != 1 || !strings.Contains(resp.Answer[0].String(), "104.18.12.34") {
		t.Fatalf("unexpected answer: %v", resp.Answer)
	}
	if !rt.has(addrA) || !rt.has(stale) {
		t.Fatalf("kernel = %v, want new route plus adopted stale route", rt.routes)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rt.size() != 0 {
		t.Fatalf("routes left after shutdown: %v", rt.routes)
	}
}

func TestRunListenError(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()

	// Routes of the instance that already holds the port must survive.
	rt := newFakeRouter(addrA)
	s := testSieve(t, "127.0.0.1:53", rt)
	s.cfg.ListenAddr = pc.LocalAddr().String() // already in use

	if err := s.Run(context.Background()); err == nil {
		t.Fatal("expected bind error")
	}
	if !rt.has(addrA) {
		t.Fatal("a failed start flushed routes owned by another instance")
	}
}
