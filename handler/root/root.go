// Package root implements the handler for when / is requested. This endpoint
// shows the exporter name, version, and a simple form to scrape a target.
package root

import (
	"fmt"
	"html/template"
	"net/http"

	"github.com/gebn/go-stamp/v2"
)

const (
	html = `<!DOCTYPE html>
<html lang="en">
    <head>
        <meta charset="utf-8"/>
        <title>{{ .Name }}</title>
    </head>
    <body>
        <h1>{{ .Name }}</h1>
        <form action="/bmc">
            <label>Target:</label>
            <input type="text" name="target" placeholder="IP[:port=623]" required="required"/>
            <input type="submit" value="Scrape"/>
        </form>
        <pre>{{ .Version }}</pre>
    </body>
</html>
`
)

var (
	parsed = template.Must(template.New("root").Parse(html))
	data   = interpolations{
		Name:    "BMC Exporter",
		Version: stamp.Summary(),
	}
)

// interpolations defines the shape of data accepted by the root page template.
type interpolations struct {

	// Name is the name of the exporter, appearing in the page title and
	// top-level heading.
	Name string

	// Version contains information about the running binary.
	Version string
}

// Handler returns a handler that will render the homepage when invoked.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := parsed.Execute(w, data); err != nil {
			http.Error(w, fmt.Sprintf("failed to execute template: %v", err),
				http.StatusInternalServerError)
		}
	})
}
