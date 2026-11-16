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

func (s *Sieve) purgeExpired() {
	now := time.Now()
	var expired []string

	s.mu.Lock()
	for ipStr, expireAt := range s.routeCache {
		if now.After(expireAt) {
			expired = append(expired, ipStr)
			delete(s.routeCache, ipStr)
		}
	}
	s.mu.Unlock()

	for _, ipStr := range expired {
		s.removeRoute(ipStr)
	}
}
