package subcollector

import (
	"context"
	"encoding/hex"

	"github.com/gebn/bmc"
	"github.com/gebn/bmc/pkg/ipmi"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	bmcInfo = prometheus.NewDesc(
		"bmc_info",
		"Provides the BMC's GUID, firmware, and the version of IPMI used to scrape it. Constant 1.",
		[]string{
			"guid",     // Get System GUID
			"firmware", // Get Device ID
			"ipmi",     // version used for connection
		},
		nil,
	)
)

type BMCInfo struct {
	bmc.Session

	getSystemGUID ipmi.GetSystemGUIDCmd
	getDeviceID   ipmi.GetDeviceIDCmd
}

func (c *BMCInfo) Initialise(_ context.Context, s bmc.Session, _ bmc.SDRRepository) error {
	c.Session = s
	return nil
}

func (*BMCInfo) Describe(ch chan<- *prometheus.Desc) {
	ch <- bmcInfo
}

func (c *BMCInfo) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	if err := bmc.ValidateResponse(c.SendCommand(ctx, &c.getSystemGUID)); err != nil {
		return err
	}
	if err := bmc.ValidateResponse(c.SendCommand(ctx, &c.getDeviceID)); err != nil {
		return err
	}
	guidBuf := [36]byte{}
	encodeHex(guidBuf[:], c.getSystemGUID.Rsp.GUID)
	ch <- prometheus.MustNewConstMetric(
		bmcInfo,
		prometheus.GaugeValue,
		1,
		string(guidBuf[:]),
		bmc.FirmwareVersion(&c.getDeviceID.Rsp),
		c.Session.Version(),
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
