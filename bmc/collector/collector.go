package collector

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/gebn/bmc_exporter/bmc/subcollector"
	"github.com/gebn/bmc_exporter/session"

	"github.com/gebn/bmc"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	namespace = "bmc"
	subsystem = "collector"

	collectDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "collect_duration_seconds",
		Help:      "Observes the time taken by each BMC collection.",
	})
	providerRequests = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "provider_requests_total",
		Help:      "The number of requests made to a session provider.",
	})
	initialiseTimeouts = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "initialise_timeouts_total",
		Help: "The number of times we established a session, but failed " +
			"to retrieve the SDR repo and initialise the subcollectors before " +
			"we timed out.",
	})
	sessionExpiriesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "session_expiries_total",
		Help:      "The number of sessions that have stopped working.",
	})

	// "meta" scrape metrics
	up = prometheus.NewDesc(
		"bmc_up",
		"1 if the exporter was able to establish a session, 0 otherwise.",
		nil, nil,
	)
	scrapeDuration = prometheus.NewDesc(
		"bmc_scrape_duration_seconds",
		"The time taken to collect all metrics, measured by the exporter.",
		nil, nil,
	)
)

// Collector implements the custom collector to scrape metrics from a single BMC
// on demand. Note this struct is only called by a single Target by a single
// goroutine, so contrary to the Prometheus docs, is not safe for concurrent
// use.
type Collector struct {

	// lastCollection holds the time of the previous scrape as nanoseconds since
	// the Unix epoch.
	//
	// GC, which is triggered on a timer outside the usual dispatch mechanism,
	// uses this value to decide which targets to clear up. Therefore it must be
	// retrievable at any time, including while a scrape is in progress. It
	// would be a time.Time, however an int allows us to use atomic operations
	// rather than a full-blown mutex, and avoids potentially thousands of
	// allocations and less efficient comparisons inside mapper while holding
	// the write lock. Although its value will always be >=0, it is signed to
	// match time.Time.UnixNano()'s return type.
	//
	// This is the first field to ensure it is 64-bit aligned, for the sake of
	// ARM, x86-32, and 32-bit MIPS:
	// https://golang.org/pkg/sync/atomic/#pkg-note-BUG
	lastCollection int64

	// Target is the addr of the target this collector is responsible for.
	Target string

	// Provider is the session provider to use to establish new sessions with
	// the BMC when required.
	Provider session.Provider

	// Timeout is the time to allow for each collection before returning what we
	// have. This exists to ensure fairness when multiple scrapers are querying
	// the exporter for a given BMC. Collection will return when this duration
	// has passed, or the context expires (whichever happens first).
	Timeout time.Duration

	// Context is our way of passing a context to the Collect() method, so we
	// can implement an end-to-end timeout for the scrape in the exporter. The
	// ultimate aim is to give up scraping before Prometheus gives up on us, so
	// we can return at least a subset of metrics, even if we don't have time to
	// collect them all.
	//
	// This is a huge hack, but it is safe here. Target sets this before calling
	// ServeHTTP() on the http.Handler returned by promhttp for this collector's
	// registry. As access to a given target is serialised by the event loop,
	// there is no race, and no locking is required. See #13 for more context
	// (ba dum tss).
	Context context.Context

	bmcInfo               subcollector.BMCInfo
	chassisStatus         subcollector.ChassisStatus
	processorTemperatures subcollector.ProcessorTemperatures
	powerDraw             subcollector.PowerDraw

	// session is the session we've established with the target addr, if any.
	// This will be nil if no collection has been attempted, or if
	// initialisation failed, or the collector has been closed. It may also have
	// expired since the last collection.
	session bmc.Session

	// closer is a closer for the underlying transport that the current session
	// is operating over. We close this before trying to establish a new
	// session.
	closer io.Closer
}

// LastCollection returns when this collector was last invoked as nanoseconds
// since the Unix epoch. If required, it can be converted to a time.Time using
// time.Unix(0, <value>). It is used by mapper GC to determine which BMCs are no
// longer being scraped, so their http.Handlers can be removed.
func (c *Collector) LastCollection() int64 {

	// this doesn't use the normal event dispatch mechanism, as we need to
	// obtain this value quickly, as all incoming scrapes are blocked - we don't
	// want to be waiting for current scrapes to finish
	return atomic.LoadInt64(&c.lastCollection)
}

func (c *Collector) Describe(d chan<- *prometheus.Desc) {
	// descriptors are all pre-allocated; we simply send them
	d <- up
	d <- scrapeDuration

	// ask each subcollector to describe itself; this is partly why these
	// objects have the same lifetime as this collector (the other reason being
	// avoiding reallocs)
	c.bmcInfo.Describe(d)
	c.chassisStatus.Describe(d)
	c.processorTemperatures.Describe(d)
	c.powerDraw.Describe(d)
}

// Collect sends a number of commands to the BMC to gather metrics about its
// current state.
//
// Contrary to the Prometheus docs, this method is *not* safe for calling
// concurrently: a given BMC is always collected by the same goroutine, so
// concurrent collections are not possible. If it's possible for Prometheus to
// do a spurious collection, then we're in trouble, so we don't implement the
// locking to add complexity and give a false sense of security, when in reality
// there would be deadlocks.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(c.Context, c.Timeout)
	defer cancel()

	// this timestamp is used by GC to determine when this target can be deleted
	atomic.StoreInt64(&c.lastCollection, start.UnixNano())

	c.collect(ctx, ch) // TODO do something with error?

	elapsed := time.Since(start)
	collectDuration.Observe(elapsed.Seconds())
	ch <- prometheus.MustNewConstMetric(
		scrapeDuration,
		prometheus.GaugeValue,
		elapsed.Seconds(),
	)
}

