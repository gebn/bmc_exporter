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
	// session. We do it this way to avoid allocating a new subcollector struct
	// each session; instead, subcollectors are bound to the lifetime of the
	// target's prometheus.Collector. This is also necessary to ensure
	// Describe() can be called on that collector at any time.
	//
	// This method will always be called before the first call to Collect(). It
	// may be called multiple times, so should ensure previously initialised
	// state is cleared up correctly.
	//
	// The SDR repo is passed as an optimisation to relieve multiple
	// subcollectors from having to retrieve this several times themselves; it
	// should not be modified.
	//
	// Initialise should (re)allocate as little memory as possible. For example,
	// subcollectors should contain all their commands as fields, lasting for
	// the lifetime of the object, so no allocations occur during either
	// initialisation or collection.
	Initialise(context.Context, bmc.Session, bmc.SDRRepository)
	// TODO consider allowing an error to be returned - but what would it achieve?

	// Describe is identical to prometheus.Collector's Describe() method. It
	// should statically send all descriptors for metrics the subcollector is
	// capable of yielding to the provided channel.
	Describe(chan<- *prometheus.Desc)

	// Collect sends the relevant commands to the BMC, yielding appropriate
	// metrics. It must return when the context is cancelled. An error should be
	// returned if collection fails to complete successfully, e.g. if the
	// context expires before a valid response is received. Note it should not
	// return an error if a sensor cannot be read, as the machine could be
	// turned off. It is acceptable for this method to panic (e.g. segfault due
	// to nil session) if Initialise() was not called before.
	Collect(context.Context, chan<- prometheus.Metric) error

	// TODO may need a Close() at some point
}
