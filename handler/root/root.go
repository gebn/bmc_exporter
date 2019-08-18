// Package root implements the handler for when / is requested. This endpoint
// shows the exporter name, version, and a simple form to scrape an addr.
package root

import (
	"fmt"
	"html/template"
	"net/http"

	"github.com/gebn/go-stamp"
)

const (
	text = `<!DOCTYPE html>
    <html lang="en">
        <head>
            <meta charset="utf-8"/>
            <title>{{ .Name }}</title>
        </head>
        <body>
            <h1>{{ .Name }}</h1>
            <form action="/bmc">
                <label>Target:</label>
                <input type="text" name="target" placeholder="IP[:port=623]"/>
                <input type="submit" value="Scrape"/>
            </form>
            <pre>{{ .Version }}</pre>
        </body>
    </html>`
)

var (
	parsed = template.Must(template.New("root").Parse(text))
	data   = interpolations{
		Name:    "BMC Exporter",
		Version: stamp.Summary(),
	}
)

type interpolations struct {
	Name    string
	Version string
}

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := parsed.Execute(w, data); err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(fmt.Sprintf("failed to execute template: %v\n", err)))
		}
	})
}
