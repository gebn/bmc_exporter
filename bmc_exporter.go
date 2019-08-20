package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/gebn/bmc_exporter/handler/bmc"
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
	subsystem = "exporter"

	buildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "build_info",
			Help:      "The version and commit of the running exporter. Constant 1.",
		},
		// the runtime version is already exposed by the default Go collector
		[]string{"version", "commit"},
	)
	buildTime = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      "build_time",
		Help:      "When the running exporter was build, as seconds since the Unix Epoch.",
	})
	requestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "request_duration_seconds",
			Help: "The time taken to execute the handlers of web server " +
				"endpoints.",
		},
		[]string{"path"},
	)

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

func init() {
	for _, path := range []string{"/", "/bmc", "/metrics"} {
		requestDuration.WithLabelValues(path)
	}
}

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
	mapper := bmc.NewMapper(provider, *scrapeTimeout)
	defer mapper.Close()
	// we must not exit with os.Exit (e.g. log.Fatal) from now on, otherwise the
	// mapper, and hence BMC connections, will not be closed

	http.Handle("/", promhttp.InstrumentHandlerDuration(
		requestDuration.MustCurryWith(prometheus.Labels{
			"path": "/",
		}),
		root.Handler(),
	))
	http.Handle("/bmc", promhttp.InstrumentHandlerDuration(
		requestDuration.MustCurryWith(prometheus.Labels{
			"path": "/bmc",
		}),
		bmc.Handler(mapper),
	))
	http.Handle("/metrics", promhttp.InstrumentHandlerDuration(
		requestDuration.MustCurryWith(prometheus.Labels{
			"path": "/metrics",
		}),
		promhttp.Handler(),
	))

	srv := &http.Server{
		Addr: *listenAddr,
	}
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			// without the wait group, this line may not be printed in case of
			// failure
			log.Printf("server did not close cleanly: %v", err)
		}
	}()

	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt)
	<-quit
	fmt.Println() // avoids "^C" being printed on the same line as the log date
	log.Println("waiting for in-progress requests to finish...")
	if err := srv.Shutdown(context.Background()); err != nil {
		// either a context or listener error, and it cannot be the former as
		// we're using the background ctx
		log.Printf("failed to close listener: %v", err)
	}
	wg.Wait()
}
