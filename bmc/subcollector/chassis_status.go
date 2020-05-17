package subcollector

import (
	"context"

	"github.com/gebn/bmc"
	"github.com/gebn/bmc/pkg/ipmi"

	"github.com/prometheus/client_golang/prometheus"
)

var (
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
)

type ChassisStatus struct {
	bmc.Session

	getChassisStatus ipmi.GetChassisStatusCmd
}

func (s *ChassisStatus) Initialise(_ context.Context, sess bmc.Session, _ bmc.SDRRepository) error {
	s.Session = sess
	return nil
}

func (*ChassisStatus) Describe(ch chan<- *prometheus.Desc) {
	ch <- chassisPoweredOn
	ch <- chassisIntrusion
	ch <- chassisPowerFault
	ch <- chassisCoolingFault
	ch <- chassisDriveFault
}

func (s *ChassisStatus) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	if err := bmc.ValidateResponse(s.SendCommand(ctx, &s.getChassisStatus)); err != nil {
		return err
	}

	rsp := &s.getChassisStatus.Rsp

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
