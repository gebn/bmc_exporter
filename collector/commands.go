package collector

import (
	"github.com/gebn/bmc/pkg/dcmi"
	"github.com/gebn/bmc/pkg/ipmi"
)

// commands contains all the layers needed to perform a collection. This is a
// single allocation, lasting for the lifetime of the collector, meaning there
// are no layer allocations during collection.
type commands struct {
	getChannelAuthenticationCapabilities ipmi.GetChannelAuthenticationCapabilitiesCmd
	getSystemGUID                        ipmi.GetSystemGUIDCmd
	getDeviceID                          ipmi.GetDeviceIDCmd
	getChassisStatus                     ipmi.GetChassisStatusCmd
	getPowerReading                      dcmi.GetPowerReadingCmd
}
