package collector

import (
	"context"
	"encoding/hex"
	"io"
	"sync"
	"time"

	"github.com/gebn/bmc_exporter/session"

	"github.com/gebn/bmc"
	"github.com/gebn/bmc/pkg/dcmi"
	"github.com/gebn/bmc/pkg/ipmi"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// "meta" scrape metrics

	lastScrape = prometheus.NewDesc(
		"bmc_last_scrape",
		"When this BMC was last scraped by this exporter, expressed as "+
			"seconds since the Unix epoch. This metric will not be present "+
			"in the first scrape of a BMC.",
		nil, nil,
	)
	scrapeDuration = prometheus.NewDesc(
		"bmc_scrape_duration_seconds",
		"The amount of time collection of BMC metrics took for this scrape, measured by the exporter.",
		nil, nil,
	)
	up = prometheus.NewDesc(
		"bmc_up",
		"1 if the exporter was able to gather all desired metrics this scrape, 0 otherwise.",
		nil, nil,
	)

	// concrete metrics about a BMC

	bmcInfo = prometheus.NewDesc(
		"bmc_info",
		"Provides the BMC's GUID, manufacturer name, firmware version, and the IPMI version used to scrape it. Constant 1.",
		[]string{
			"guid",         // Get System GUID
			"manufacturer", // Get Device ID
			"firmware",     // Get Device ID
			"ipmi",         // version used for connection
		},
		nil,
	)
	chassisPoweredOn = prometheus.NewDesc(
		"chassis_powered_on",
		"Whether the system is currently turned on, according to Get Chassis Status. If 0, the system could be in S4/S5, or mechanical off.",
		nil, nil,
	)
	chassisIntrusion = prometheus.NewDesc(
		"chassis_intrusion",
		"Whether the system cover is open, according to Get Chassis Status.",
		nil, nil,
	)
	chassisPowerFault = prometheus.NewDesc(
		"chassis_power_fault",
		"Whether a fault has been detected in the main power subsystem, according to Get Chassis Status.",
		nil, nil,
	)
	chassisCoolingFault = prometheus.NewDesc(
		"chassis_cooling_fault",
		"Whether a cooling or fan fault has been detected, according to Get Chassis Status.",
		nil, nil,
	)
	chassisDriveFault = prometheus.NewDesc(
		"chassis_drive_fault",
		"Whether a disk drive in the system is faulty, according to Get Chassis Status.",
		nil, nil,
	)
	chassisPowerConsumption = prometheus.NewDesc(
		"chassis_power_consumption_watts",
		"The system's current power draw. This requires DCMI to be supported and the hardware correctly configured.",
		nil, nil,
	)
)

type Collector struct {

	// Target is the addr of the target this collector is responsible for.
	Target string

	// Provider is the session provider to use to establish new sessions with
	// the BMC when required.
	Provider session.Provider

	// Timeout is the time to allow for each collection before returning what we
	// have. The aim of having this is to ensure Prometheus gets at least a
	// subset of the metrics, even if we don't have time to collect them all.
	Timeout time.Duration

	// commands contains all the IPMI structures for commands we will send to
	// the BMC.
	commands commands

	// session is the session we've established with the target addr, if any.
	// This may have expired since the last collection.
	session bmc.Session

	// closer is a closer for the underlying transport that the current session
	// is operating over. We close this before trying to establish a new
	// session.
	closer io.Closer

	// mux guards the Collect() method, ensuring the target BMC is only scraped
	// once at a time.
	mux sync.Mutex

	// lastCollection holds the time of the previous scrape.
	lastCollection time.Time

	// supportsGetPowerReading indicates whether the BMC supports the DCMI Get
	// Power Reading command. This is discovered after session establishment,
	// and controls whether we bother trying to retrieve the power usage for the
	// remainder of the session.
	supportsGetPowerReading bool
}

// Close cleanly terminates the underlying BMC connection that powers the
// collector. This is used to cleanly shut down the exporter.
func (c *Collector) Close(ctx context.Context) error {
	if err := c.session.Close(ctx); err != nil {
		return err
	}
	return c.closer.Close()
}

func (c *Collector) Describe(d chan<- *prometheus.Desc) {
	// descriptors are all pre-allocated; we simply send them
	// N.B. this method must be concurrent
	d <- lastScrape
	d <- scrapeDuration
	d <- up
	d <- bmcInfo
	d <- chassisPoweredOn
	d <- chassisIntrusion
	d <- chassisPowerFault
	d <- chassisCoolingFault
	d <- chassisDriveFault
	d <- chassisPowerConsumption
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	c.mux.Lock()
	defer c.mux.Unlock() // critical section could be shorter, but this is safer

	if !c.lastCollection.IsZero() {
		ch <- prometheus.MustNewConstMetric(
			lastScrape,
			prometheus.GaugeValue,
			float64(c.lastCollection.UnixNano())/float64(time.Second),
		)
	}
	c.lastCollection = start

	success := false
	// ensure we have a working session before continuing
	if err := c.prescrape(ctx); err == nil {

		// collect bmc-specific metrics
		if err := c.collect(ctx, ch); err == nil {
			success = true
		}
	}

	ch <- prometheus.MustNewConstMetric(
		up,
		prometheus.GaugeValue,
		boolToFloat64(success),
	)

	duration := time.Since(start)
	ch <- prometheus.MustNewConstMetric(
		scrapeDuration,
		prometheus.GaugeValue,
		duration.Seconds(),
	)
}

