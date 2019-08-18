package bmc

import (
	"net/http"
)

func Handler(m *Mapper) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("target")
		if target == "" {
			http.Error(w, "'target' parameter must be specified",
				http.StatusBadRequest)
			return
		}
		m.Handler(target).ServeHTTP(w, r)
	})
}
