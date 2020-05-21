package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/gebn/bmc_exporter/bmc/collector"
	"github.com/gebn/bmc_exporter/bmc/target"
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
		Name:      "build_time_seconds",
		Help:      "When the running exporter was built, as seconds since the Unix Epoch.",
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

	help = "An IPMI v2.0 Prometheus exporter."

	listenAddr = kingpin.Flag("web.listen-address", "Address on which to "+
		"expose metrics.").
		Default(":9622").
		String()
	scrapeTimeout = kingpin.Flag("scrape.timeout", "Maximum time allowed for "+
		"each request to /bmc, including time spent queuing. The aim is to "+
		"return what we have rather than Prometheus give up and throw "+
		"everything away, so this value should be slightly shorter than the "+
		"scrape_timeout.").
		Default("9s"). // network RTT
		Duration()
	collectTimeout = kingpin.Flag("collect.timeout", "Maximum time allowed "+
		"for a single scrape to query the BMC once it has reached the front "+
		"of the queue. After this, the exporter will return what is has. "+
		"This parameter is most useful to ensure fairness when the exporter "+
		"is being scraped by multiple Prometheis.").
		Default("9s"). // network RTT
		Duration()
	secretsStatic = kingpin.Flag("secrets.static", "Used by the static "+
		"session provider to look up BMC credentials.").
		Default("secrets.yml").
		String() // we don't use ExistingFile() due to kingpin issue #261
)

func init() {
	for _, path := range []string{"/", "/bmc", "/metrics"} {
		requestDuration.WithLabelValues(path)
	}
	buildInfo.WithLabelValues(stamp.Version, stamp.Commit).Set(1)
	buildTime.Set(float64(stamp.Time().UnixNano()) / float64(time.Second))
	runtime.SetMutexProfileFraction(5)
}

func main() {
	kingpin.CommandLine.Help = help
	kingpin.Version(stamp.Summary())
	kingpin.Parse()

	provider, err := file.New(*secretsStatic)
	if err != nil {
		log.Fatal(err)
	}

	mapper := target.NewMapper(target.ProviderFunc(func(addr string) *target.Target {
		return target.New(&collector.Collector{
			Target:   addr,
			Provider: provider,
			Timeout:  *collectTimeout,
		})
	}))
	// must not return early from now on

	registerHandler("/", root.Handler())
	registerHandler("/bmc", bmc.Handler(mapper, *scrapeTimeout))
	registerHandler("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr: *listenAddr,

		// solves is waiting indefinitely before we get to a handler; handlers
		// are capable of timing out themselves. This isn't intended to ensure
		// we have time to do something useful with the request - it is only to
		// avoid a possible goroutine leak (#39).
		ReadHeaderTimeout: *scrapeTimeout,

		// this is above the max recommended scrape interval
		// (https://stackoverflow.com/a/40233721)
		IdleTimeout: time.Minute * 3,
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
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	fmt.Println() // avoids "^C" being printed on the same line as the log date
	log.Println("waiting for in-progress requests to finish...")

	if err := srv.Shutdown(context.Background()); err != nil {
		// either a context or listener error, and it cannot be the former as
		// we're using the background ctx
		log.Printf("failed to close listener: %v", err)
	}
	wg.Wait()
	mapper.Close()
}

// registerHandler adds an instrumented version of the provided handler to the
// default mux at the indicated path.
func registerHandler(path string, handler http.Handler) {
	http.Handle(path, promhttp.InstrumentHandlerDuration(
		requestDuration.MustCurryWith(prometheus.Labels{
			"path": path,
		}),
		handler,
	))
}
