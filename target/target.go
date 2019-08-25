package target

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gebn/bmc_exporter/collector"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// handlerOpts is passed when creating a handler for the collector registry.
	handlerOpts = promhttp.HandlerOpts{}
)

type scrapeReqOpts struct {
	ResponseWriter http.ResponseWriter
	Request        *http.Request

	// Done receives a single element when the scrape is complete. The handler
	//should block until it receives a value on this.
	Done chan struct{}
}

// Target is a BMC target. This type provides functions that delegate to an
// event loop run by a single goroutine, freeing us from locking.
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

	scrapeReq := make(chan scrapeReqOpts)
	closeReq := make(chan struct{})

	bmc := &Target{
		collector: c,
		handler:   promhttp.HandlerFor(reg, handlerOpts),
		scrapeReq: scrapeReq,
		closeReq:  closeReq,
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

// Close cleanly terminates the connection and resources associated with the
// BMC. This method must only be called once, otherwise it will panic.
func (t *Target) Close() {
	t.closeReq <- struct{}{}
	t.wg.Wait() // satisfied when event loop has stopped
	close(t.closeReq)
	close(t.scrapeReq)
}

func (t *Target) LastCollection() int64 {
	return t.collector.LastCollection()
}

// ServeHTTP satisfies http.Handler, allowing this BMC to respond to scrape
// requests.
func (t *Target) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	done := make(chan struct{})
	t.scrapeReq <- scrapeReqOpts{
		ResponseWriter: w,
		Request:        r,
		Done:           done,
	}
	<-done
}