func (c *Collector) collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	// N.B. once a session is established, we assume it will not be invalidated
	// in the same scrape. If this is invalid, we have problems - restarting a
	// session involves potentially hundreds of commands to enumerate the SDR.
	if c.session == nil {
		// first scrape, target GCd since last scrape
		if err := c.newSession(ctx); err != nil {
			ch <- prometheus.MustNewConstMetric(up, prometheus.GaugeValue, 0)
			return fmt.Errorf("could not obtain session for %v: %v", c.Target, err)
		}
		if err := c.bmcInfo.Collect(ctx, ch); err != nil {
			// assume the session is fine - we've only just created it - we'll
			// try again next scrape with a shorter timeout and recreate if
			// necessary
			return err
		}
	} else {
		// we have a session, but the BMC might have forgotten about it; use the
		// BMCInfo subcollector as a canary to test
		canaryCtx, canaryCancel := context.WithTimeout(ctx, time.Second*2)
		defer canaryCancel()
		if err := c.bmcInfo.Collect(canaryCtx, ch); err != nil {
			// the commands we send here should always succeed, so our session's
			// either expired, or we've hit a BMC bug. Try again from fresh;
			// resetting only the session is not enough, as a response packet
			// from the last session could confuse things.
			sessionExpiriesTotal.Inc()

			// limit the close to a second; it's unlikely we'll get a reply if
			// the session really has expired
			closeCtx, closeCancel := context.WithTimeout(ctx, time.Second)
			defer closeCancel()
			c.Close(closeCtx)
			if err := c.newSession(ctx); err != nil {
				// give up; newSession() ensures we're left in a clean state
				ch <- prometheus.MustNewConstMetric(up, prometheus.GaugeValue, 0)
				return fmt.Errorf("could not obtain session for %v: %v", c.Target, err)
			}
			// retry as normal
			if err := c.bmcInfo.Collect(ctx, ch); err != nil {
				return err
			}
		}
	}
	// we could establish a session: BMC is healthy
	ch <- prometheus.MustNewConstMetric(up, prometheus.GaugeValue, 1)

	// TODO probably should be two different methods...?

	// let each subcollector do its thing. We break on error as the only reason
	// for an error is ctx expiry, in which case there is no time to send any
	// more commands so we return what we have. This could be done in parallel,
	// however very little computation is done - it's really just sending
	// commands, which have to be serialised anyway, so there would be little
	// gain.
	if err := c.chassisStatus.Collect(ctx, ch); err != nil {
		return err
	}
	if err := c.processorTemperatures.Collect(ctx, ch); err != nil {
		return err
	}
	if err := c.powerDraw.Collect(ctx, ch); err != nil {
		return err
	}
	return nil
}

// newSession establishes a session with a BMC, and performs discovery to
// relieve subsequent scrapes of doing this. This method does not close any
// existing session. It is not necessary to call Close() if it returns a non-nil
// error.
func (c *Collector) newSession(ctx context.Context) error {
	// general point: if there's one thing to be *really* careful of, it's
	// partially initialised sessions. We have to ensure everything is
	// initialised successfully, or the collector is left in a clean state for a
	// retry next time, otherwise we end up trying to invoke methods on a nil
	// session which doesn't end well.

	providerRequests.Inc() // TODO should be moved to provider itself?
	session, closer, err := c.Provider.Session(ctx, c.Target)
	// setting these here in the error case avoids repeatedly trying to close,
	// preventing a negative number of open sessions/connections
	c.session = session
	c.closer = closer
	if err != nil {
		// provider interface guarantees c.session and c.closer are now nil
		return err
	}

	sdrr, err := bmc.RetrieveSDRRepository(ctx, session)
	if err != nil {
		c.Close(ctx) // otherwise collector is left partially initialised
		initialiseTimeouts.Inc()
		return err
	}
	subcollectors := []Subcollector{
		&c.chassisStatus,
		&c.bmcInfo,
		&c.processorTemperatures,
		&c.powerDraw,
	}
	for _, subcollector := range subcollectors {
		if err := subcollector.Initialise(ctx, session, sdrr); err != nil {
			c.Close(ctx)
			initialiseTimeouts.Inc()
			return err
		}
	}
	return nil
}

// Close cleanly terminates the underlying BMC connection and socket that powers
// the collector. The collector is left in a usable state - calling Collect()
// will re-establish a connection. The context constrains the time allowed to
// execute the Close Session command. This is used when the connection is
// thought to have expired, and when shutting down the entire exporter.
func (c *Collector) Close(ctx context.Context) {
	// the session can be nil if the BMC is yet to be scraped, or failed to be
	// scraped
	if c.session == nil {
		return
	}

	c.session.Close(ctx)
	// if session is non-nil, c.closer will always be non-nil; always close,
	// even if session close fails
	c.closer.Close()

	c.session = nil
	c.closer = nil
}