func (c *Collector) prescrape(ctx context.Context) error {
	if c.session != nil {
		probeCtx, cancel := context.WithTimeout(ctx, time.Second*2)
		defer cancel()

		// we use Get Channel Authentication Capabilities as a liveness probe;
		// this command is typically used for session keepalives
		if err := bmc.ValidateResponse(c.session.SendCommand(probeCtx,
			&c.commands.getChannelAuthenticationCapabilities)); err == nil {
			// we hope this will be the most common case
			return nil
		}

        // failed, session or transport is bad. We ditch the entire socket
        // rather than only the session-based connection, just in case. Allow
        // another second as probeCtx will likely have expired
		cancelCtx, cancel := context.WithTimeout(ctx, time.Second)
		_ = c.Close(cancelCtx)
		cancel()
	}

	if err := c.newSession(ctx); err != nil {
		return err
	}

	return nil
}

// newSession establishes a session with a BMC, and performs discovery to
// relieve subsequent scrapes of doing this. This method will only return a nil
// error if c.session != nil.
func (c *Collector) newSession(ctx context.Context) error {
	sess, closer, err := c.Provider.Session(ctx, c.Target)
	if err != nil {
		return err
	}
	c.session = sess
	c.closer = closer
	c.supportsGetPowerReading = true

	// set request struct fields based on capabilities; as these structs are
	// specific to this collector, we only have to do this once, and can tailor
	// it based on what we discover
	c.commands.getChannelAuthenticationCapabilities.Req.Channel = ipmi.ChannelPresentInterface
	c.commands.getChannelAuthenticationCapabilities.Req.MaxPrivilegeLevel = ipmi.PrivilegeLevelUser
	c.commands.getPowerReading.Req.Mode = dcmi.SystemPowerStatisticsModeNormal

	if err := bmc.ValidateResponse(c.session.SendCommand(ctx,
		&c.commands.getPowerReading)); err != nil {
		// let's not try that again
		c.supportsGetPowerReading = false
	}

	return nil
}

func (c *Collector) collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	if err := c.bmcInfo(ctx, ch); err != nil {
		return err
	}
	if err := c.chassisStatus(ctx, ch); err != nil {
		return err
	}
	return nil
}

func (c *Collector) bmcInfo(ctx context.Context, ch chan<- prometheus.Metric) error {
	if err := bmc.ValidateResponse(c.session.SendCommand(ctx, &c.commands.getSystemGUID)); err != nil {
		return err
	}
	if err := bmc.ValidateResponse(c.session.SendCommand(ctx, &c.commands.getDeviceID)); err != nil {
		return err
	}
	guidBuf := [36]byte{}
	encodeHex(guidBuf[:], c.commands.getSystemGUID.Rsp.GUID)
	ch <- prometheus.MustNewConstMetric(
		bmcInfo,
		prometheus.GaugeValue,
		1,
		string(guidBuf[:]),
		c.commands.getDeviceID.Rsp.Manufacturer.Organisation(),
		bmc.FirmwareVersion(&c.commands.getDeviceID.Rsp),
		c.session.Version(),
	)
	return nil
}

func encodeHex(dst []byte, guid [16]byte) {
	hex.Encode(dst, guid[:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], guid[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], guid[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], guid[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:], guid[10:])
}

func (c *Collector) chassisStatus(ctx context.Context, ch chan<- prometheus.Metric) error {
	if err := bmc.ValidateResponse(c.session.SendCommand(
		ctx, &c.commands.getChassisStatus)); err != nil {
		return err
	}

	rsp := &c.commands.getChassisStatus.Rsp

	ch <- prometheus.MustNewConstMetric(
		chassisPoweredOn,
		prometheus.GaugeValue,
		boolToFloat64(rsp.PoweredOn),
	)
	ch <- prometheus.MustNewConstMetric(
		chassisIntrusion,
		prometheus.GaugeValue,
		boolToFloat64(rsp.Intrusion),
	)
	ch <- prometheus.MustNewConstMetric(
		chassisPowerFault,
		prometheus.GaugeValue,
		boolToFloat64(rsp.PowerFault),
	)
	ch <- prometheus.MustNewConstMetric(
		chassisCoolingFault,
		prometheus.GaugeValue,
		boolToFloat64(rsp.CoolingFault),
	)
	ch <- prometheus.MustNewConstMetric(
		chassisDriveFault,
		prometheus.GaugeValue,
		boolToFloat64(rsp.DriveFault),
	)

	return nil
}

func boolToFloat64(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
