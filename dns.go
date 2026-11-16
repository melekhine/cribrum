package cribrum

import "github.com/miekg/dns"

// handleDNS proxies incoming DNS queries upstream and extracts A/AAAA records.
func (s *Sieve) handleDNS(w dns.ResponseWriter, r *dns.Msg) {
	resp, _, err := s.dnsClient.Exchange(r, s.cfg.UpstreamDNS)
	if err != nil {
		dns.HandleFailed(w, r)
		return
	}

	for _, ans := range resp.Answer {
		switch record := ans.(type) {
		case *dns.A:
			go s.injectRoute(record.A, record.Hdr.Ttl)
		case *dns.AAAA:
			go s.injectRoute(record.AAAA, record.Hdr.Ttl)
		}
	}

	_ = w.WriteMsg(resp)
}
