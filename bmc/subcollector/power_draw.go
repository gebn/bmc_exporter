package subcollector

import (
	"context"
	"errors"
	"strconv"

	"github.com/gebn/bmc"
	"github.com/gebn/bmc/pkg/dcmi"
	"github.com/gebn/bmc/pkg/ipmi"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	ErrPowerMgmtInactive = errors.New("BMC indicated power measurement is inactive")

	powerDraw = prometheus.NewDesc(
		"power_draw_watts",
		"The instantaneous amount of electricity being used by the machine.",
		[]string{"psu"}, nil,
	)
)

// TODO needs reworking to try the SDRR, then fall back to overall via DCMI

type PowerDraw struct {
	bmc.Session

	// sensors holds one reader for each PSU wattage sensor. The key is the
	// "psu" label, as a string to save conversion each scrape. Map iteration
	// order is randomised, but prometheus.Collector does not demand time series
	// are produced in a consistent order, so this is fine.
	sensors map[string]bmc.SensorReader

	// supportsGetPowerReading indicates whether the BMC supports the DCMI Get
	// Power Reading command.
	supportsGetPowerReading bool

	getPowerReading dcmi.GetPowerReadingCmd
}

func (c *PowerDraw) Initialise(ctx context.Context, s bmc.Session, sdrr bmc.SDRRepository) {
	c.Session = s
	fsrs := extractPowerSupplyFSRs(sdrr)
	if len(fsrs) > 0 {
		// the SDR repo's given us some sensors; now get a reader for each of them
		readers := make(map[string]bmc.SensorReader, len(fsrs))
		for _, fsr := range fsrs {
			psu := strconv.FormatUint(uint64(fsr.Instance), 10)
			reader, err := bmc.NewSensorReader(fsr)
			if err != nil {
				// requires something not yet implemented (e.g. non-linear); skip
				continue // TODO increment metric?
			}
			readers[psu] = reader
		}
		c.sensors = readers
		return
	}

	// fall back to DCMI, which gives a single reading for the whole machine
	c.getPowerReading = dcmi.GetPowerReadingCmd{
		Req: dcmi.GetPowerReadingReq{
			Mode: dcmi.SystemPowerStatisticsModeNormal,
		},
	}

	c.supportsGetPowerReading = true
	if err := bmc.ValidateResponse(s.SendCommand(ctx, &c.getPowerReading)); err != nil {
		// let's not try that again
		c.supportsGetPowerReading = false
	}
}

func extractPowerSupplyFSRs(sdrr bmc.SDRRepository) []*ipmi.FullSensorRecord {
	fsrs := []*ipmi.FullSensorRecord{}
	for _, fsr := range sdrr {
		// sensor type for power draw is Other (0x0b), so not helpful for
		// filtering here
		if fsr.BaseUnit != ipmi.SensorUnitWatts {
			// TODO increment a counter? log?
			continue
		}
		if fsr.Entity != ipmi.EntityIDPowerSupply {
			continue
		}
		fsrs = append(fsrs, fsr)
	}
	return fsrs
}

func (c *PowerDraw) Describe(ch chan<- *prometheus.Desc) {
	ch <- powerDraw
}

func (c *PowerDraw) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	switch {
	case len(c.sensors) > 0:
		// use SDR
		for psu, reader := range c.sensors {
			reading, err := reader.Read(ctx, c.Session)
			if err != nil {
				// machine could be off
				continue
			}
			ch <- prometheus.MustNewConstMetric(
				powerDraw,
				prometheus.GaugeValue,
				reading,
				psu,
			)
		}
	case c.supportsGetPowerReading:
		if err := bmc.ValidateResponse(c.SendCommand(ctx, &c.getPowerReading)); err != nil {
			return err
		}
		rsp := &c.getPowerReading.Rsp
		if !rsp.Active {
			return nil // TODO ErrPowerMgmtInactive?
		}
		ch <- prometheus.MustNewConstMetric(
			powerDraw,
			prometheus.GaugeValue,
			float64(rsp.Instantaneous),
		)
	}
	return nil
}
