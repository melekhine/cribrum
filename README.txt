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
       (preserving EDNS0 and DNSSEC signatures) to an upstream resolver
       over UDP, retrying over TCP when the answer is truncated.
   (c) Netlink Injection: Every A and AAAA record that belongs to the
       queried name (directly or via its CNAME chain) is installed as
       a /32 or /128 host route BEFORE the reply is sent, so the client
       never connects ahead of its route. Routes are tagged with a
       dedicated route protocol (default 201) and are ready to be
       exported by BIRD over BGP.
   (d) Lifecycle Management: A background loop expires routes after
       their DNS TTL (clamped to [-ttl, -max-ttl]) and reconciles the
       kernel table, restoring routes lost when the egress interface
       is recreated. On SIGINT/SIGTERM all routes are flushed. After a
       crash, the next start adopts leftover routes for -ttl seconds.

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

   $ cribrum [-listen addr] [-upstream addr] [-table id] [-proto id]
             [-iface dev] [-ttl sec] [-max-ttl sec] [-allow-private]
             [-debug] [-completion shell] [-version]

   Flags:
     -listen         Local bind address, UDP and TCP
                     (default "127.0.0.1:5354")
     -upstream       Upstream DNS resolver (default "127.0.1.1:53")
     -table          Kernel routing table ID (default 100; 253-255
                     are reserved and rejected)
     -proto          Route protocol ID owned by cribrum (default 201)
     -iface          Egress tunnel interface (default: none, which
                     installs blackhole routes)
     -ttl            Minimum route lifetime in seconds (default 300)
     -max-ttl        Maximum route lifetime in seconds (default 86400)
     -allow-private  Allow RFC 1918, CGNAT (100.64/10) and ULA targets
     -debug          Log every route change
     -completion     Generate shell completion script (bash|zsh)
     -version        Print version information
     -h, -help       Display this specification

   Addresses that must never be routed (0.0.0.0, ::, loopback,
   link-local, multicast, reserved) are always ignored, so answers
   from blocking resolvers do not reach the routing table.

4. Integration

   Register the route protocol name (optional, for "ip route" output):
      # echo "201 cribrum" >> /etc/iproute2/rt_protos
      $ ip route show table 100 proto cribrum

   Forward target domains from dnsmasq.conf (subdomains included):
      server=/example.com/127.0.0.1#5354
      server=/example.net/127.0.0.1#5354

   Configure the BIRD kernel protocol:
      protocol kernel {
          learn;
          kernel table 100;
          scan time 30;
          ipv4 { import where krt_source = 201; export none; };
          ipv6 { import where krt_source = 201; export none; };
      }

   A systemd unit running without root (CAP_NET_ADMIN only) is
   shipped as contrib/cribrum.service.

5. Operational Considerations

   Long-lived connections (WebSocket, streaming, SSH) may outlive the
   DNS TTL. Once a route expires their packets follow the default
   route and the connection breaks. Choose -ttl comfortably above the
   lifetime of such sessions; client-side DNS caches that ignore TTL
   have the same effect.

   Do not run two instances with the same -table and -proto: each one
   removes routes it does not know about.

6. Security Considerations

   Bind -listen strictly to loopback (127.0.0.1) to prevent unauthorized
   route table exhaustion via spoofed DNS packets. CRIBRUM logs a
   warning when bound to any other address.

Author's Address

   `ask+code@takefrom.ru`
