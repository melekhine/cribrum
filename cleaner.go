package cribrum

import (
	"context"
	"time"
)

// startCleaner periodically purges expired routes from the cache and kernel table.
func (s *Sieve) startCleaner(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.purgeExpired()
		}
	}
}

func (s *Sieve) drainExpired(now time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var expired []string
	for ipStr, expireAt := range s.routeCache {
		if now.After(expireAt) {
			expired = append(expired, ipStr)
			delete(s.routeCache, ipStr)
		}
	}
	return expired
}

func (s *Sieve) purgeExpired() {
	for _, ipStr := range s.drainExpired(time.Now()) {
		s.removeRoute(ipStr)
	}
}
