package cribrum

import (
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"
)

// fakeRouter is an in-memory kernel table with injectable failures.
type fakeRouter struct {
	mu         sync.Mutex
	routes     map[netip.Addr]bool
	failAdd    map[netip.Addr]error
	failDel    map[netip.Addr]error
	replaceCnt int
}

func newFakeRouter(addrs ...netip.Addr) *fakeRouter {
	f := &fakeRouter{
		routes:  make(map[netip.Addr]bool),
		failAdd: make(map[netip.Addr]error),
		failDel: make(map[netip.Addr]error),
	}
	for _, a := range addrs {
		f.routes[a] = true
	}
	return f
}

func (f *fakeRouter) Replace(a netip.Addr) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replaceCnt++
	if err := f.failAdd[a]; err != nil {
		return err
	}
	f.routes[a] = true
	return nil
}

func (f *fakeRouter) Delete(a netip.Addr) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failDel[a]; err != nil {
		return err
	}
	delete(f.routes, a)
	return nil
}

func (f *fakeRouter) List() ([]netip.Addr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Collect(maps.Keys(f.routes)), nil
}

func (f *fakeRouter) has(a netip.Addr) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.routes[a]
}

func (f *fakeRouter) size() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.routes)
}

var discardLog = slog.New(slog.NewTextHandler(io.Discard, nil))

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestRegistry(rt router) (*registry, *fakeClock) {
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	r := newRegistry(rt, discardLog)
	r.now = clk.now
	return r, clk
}

var (
	addrA = netip.MustParseAddr("104.18.12.34")
	addrB = netip.MustParseAddr("2606:4700::6812:c22")
)

func TestEnsureAddsOnceAndOnlyExtends(t *testing.T) {
	rt := newFakeRouter()
	r, clk := newTestRegistry(rt)

	if err := r.Ensure(addrA, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := r.Ensure(addrA, time.Minute); err != nil { // shorter: must not shrink
		t.Fatal(err)
	}
	if rt.replaceCnt != 1 {
		t.Fatalf("Replace called %d times, want 1", rt.replaceCnt)
	}

	clk.advance(5 * time.Minute)
	r.Expire()
	if !rt.has(addrA) {
		t.Fatal("route expired early: a shorter TTL shrank the lifetime")
	}

	if err := r.Ensure(addrA, 20*time.Minute); err != nil { // longer: extends
		t.Fatal(err)
	}
	clk.advance(10 * time.Minute)
	r.Expire()
	if !rt.has(addrA) {
		t.Fatal("route expired although its lifetime was extended")
	}

	clk.advance(10 * time.Minute)
	r.Expire()
	if rt.has(addrA) || r.Len() != 0 {
		t.Fatal("route not removed after expiry")
	}
}

func TestEnsureFailureIsRetried(t *testing.T) {
	rt := newFakeRouter()
	rt.failAdd[addrA] = errors.New("ENODEV")
	r, _ := newTestRegistry(rt)

	if err := r.Ensure(addrA, time.Minute); err == nil {
		t.Fatal("expected error")
	}
	if r.Len() != 0 {
		t.Fatal("failed route must not be tracked")
	}

	delete(rt.failAdd, addrA)
	if err := r.Ensure(addrA, time.Minute); err != nil {
		t.Fatal(err)
	}
	if !rt.has(addrA) {
		t.Fatal("route not installed on retry")
	}
}

func TestExpireKeepsRouteWhenDeleteFails(t *testing.T) {
	rt := newFakeRouter()
	r, clk := newTestRegistry(rt)
	_ = r.Ensure(addrA, time.Minute)

	rt.failDel[addrA] = errors.New("EBUSY")
	clk.advance(2 * time.Minute)
	r.Expire()
	if r.Len() != 1 {
		t.Fatal("route forgotten although kernel delete failed")
	}

	delete(rt.failDel, addrA)
	r.Expire()
	if r.Len() != 0 || rt.has(addrA) {
		t.Fatal("delete not retried")
	}
}

func TestReconcileRestoresAndRemovesOrphans(t *testing.T) {
	orphan := netip.MustParseAddr("203.0.113.7")
	rt := newFakeRouter()
	r, _ := newTestRegistry(rt)
	_ = r.Ensure(addrA, time.Minute)
	_ = r.Ensure(addrB, time.Minute)

	// Interface flap: the kernel dropped addrA; someone left an orphan behind.
	_ = rt.Delete(addrA)
	_ = rt.Replace(orphan)

	if err := r.Reconcile(); err != nil {
		t.Fatal(err)
	}
	if !rt.has(addrA) || !rt.has(addrB) {
		t.Error("tracked routes not restored")
	}
	if rt.has(orphan) {
		t.Error("orphan route not removed")
	}
}

func TestAdoptTakesOverStaleRoutes(t *testing.T) {
	broken := netip.MustParseAddr("198.51.100.1")
	rt := newFakeRouter(addrA, broken)
	rt.failAdd[broken] = errors.New("ENODEV")
	r, clk := newTestRegistry(rt)

	n, err := r.Adopt(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || !rt.has(addrA) || rt.has(broken) {
		t.Fatalf("adopted %d, kernel=%v; want addrA kept, broken removed", n, rt.routes)
	}

	clk.advance(2 * time.Minute)
	r.Expire()
	if rt.has(addrA) {
		t.Fatal("adopted route never expires")
	}
}

func TestCloseFlushesAndRejectsLateAdds(t *testing.T) {
	rt := newFakeRouter()
	r, _ := newTestRegistry(rt)
	_ = r.Ensure(addrA, time.Minute)
	_ = r.Ensure(addrB, time.Minute)

	r.Close()
	if rt.size() != 0 {
		t.Fatalf("routes left after Close: %v", rt.routes)
	}
	if err := r.Ensure(addrA, time.Minute); !errors.Is(err, errClosed) {
		t.Fatalf("Ensure after Close = %v, want errClosed", err)
	}
	if rt.size() != 0 {
		t.Fatal("route leaked after Close")
	}
	r.Close() // idempotent
}

// TestConcurrentEnsureExpire checks that the kernel never diverges from the
// registry under contention (the original async design lost routes here).
func TestConcurrentEnsureExpire(t *testing.T) {
	rt := newFakeRouter()
	r := newRegistry(rt, discardLog)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 500 {
				_ = r.Ensure(addrA, time.Nanosecond)
				r.Expire()
			}
		})
	}
	wg.Wait()

	if tracked, inKernel := r.Len() == 1, rt.has(addrA); tracked != inKernel {
		t.Fatalf("registry tracked=%v but kernel has=%v", tracked, inKernel)
	}
}
