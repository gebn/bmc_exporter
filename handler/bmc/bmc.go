package bmc

import (
	"context"
	"net/http"
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
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		m.Handler(target).ServeHTTP(w, r.WithContext(ctx))
		cancel()
	})
}
