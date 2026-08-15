package main

import (
	"log"
	"net/http"
	"os"
)

// healthHandler responds to GET /health with a JSON status payload.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// newMux builds and returns the fully configured routing handler for the
// service. Tests can obtain the routed handler from this function without
// binding a network port.
func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler)
	return mux
}

// port returns the configured listen port: the PORT environment variable if
// set and non-empty, otherwise the default 8080.
func port() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return "8080"
}

func main() {
	p := port()
	log.Printf("server starting on port %s", p)
	if err := http.ListenAndServe(":"+p, newMux()); err != nil {
		log.Fatal(err)
	}
}
