package cribrum

import (
	"errors"
	"log/slog"
	"net/netip"
	"sync"
	"time"
)

// router manipulates host routes owned by cribrum in the kernel.
// Implementations need not be safe for concurrent use: registry
// serialises every call under its own mutex.
type router interface {
	// Replace installs (or overwrites) the host route for addr.
	Replace(addr netip.Addr) error
	// Delete removes the host route for addr; a missing route is not an error.
	Delete(addr netip.Addr) error
	// List returns every host route currently owned by cribrum.
	List() ([]netip.Addr, error)
}

var errClosed = errors.New("cribrum: route registry closed")

// registry is the single source of truth for which routes should exist and
// until when. Kernel operations run under the same lock as the state change,
// so the kernel can never disagree with the map because of interleaving.
// A netlink round-trip is tens of microseconds, which makes that affordable.
type registry struct {
	mu     sync.Mutex
	rt     router
	log    *slog.Logger
	now    func() time.Time
	expiry map[netip.Addr]time.Time
	closed bool
}

func newRegistry(rt router, log *slog.Logger) *registry {
	return &registry{
		rt:     rt,
		log:    log,
		now:    time.Now,
		expiry: make(map[netip.Addr]time.Time),
	}
}

// Ensure synchronously guarantees a route to addr for at least ttl.
// A failed kernel update leaves addr untracked, so the next DNS answer retries.
func (r *registry) Ensure(addr netip.Addr, ttl time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errClosed
	}

	exp := r.now().Add(ttl)
	if cur, ok := r.expiry[addr]; ok {
		if exp.After(cur) {
			r.expiry[addr] = exp
		}
		return nil
	}
	if err := r.rt.Replace(addr); err != nil {
		return err
	}
	r.expiry[addr] = exp
	r.log.Debug("route added", "addr", addr, "ttl", ttl)
	return nil
}

// Expire deletes routes whose lifetime has passed. A route whose deletion
// fails stays tracked and is retried on the next call.
func (r *registry) Expire() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	for addr, exp := range r.expiry {
		if now.Before(exp) {
			continue
		}
		if err := r.rt.Delete(addr); err != nil {
			r.log.Warn("route delete failed", "addr", addr, "err", err)
			continue
		}
		delete(r.expiry, addr)
		r.log.Debug("route expired", "addr", addr)
	}
}

// Reconcile makes the kernel match the registry: routes the kernel lost
// (e.g. the egress interface was recreated) are restored, and routes owned
// by cribrum that the registry does not know about are removed.
func (r *registry) Reconcile() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}

	present, err := r.rt.List()
	if err != nil {
		return err
	}

	var errs []error
	inKernel := make(map[netip.Addr]struct{}, len(present))
	for _, addr := range present {
		inKernel[addr] = struct{}{}
		if _, ok := r.expiry[addr]; ok {
			continue
		}
		if err := r.rt.Delete(addr); err != nil {
			errs = append(errs, err)
			continue
		}
		r.log.Info("orphan route removed", "addr", addr)
	}
	for addr := range r.expiry {
		if _, ok := inKernel[addr]; ok {
			continue
		}
		if err := r.rt.Replace(addr); err != nil {
			errs = append(errs, err)
			continue
		}
		r.log.Info("route restored", "addr", addr)
	}
	return errors.Join(errs...)
}

// Adopt takes ownership of routes left by a previous run (crash, SIGKILL)
// and keeps them for ttl, so a restart does not withdraw routes that
// clients still rely on. Each one is re-installed to pick up the current
// egress target; those that cannot be are deleted.
func (r *registry) Adopt(ttl time.Duration) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	present, err := r.rt.List()
	if err != nil {
		return 0, err
	}
	exp := r.now().Add(ttl)
	for _, addr := range present {
		if err := r.rt.Replace(addr); err != nil {
			r.log.Warn("stale route dropped", "addr", addr, "err", err)
			_ = r.rt.Delete(addr)
			continue
		}
		r.expiry[addr] = exp
	}
	return len(r.expiry), nil
}

// Close removes every tracked route and rejects further Ensure calls,
// so handlers still in flight cannot leak routes past shutdown.
func (r *registry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true

	if len(r.expiry) > 0 {
		r.log.Info("flushing routes", "count", len(r.expiry))
	}
	for addr := range r.expiry {
		if err := r.rt.Delete(addr); err != nil {
			r.log.Warn("route delete failed", "addr", addr, "err", err)
		}
	}
	clear(r.expiry)
}

// Len reports the number of tracked routes.
func (r *registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.expiry)
}
