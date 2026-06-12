// Command embed is a runnable example of using Gresbase as a Go framework:
// register real Go hooks and custom routes, then serve — all from your own
// binary. Run it with:
//
//	cd backend && go run ./examples/embed serve
//
// It boots with embedded PostgreSQL (no DATABASE_URL needed) in dev mode.
package main

import (
	"log"
	"net/http"

	"github.com/gresbase/gresbase"
	"github.com/gresbase/gresbase/internal/events"
)

func main() {
	gb := gresbase.New()
	gb.SetDevMode(true)

	// A real Go hook — fires on every record creation, bound before Start().
	gb.OnRecordCreate().BindFunc(func(e events.Event) error {
		if re, ok := e.(*events.RecordEvent); ok {
			log.Printf("[hook] record created in %q: id=%s", re.CollectionName, re.RecordID)
		}
		return e.Next()
	})

	// A custom route served alongside the built-in REST API and dashboard.
	gb.OnServe().BindFunc(func(e events.Event) error {
		if se, ok := e.(*events.ServeEvent); ok {
			se.Router.Get("/api/v1/custom/ping", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"pong":true}`))
			})
		}
		return e.Next()
	})

	if err := gb.Start(); err != nil {
		log.Fatal(err)
	}
}
