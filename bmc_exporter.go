package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gebn/bmc_exporter/collector"
	"github.com/gebn/bmc_exporter/handler/root"
	"github.com/gebn/bmc_exporter/session/file"

	"github.com/alecthomas/kingpin"
	"github.com/gebn/go-stamp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	namespace = "bmc"

	buildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "exporter",
			Name:      "build_info",
			Help:      "The version and commit of the running exporter. Constant 1.",
		},
		// the runtime version is already exposed by the default Go collector
		[]string{"version", "commit"},
	)
	buildTime = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "exporter",
		Name:      "build_time",
		Help:      "When the running exporter was build, as seconds since the Unix Epoch.",
	})

	help = "An IPMI v1.5/2.0 Prometheus exporter."

	listenAddr = kingpin.Flag("web.listen-address", "Address on which to "+
		"expose metrics.").
		Default(":9622").
		String()
	scrapeTimeout = kingpin.Flag("scrape.timeout", "BMC scrapes will return "+
		"early after this long. This value should be slightly shorter than "+
		"the Prometheus scrape_timeout.").
		Default("8s"). // ~1.5s delay + network RTT
		Duration()
	staticSecrets = kingpin.Flag("session.static.secrets", "Used by the "+
		"static session provider to look up BMC credentials.").
		Default("secrets.yml").
		String() // we don't use ExistingFile() due to kingpin issue #261
)

func main() {
	buildInfo.WithLabelValues(stamp.Version, stamp.Commit).Set(1)
	buildTime.Set(float64(stamp.Time().UnixNano()) / float64(time.Second))

	kingpin.CommandLine.Help = help
	kingpin.Version(stamp.Summary())
	kingpin.Parse()

	provider, err := file.New(*staticSecrets)
	if err != nil {
		log.Fatal(err)
	}

	c := &collector.Collector{
		Target:   "",
		Provider: provider,
		Timeout:  *scrapeTimeout,
	}
	reg := prometheus.NewPedanticRegistry() // TODO change to NewRegistry once all confirmed working
	if err := reg.Register(c); err != nil {
		// would return 5xx error - main thing is not to panic
		log.Fatal(err)
	}
	bmcHandler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})

	http.Handle("/", root.Handler())
	http.Handle("/bmc", bmcHandler)
	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(*listenAddr, nil))
}
