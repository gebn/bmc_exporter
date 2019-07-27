package session

import (
	"context"

	"github.com/gebn/bmc"
)

// Provider is implemented by things that can establish a session with BMCs.
// This exists to abstract the rest of the exporter away from IPMI versions,
// secrets and algorithms.
type Provider interface {

	// Session opens a new session with the BMC and the supplied address. This
	// is the raw "target" string given to us by Prometheus, so in theory it
	// could be anything, but for the sake of compatibility, it is recommended
	// for this to be the bare IP address, possibly with a port number on the
	// end. This method should return an error if the context expires, the addr
	// is not known, or session establishment fails. The exporter will print all
	// errors received, with the requested addr, so it is not necessary to
	// include this in the error string.
	//
	// The exporter will only attempt to call this method once per scrape for a
	// given addr, until it receives no error. We assume currently unknown BMCs
	// will be known at some point in the future.
	//
	// This function must be safe for unbounded concurrent use, however it will
	// never be called concurrently for a given addr. The exporter will also
	// endeavour to close an addr's session before calling this method to obtain
	// a new one. As a BMC may choose to terminate a session at any time, or it
	// may timeout, this method must be safe for use throughout the exporter's
	// lifetime (not just during startup or once per addr).
	//
	// It is strongly recommended for implementations to support hot reloading,
	// to allow BMCs to be added and removed without having to restart the
	// exporter.
	Session(ctx context.Context, addr string) (bmc.Session, error)
}
