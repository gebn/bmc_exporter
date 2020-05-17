package collector

import (
	"context"

	"github.com/gebn/bmc"

	"github.com/prometheus/client_golang/prometheus"
)

// Subcollector is implemented by things that know how to produce a subset of a
// BMC's metrics. We use this rather than prometheus.Collector, as these
// collections have a timeout and must be cancellable. This is very similar to
// the node_exporter's Collector interface.
type Subcollector interface {

	// Initialise configures this subcollector to work with the provided
	// session, doing any relevant detection. It is expected that the
	// subcollector retain the session. We do it this way to avoid allocating a
	// new subcollector struct each session; instead, subcollectors are bound to
	// the lifetime of the target's prometheus.Collector. This is also necessary
	// to ensure Describe() can be called on that collector at any time.
	//
	// Whether this method should return an error is an interesting discussion.
	// The obvious case is to avoid incomplete initialisation, resulting in us
	// ignoring features of the BMC (e.g. DCMI) because we didn't have enough
	// time to detect them. This would lead to missing metrics for the entire
	// length of the session. The issue is BMCs have a habit of ignoring
	// commands they don't know about. A missed deadline could be us taking too
	// long (e.g. packet loss, high latency), or it could be the BMC ignoring
	// us; we can never be sure which. This is the case with all commands that
	// could be sent in this method. If the implementation is sure it is not
	// sending a command the BMC may not support, it can return an error to void
	// the session and retry next scrape. This is a signal for a human to
	// investigate why it's taking so long.
	//
	// The SDR repo is passed as an optimisation to relieve multiple
	// subcollectors from having to retrieve this several times themselves; it
	// should not be modified.
	//
	// Initialise should (re)allocate as little memory as possible. For example,
	// subcollectors should contain all their commands as fields, lasting for
	// the lifetime of the object, so no allocations occur during initialisation
	// or collection.
	Initialise(context.Context, bmc.Session, bmc.SDRRepository) error

	// Describe is identical to prometheus.Collector's Describe() method. It
	// should statically send all descriptors for metrics the subcollector is
	// capable of yielding to the provided channel.
	Describe(chan<- *prometheus.Desc)

	// Collect sends the relevant commands to the BMC, yielding appropriate
	// metrics. It must return when the context is cancelled. An error should be
	// returned if collection fails to complete successfully, e.g. if the
	// context expires before a valid response is received. Note it should not
	// return an error if the BMC does not support its function, or if a sensor
	// cannot be read, as the machine could be turned off. It is acceptable for
	// this method to panic (e.g. segfault due to nil session) if Initialise()
	// was not called before.
	Collect(context.Context, chan<- prometheus.Metric) error

	// I haven't implemented a subcollector with any state needing explicit
	// Close()ing, so this interface lacks that method.
}
