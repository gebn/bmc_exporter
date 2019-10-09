package bmc

import (
	"net/http"
	"sync"
	"time"

	"github.com/gebn/bmc_exporter/target"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// gcInterval is the period at which GC of targets is done. This clears up
	// targets no longer being scraped, so the mapper's underlying map does not
	// grow indefinitely.
	gcInterval = time.Minute * 30

	// inactivityThreshold is the duration after which a target will be eligible
	// for GC next GC interval. Targets older than this may exist - they are
	// only cleared on GC.
	inactivityThreshold = time.Minute * 30

	namespace = "bmc"
	subsystem = "mapper"

	queries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "queries_total",
		Help: "The number of times a handler has been requested from the " +
			"mapper.",
	})
	hits = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "hits_total",
		Help:      "The number of times a previously created handler was returned.",
	})
	gcTargetsCleared = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "gc_targets_cleared",
		Help:      "Observes the number of targets removed by GC each cycle.",
		// can also get number of GCs from _count, and total number of removed
		// targets from _sum; can obtain size of map with queries - hits -
		// targets cleared sum
		Buckets: prometheus.ExponentialBuckets(1, 2, 10), // 512
	})
	gcDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "gc_duration_seconds",
		Help:      "The amount of time taken by GC.",
		Buckets:   prometheus.ExponentialBuckets(0.0005, 1.2, 10), // 0.0026
	})
)

// Mapper manages the http.Handler we create for each BMC being scraped. Given a
// target addr, it returns a promhttp-created handler that, when invoked, will
// retrieve and yield metrics for that BMC.
type Mapper struct {
	provider target.Provider
	targets  map[string]*target.Target
	mu       sync.RWMutex
	done     chan struct{}  // closed when mapper should shut down
	wg       sync.WaitGroup // becomes done when ticker has closed
}

// NewMapper creates a Mapper struct ready for mapping targets to handlers.
func NewMapper(p target.Provider) *Mapper {
	m := &Mapper{
		provider: p,
		targets:  map[string]*target.Target{},
		done:     make(chan struct{}),
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(gcInterval)
		for {
			select {
			case <-ticker.C:
				m.gc()
			case <-m.done:
				// note, irritatingly, this does *not* close the C channel,
				// which is the only reason why the done channel is needed
				ticker.Stop()
				return
			}
		}
	}()
	return m
}

// Handler returns a http.Handler that returns metrics collected from the BMC at
// the provided address. If the address has been requested before, this will
// return the original handler, otherwise it will create a new one. It is
// effectively a synchronised, lazy map access.
func (m *Mapper) Handler(addr string) http.Handler {
	queries.Inc()
	// first, try with a read lock, optimistically assuming it's there; this
	// will be the case most of the time
	m.mu.RLock()
	t, ok := m.targets[addr]
	m.mu.RUnlock()
	if ok {
		hits.Inc()
		return t
	}

	// unlucky, first scrape, need to take out a write lock and create the
	// structures
	m.mu.Lock()
	defer m.mu.Unlock()

	// in the time taken to acquire the lock, another goroutine may have done
	// this, so check once more
	if t, ok = m.targets[addr]; ok {
		return t
	}

	// nope, still don't have it, create. This could panic, but if it does, it
	// will fail for every request, so we'll realise pretty quickly.
	bmc := m.provider.TargetFor(addr)
	m.targets[addr] = bmc
	return bmc
}

// gc deletes inactive handlers. This process is necessary due to our caching of
// handlers and collectors - we always assume there will be another scrape, and
// allocate resources to that effect, so we need something to come along and
// call it a day when a given BMC hasn't been scraped in a while to avoid an
// unbounded increase in memory use over time.
func (m *Mapper) gc() {
	timer := prometheus.NewTimer(gcDuration)
	defer timer.ObserveDuration()

	threshold := time.Now().Add(-inactivityThreshold).UnixNano()
	expired := m.closeTargets(func(t *target.Target) bool {
		return t.LastCollection() < threshold
	})
	gcTargetsCleared.Observe(float64(expired))
}

func (m *Mapper) Close() {
	// if a GC is in progress, we may have GC closing some targets while we
	// close the rest - this is fine as they will never try to close the same
	// one due to the mutex
	m.done <- struct{}{}
	m.closeTargets(func(_ *target.Target) bool {
		return true
	})

	// wait for GC goroutine to finish; it very likely has as there will
	// normally be more active targets than expired ones
	m.wg.Wait()
}

// closeTargets cleanly shuts down targets matching a predicate. This is used
// both during GC and when shutting down the mapper completely; the former
// simply involves a subset rather than all targets. This method is safe to call
// concurrently, even with predicates that select an intersecting set of
// targets.
func (m *Mapper) closeTargets(shouldClose func(t *target.Target) bool) int {
	// put all eligible targets in a slice rather than clear them up immediately
	// to ensure we don't switch to another goroutine - in the case of GC, we
	// want to run as quickly as possible
	toClose := []*target.Target{}

	m.mu.Lock()
	for addr, t := range m.targets {
		if shouldClose(t) {
			toClose = append(toClose, t)
			delete(m.targets, addr)
		}
	}
	m.mu.Unlock()

	// because we removed targets from the map, no new scrapes will come in for
	// them, but there may still be some in progress; there is now a very small
	// period where we could potentially create a new target and start scraping
	// afresh before these finish, however the chances are miniscule

	wg := sync.WaitGroup{}
	wg.Add(len(toClose))
	for _, t := range toClose {
		go func(t *target.Target) {
			defer wg.Done()
			// this uses the event loop, so will wait for any in-progress scrape
			// to finish
			t.Close()
		}(t)
	}
	wg.Wait()

	return len(toClose)
}
