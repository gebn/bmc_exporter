package target

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gebn/bmc_exporter/bmc/collector"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	namespace = "bmc"
	subsystem = "target"

	// handlerOpts is passed when creating a handler for the collector registry.
	handlerOpts = promhttp.HandlerOpts{}

	scrapeDispatchLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "scrape_dispatch_latency_seconds",
		Help: "Observes the duration spent waiting for the event loop to " +
			"pick up scrape requests.",
		Buckets: prometheus.ExponentialBuckets(0.1, 1.8, 10), // 19.84
	})

	abandonedRequests = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "abandoned_requests_total",
		Help: "The number of scrapes we have abandoned before the client's " +
			"request got to the front of the queue for the BMC, either " +
			"because they gave up or one of our timeouts fired. This " +
			"indicates an overly short scrape timeout and/or interval.",
	})
)

type scrapeReqOpts struct {
	ResponseWriter http.ResponseWriter
	Request        *http.Request

	// Done receives a single element when the scrape is complete. The handler
	//should block until it receives a value on this.
	Done chan struct{}

	// Created is used for instrumenting the time spent waiting for the event
	// loop. This goes into the scrapeDispatchLatency metric.
	Created time.Time
}

// Target is the outermost wrapper around a BMC being scraped. It encapsulates
// the Collector implementation, implementing an event loop around it. This
// serialises access to a BMC, freeing us from locking.
type Target struct {
	collector *collector.Collector

	// handler is the underlying promhttp handler that displays the metrics
	// page. Note that this struct also implements http.Handler, but only
	// performs management around delegating to this.
	handler http.Handler

	scrapeReq chan scrapeReqOpts
	closeReq  chan struct{}

	// wg becomes done when the event loop has stopped.
	wg sync.WaitGroup
}

// New constructs and starts a new BMC target. Be sure to call Close() when
// finished with it to terminate the event loop and underlying BMC connection.
func New(c *collector.Collector) *Target {
	reg := prometheus.NewRegistry()
	reg.MustRegister(c)

	bmc := &Target{
		collector: c,
		handler:   promhttp.HandlerFor(reg, handlerOpts),
		scrapeReq: make(chan scrapeReqOpts),
		closeReq:  make(chan struct{}),
	}

	bmc.wg.Add(1)
	go bmc.eventLoop()
	return bmc
}

func (t *Target) eventLoop() {
	defer t.wg.Done()
	for {
		// the fact we can only do one thing at once ensures requests to a given
		// BMC are serialised
		select {
		case req := <-t.scrapeReq:
			// we don't worry about receiving a default struct, as the
			// channel is only closed when this loop has been broken out of.

			scrapeDispatchLatency.Observe(time.Since(req.Created).Seconds())

			// this is utterly hacky, however it is a safe workaround for
			// getting a context inside the Collect() method, made possible by
			// the fact that each collector is only called once at a time. This
			// allows us to implement end-to-end timeouts without creating lots
			// of garbage each request for new Prometheus structs.
			t.collector.Context = req.Request.Context()

			// N.B. use of underlying handler - calling t.ServeHTTP would cause
			// a stack overflow
			t.handler.ServeHTTP(req.ResponseWriter, req.Request)
			req.Done <- struct{}{}
		case <-t.closeReq:
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
			t.collector.Close(ctx)
			cancel()
			return
		}
	}
}

// ServeHTTP satisfies http.Handler, allowing this BMC to respond to scrape
// requests.
func (t *Target) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	done := make(chan struct{})
	ctx := r.Context()
	opts := scrapeReqOpts{
		ResponseWriter: w,
		Request:        r,
		Done:           done,
		Created:        time.Now(),
	}

	// this ensures we don't clog up the event loop with requests that have
	// either already been abandoned by the client, or deemed too old by one of
	// our timeouts. This was first implemented before an end-to-end request
	// timeout, where blocking indefinitely on the chan send effectively caused
	// a goroutine leak (#34), however it is still useful for clearing the way
	// for fresh scrapes as soon as possible.
	//
	// Note a small number of requests briefly blocked here is normal,
	// especially with multiple prometheis scraping simultaneously - this is
	// just the exporter doing its job of serialising access to the BMC.
	select {
	case t.scrapeReq <- opts:
		<-done
	case <-ctx.Done():
		abandonedRequests.Inc()
		http.Error(w, ctx.Err().Error(), http.StatusServiceUnavailable)
	}
}

func (t *Target) LastCollection() int64 {
	return t.collector.LastCollection()
}

// Close cleanly terminates the connection and resources associated with the
// BMC. This method must only be called once, otherwise it will panic.
func (t *Target) Close() {
	t.closeReq <- struct{}{}
	t.wg.Wait() // satisfied when event loop has stopped
	close(t.closeReq)
	close(t.scrapeReq)
}
