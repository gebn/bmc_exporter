package bmc

import (
	"net/http"
	"sync"
	"time"

	"github.com/gebn/bmc_exporter/collector"
	"github.com/gebn/bmc_exporter/session"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Mapper manages the http.Handler we create for each BMC being scraped. Given a
// target addr, it returns a promhttp-created handler that, when invoked, will
// retrieve and yield metrics for that BMC.
type Mapper struct {
	Provider session.Provider
	Timeout  time.Duration

	handlers map[string]http.Handler
	mu       sync.RWMutex
}

// NewMapper creates a Mapper struct ready for mapping targets to handlers.
func NewMapper(provider session.Provider, timeout time.Duration) *Mapper {
	// this function is only needed because maps initialise to nil, which we
	// cannot assign to... we could also do this in Handler(), but that's messy
	return &Mapper{
		Provider: provider,
		Timeout:  timeout,
		handlers: map[string]http.Handler{},
	}
}

// Handler returns a http.Handler that returns metrics collected from the BMC at
// the provided address. If the address has been requested before, this will
// return the original handler, otherwise it will create a new one. It is
// effectively a synchronised, lazy map access.
func (m *Mapper) Handler(addr string) http.Handler {
	// first, try with a read lock, optimistically assuming it's there; this
	// will be the case most of the time
	m.mu.RLock()
	handler, ok := m.handlers[addr]
	m.mu.RUnlock()
	if ok {
		return handler
	}

	// unlucky, first scrape, need to take out a write lock and create the
	// structures
	m.mu.Lock()
	defer m.mu.Unlock()

	// in the time taken to acquire the lock, another goroutine may have done
	// this, so check once more
	if handler, ok := m.handlers[addr]; ok {
		return handler
	}

	// nope, still don't have it, create. This could panic, but if it does, it
	// will fail for every request, so we'll realise pretty quickly.
	reg := prometheus.NewRegistry()
	reg.MustRegister(&collector.Collector{
		Target:   addr,
		Provider: m.Provider,
		Timeout:  m.Timeout,
	})
	handler = promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	m.handlers[addr] = handler
	return handler
}

// TODO instrument hits and misses as if we were a cache - we essentially are
// TODO GC
