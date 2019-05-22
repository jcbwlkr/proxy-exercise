package main

import (
	"flag"
	"log"
	"net/http"
	"time"
)

func main() {
	addr := flag.String("addr", ":8080", "ip:port for the service to bind to")
	backend := flag.String("backend", "http://localhost:7080", "url for the upstream code host API")

	flag.Parse()

	srv := http.Server{
		Addr:    *addr,
		Handler: app(*backend),
	}

	log.Fatal(srv.ListenAndServe())
}

func app(backend string) http.Handler {

	mux := http.NewServeMux()

	rh := RepositoryHandlers{
		CodeHostURL: backend,
	}

	mux.HandleFunc("/repositories", rh.List)

	return mux
}

type Repository struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	FetchedAt time.Time `json:"fetched_at"`
}

type RepositoryHandlers struct {
	CodeHostURL string
}

func (rh *RepositoryHandlers) List(w http.ResponseWriter, r *http.Request) {
}
