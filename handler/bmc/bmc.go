package bmc

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gebn/bmc_exporter/bmc/target"
)

func Handler(m *target.Mapper, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("target")
		if target == "" {
			http.Error(w, "'target' parameter must be specified",
				http.StatusBadRequest)
			return
		}

		timeout = lowestTimeout(r, timeout)
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		m.Handler(target).ServeHTTP(w, r.WithContext(ctx))
		cancel()
	})
}

// lowestTimeout returns the lower of the exporter's configured timeout, and
// the timeout Prometheus indicated in its request.
func lowestTimeout(r *http.Request, exporter time.Duration) time.Duration {
	header := r.Header.Get("X-Prometheus-Scrape-Timeout-Seconds")
	if header == "" {
		// not Prometheus knocking
		return exporter
	}
	seconds, err := strconv.ParseFloat(header, 64) // e.g. "10.000000"
	if err != nil {
		return exporter
	}
	prometheus := time.Duration(seconds * 1_000_000_000)
	if prometheus > exporter {
		// we have a stricter constraint; ignore Prometheus's timeout
		return exporter
	}
	// Prometheus will timeout before we do, better hurry up
	return prometheus
}
