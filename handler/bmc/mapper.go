package bmc

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gebn/bmc_exporter/collector"
	"github.com/gebn/bmc_exporter/session"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
		Buckets: prometheus.ExponentialBuckets(1, 1.5, 10), // 38.44
	})
	gcDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "gc_duration_seconds",
		Help:      "The amount of time taken by GC. This is stop-the-world.",
		Buckets:   prometheus.ExponentialBuckets(0.0001, 1.5, 10), // 0.0038
	})
)

// target contains the data we need to carry out the mapper's functionality.
// This would normally be just a http.Handler, however we need access to the
// underlying collector in order for GC to query when it was last invoked.
type target struct {
	handler   http.Handler
	collector *collector.Collector
}

// Mapper manages the http.Handler we create for each BMC being scraped. Given a
// target addr, it returns a promhttp-created handler that, when invoked, will
// retrieve and yield metrics for that BMC.
type Mapper struct {
	Provider session.Provider
	Timeout  time.Duration

	targets map[string]target
	mu      sync.RWMutex
	ticker  *time.Ticker
}

// NewMapper creates a Mapper struct ready for mapping targets to handlers.
func NewMapper(provider session.Provider, timeout time.Duration) *Mapper {
	ticker := time.NewTicker(gcInterval)
	m := &Mapper{
		Provider: provider,
		Timeout:  timeout,
		targets:  map[string]target{},
		ticker:   ticker,
	}
	go func() {
		for range ticker.C {
			m.gc()
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
		return t.handler
	}

	// unlucky, first scrape, need to take out a write lock and create the
	// structures
	m.mu.Lock()
	defer m.mu.Unlock()

	// in the time taken to acquire the lock, another goroutine may have done
	// this, so check once more
	if target, ok := m.targets[addr]; ok {
		return target.handler
	}

	// nope, still don't have it, create. This could panic, but if it does, it
	// will fail for every request, so we'll realise pretty quickly.
	col := &collector.Collector{
		Target:   addr,
		Provider: m.Provider,
		Timeout:  m.Timeout,
	}
	reg := prometheus.NewRegistry()
	reg.MustRegister(col)
	handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	m.targets[addr] = target{
		handler:   handler,
		collector: col,
	}
	return handler
}

// gc deletes inactive handlers. This process is necessary due to our caching of
// handlers and collectors - we always assume there will be another scrape, and
// allocate resources to that effect, so we need something to come along and
// call it a day when a given BMC hasn't been scraped in a while to avoid an
// unbounded increase in memory use over time.
func (m *Mapper) gc() {
	timer := prometheus.NewTimer(gcDuration)
	defer timer.ObserveDuration()

	// create an expired context so we don't wait for session close - it will be
	// long dead; we just want to close the UDP socket
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	threshold := time.Now().Add(-inactivityThreshold)
	cleared := 0

	m.mu.Lock()
	for addr, target := range m.targets {
		last := target.collector.LastCollection()
		if !last.IsZero() && last.Before(threshold) {
			target.collector.Close(ctx)
			delete(m.targets, addr)
			cleared++
		}
	}
	m.mu.Unlock()

	gcTargetsCleared.Observe(float64(cleared))
}

func (m *Mapper) Close(ctx context.Context) {
	m.ticker.Stop() // will also cause GC goroutine to terminate

	m.mu.Lock()
	defer m.mu.Unlock()

	// TODO look into doing this in parallel if shut down times shoot up -
	// getting a SIGKILL because we took too long to shut down cleanly would
	// defeat the purpose of all this
	for _, target := range m.targets {
		collectorCtx, cancel := context.WithTimeout(ctx, time.Second)
		target.collector.Close(collectorCtx)
		cancel()
	}
}
