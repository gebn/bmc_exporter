package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gebn/bmc_exporter/collector"
	"github.com/gebn/bmc_exporter/session/file"

	"github.com/alecthomas/kingpin"
	"github.com/gebn/go-stamp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	help = "An IPMI v1.5/2.0 Prometheus exporter."

	namespace = "bmc"

	buildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "exporter",
			Name:      "build_info",
			Help: "Indicates the version and commit from which the running " +
				"exporter was built. Always has a value of 1.",
		},
		// the runtime version is already exposed by the default Go collector
		[]string{"version", "commit"},
	)

	buildTime = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "exporter",
		Name:      "build_time",
		Help: "When the running exporter was build, expressed as seconds since" +
			"the Unix Epoch.",
	})
)

func main() {
	buildInfo.WithLabelValues(stamp.Version, stamp.Commit).Set(1)
	buildTime.Set(float64(stamp.Time().UnixNano()) / float64(time.Second))

	kingpin.CommandLine.Help = help
	kingpin.Version(stamp.Summary())
	kingpin.Parse()

	provider, err := file.New("secrets.yml")
	if err != nil {
		log.Fatal(err)
	}

	c := &collector.Collector{
		Target:   "",
		Provider: provider,
		Timeout:  time.Second * 8, // there seems to be a 2s delay before it stops
	}
	reg := prometheus.NewPedanticRegistry() // TODO change to NewRegistry once all confirmed working
	if err := reg.Register(c); err != nil {
		// would return 5xx error - main thing is not to panic
		log.Fatal(err)
	}
	bmcHandler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})

	http.Handle("/bmc", bmcHandler)
	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(":8080", nil))
}
