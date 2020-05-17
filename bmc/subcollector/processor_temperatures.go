package subcollector

import (
	"context"
	"strconv"

	"github.com/gebn/bmc"
	"github.com/gebn/bmc/pkg/ipmi"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	processorTemperature = prometheus.NewDesc(
		"processor_temperature_celsius",
		"The temperature of a processor die in degrees celsius.",
		[]string{"cpu"}, nil,
	)
)

type ProcessorTemperatures struct {
	bmc.Session

	// sensors holds one reader for each CPU temperature sensor. The key is the
	// "cpu" label, as a string to save conversion each scrape. Map iteration
	// order is randomised, but prometheus.Collector does not demand time series
	// are produced in a consistent order, so this is fine.
	sensors map[string]bmc.SensorReader
}

// Initialise identifies processor temperature sensors given an SDR repository.
// The session is only used during collection, which is why this function does
// not accept a context.
func (c *ProcessorTemperatures) Initialise(_ context.Context, s bmc.Session, sdrr bmc.SDRRepository) {
	c.Session = s
	processorFSRs := extractProcessorTempFSRs(sdrr)

	// if we have any sensors under the processor entity ID, prefer those
	fsrs, ok := processorFSRs[ipmi.EntityIDProcessor]
	if !ok {
		// fall back to the deprecated DCMI EntityID (may also be empty); note
		// we never combine sensors from the two entities - it's one or the
		// other
		fsrs = processorFSRs[ipmi.EntityIDDCMIProcessor]
	}

	// we've decided which sensors to read; now get a reader for each of them
	readers := make(map[string]bmc.SensorReader, len(fsrs))
	for _, fsr := range fsrs {
		cpu := strconv.FormatUint(uint64(fsr.Instance), 10)
		reader, err := bmc.NewSensorReader(fsr)
		if err != nil {
			// requires something not yet implemented (e.g. non-linear); skip
			continue // TODO increment metric?
		}
		readers[cpu] = reader
	}
	c.sensors = readers
}

func extractProcessorTempFSRs(sdrr bmc.SDRRepository) map[ipmi.EntityID][]*ipmi.FullSensorRecord {
	sdrs := map[ipmi.EntityID][]*ipmi.FullSensorRecord{}
	for _, fsr := range sdrr {
		// be a little more defensive; in practice I've never seen FSRs for
		// these entity IDs that aren't temperature sensors
		if fsr.SensorType != ipmi.SensorTypeTemperature {
			continue
		}
		if fsr.BaseUnit != ipmi.SensorUnitCelsius {
			// TODO increment a counter? log?
			continue
		}
		switch fsr.Entity {
		case ipmi.EntityIDProcessor:
			sdrs[ipmi.EntityIDProcessor] = append(sdrs[ipmi.EntityIDProcessor], fsr)
		case ipmi.EntityIDDCMIProcessor:
			sdrs[ipmi.EntityIDDCMIProcessor] = append(sdrs[ipmi.EntityIDDCMIProcessor], fsr)
		}
	}
	return sdrs
}

func (c *ProcessorTemperatures) Describe(ch chan<- *prometheus.Desc) {
	ch <- processorTemperature
}

// Collect requests the temperature of each identified CPU, producing a sample
// for each one.
func (c *ProcessorTemperatures) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	for cpu, reader := range c.sensors {
		reading, err := reader.Read(ctx, c.Session)
		if err != nil {
			// machine could be off
			continue
		}
		ch <- prometheus.MustNewConstMetric(
			processorTemperature,
			prometheus.GaugeValue,
			reading,
			cpu,
		)
	}
	return nil
}
