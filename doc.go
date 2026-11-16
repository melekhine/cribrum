// Package cribrum provides a dynamic DNS-to-kernel route injector for BGP daemons.
//
// Cribrum acts as a narrow-mesh sieve behind dnsmasq, extracting IPv4 (/32) and
// IPv6 (/128) host addresses from DNS replies and injecting them directly into
// a specified Linux kernel routing table via Netlink.
package cribrum
