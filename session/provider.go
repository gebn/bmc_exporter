package session

import (
	"context"
	"io"

	"github.com/gebn/bmc"
)

// Provider is implemented by things that can establish a session with BMCs.
// This exists to abstract the rest of the exporter away from IPMI versions,
// secrets and algorithms.
type Provider interface {

	// Session opens a new session with the BMC at the supplied address,
	// returning it and a closer for the underlying transport. This is the raw
	// "target" string given to us by Prometheus, so in theory it could be
	// anything, but for the sake of compatibility, it is recommended for this
	// to be the bare IP address, possibly with a port number on the end. This
	// method should return an error if the context expires, the addr is not
	// known, or session establishment fails. The returned session and closer
	// must be nil if the error is non-nil. The exporter will print all errors
	// received, with the requested addr, so it is not necessary to include this
	// in the error string.
	//
	// The exporter will call this method a maximum of once per scrape. We
	// assume currently unknown BMCs will be known at some point in the future.
	// It is recommended for implementations to retry their credential retrieval
	// logic as makes sense (e.g. perhaps not for a local file, but definitely
	// for a remote service), and to retry session creation. The caller of this
	// method does not itself retry as this allows implementations to retry more
	// efficiently, e.g. by reusing data common between retries. We can also
	// provide abstractions to retry session creation if necessary, making that
	// part easy. Essentially, it comes down to flexibility.
	//
	// For the sake of performance, this function must be safe for unbounded
	// concurrent use, with the guarantee that it will never be called
	// concurrently for a given addr value. The exporter will also endeavour to
	// close an addr's session before calling this method to obtain a new one.
	// As a BMC may choose to terminate a session at any time, or it may
	// timeout, this method must be safe for use throughout the exporter's
	// lifetime (not just during startup or once per addr).
	//
	// It is strongly recommended for implementations to support hot reloading,
	// to allow BMCs to be added and removed without having to restart the
	// exporter.
	Session(ctx context.Context, addr string) (bmc.Session, io.Closer, error)
}

// Don't mix up providers indicating they don't know about an addr with
// back-offs: the former means "I don't have credentials", and is only relevant
// until the next reload at the latest (in the case of the file provider; the
// hypothetical Vault provider will get new BMCs at any time without a reload)
// and the latter "I have credentials, but failed to establish a session". We
// don't want a sentinel error indicating "Don't ask me again", as that is
// bespoke to providers using a static config, which is a special case. A map
// lookup takes next to no time, so don't design a system around avoiding it.
// Query the session provider once each scrape, let it return quickly if it
// doesn't know about the addr, and do its own retrying if it does. The name is
// Provider, not ProviderButRetryMeIfYouGetAnErrorKThxBye.
//
// Returning the io.Closer is messy, but there are not many ways around this,
// short of giving the provider an already-open UDP socket, but then it would
// not be able to create the session-less connection.
