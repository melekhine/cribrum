SOZVEZDIE SSK OOO                                      Igor A. Melekhine
                                                               Nov. 2026


         CRIBRUM: DYNAMIC DNS-TO-KERNEL ROUTE INJECTOR FOR BGP


   A minimalist utility for sifting IPv4 (/32) and IPv6 (/128) host
   prefixes from DNS replies directly into a Linux kernel routing
   table via Netlink.


1. Overview & How It Works

   Modern web services hide behind shared Content Delivery Networks
   (CDNs). Routing entire Autonomous Systems (ASNs) causes collateral
   traffic leakage. CRIBRUM solves this via a four-step pipeline:

   (a) Domain Filtering: dnsmasq intercepts DNS queries for selected
       domains and forwards them to CRIBRUM on a loopback port.
   (b) Transparent Resolution: CRIBRUM proxies the intact DNS packet
       (preserving EDNS0 and DNSSEC signatures) to an upstream resolver.
   (c) Netlink Injection: Every A and AAAA record in the reply is
       injected as a /32 or /128 host route into a dedicated Linux
       routing table, ready to be exported by BIRD2 over BGP.
   (d) Lifecycle Management: A background garbage collector tracks DNS
       TTLs and purges stale routes. On SIGINT/SIGTERM, CRIBRUM flushes
       all injected routes cleanly before exit.

2. Architecture

   +------------+    DNS (53)     +-----------+   Forwarded   +---------+
   | LAN Client | --------------> |  dnsmasq  | ------------> | cribrum |
   +------------+                 +-----------+   (127:5354)  +---------+
                                                                   |
                                                       Netlink     |
                                                       (Table 100) v
   +------------+   BGP / OSPFv2/v3   +-------+   Scan RIB    +---------+
   |  Neighbor  | <================== | BIRD  | <------------ | Kernel  |
   +------------+                     +-------+               +---------+

3. Usage

   $ cribrum [-listen addr] [-upstream addr] [-table id]
             [-iface dev] [-ttl sec] [-completion shell] [-version]

   Flags:
     -listen      Local UDP bind address (default "127.0.0.1:5354")
     -upstream    Upstream local DNS resolver (e.g. stubby/unbound) (default "127.0.0.53:53")
     -table       Kernel routing table ID (default 100)
     -iface       Egress tunnel interface (default "dummy0")
     -ttl         Minimum route lifetime in seconds (default 300)
     -completion  Generate shell completion script (bash|zsh)
     -version     Print version information
     -h, -help    Display this specification

4. Integration

   Add target domains to dnsmasq.conf:
      server=example.com
      server=*.example.net

   Configure BIRD kernel protocol:
      protocol kernel {
          learn;
          kernel table 100;
          scan time 30;
          ipv4 { import all; export none; };
          ipv6 { import all; export none; };
      }

5. Security Considerations

   Bind -listen strictly to loopback (127.0.0.1) to prevent unauthorized
   route table exhaustion via spoofed DNS packets.

Author's Address

   `ask+code@takefrom.ru`
