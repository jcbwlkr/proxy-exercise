package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

func main() {
	addr := flag.String("addr", ":8080", "ip:port for the service to bind to")
	backend := flag.String("backend", "http://localhost:7080", "url for the upstream code host API")

	flag.Parse()

	logger := log.New(os.Stdout, "proxy : ", log.LstdFlags|log.Lshortfile)

	srv := http.Server{
		Addr:    *addr,
		Handler: app(*backend, logger),
	}

	log.Fatal(srv.ListenAndServe())
}

func app(backend string, logger *log.Logger) http.Handler {

	mux := http.NewServeMux()

	rh := RepositoryHandlers{
		CodeHostURL: backend,
		Client:      &http.Client{},
		Log:         logger,
	}

	mux.HandleFunc("/repositories", rh.List)

	return mux
}

type Repository struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	FetchedAt time.Time `json:"fetched_at"`
}

// RepositoryHandlers holds handlers related to repositories and their dependencies.
type RepositoryHandlers struct {
	CodeHostURL string
	Client      *http.Client
	Log         *log.Logger
}

// List calls an upstream server to get a list of repositories.
func (rh *RepositoryHandlers) List(w http.ResponseWriter, r *http.Request) {

	// Parse query parameters.
	count, err := strconv.Atoi(r.URL.Query().Get("count"))
	if err != nil {
		count = 1
	}

	unique := false
	if r.URL.Query().Get("unique") == "true" {
		unique = true
	}
	_ = unique // TODO remove

	// Start making requests.

	api := rh.CodeHostURL + "/repository"

	// Make the slice which will hold our results. Using a slice with a length of
	// 0 instead of a nil slice so that 0 results encodes as json [] not null
	// which is harder for clients to digest.
	//
	// The slice starts with a capacity equal to the count so append() does not
	// need to keep allocating new backing arrays.
	repos := make([]Repository, 0, count)

	// seen keeps a map of repository IDs in the results.
	// TODO: this could be a map[int]struct{} to save on memory but it makes code below uglier. Discuss this.
	seen := make(map[int]bool)

	ctx := r.Context()

	ch := make(chan Repository, count)

	var gid int
	for gid = 0; gid < count; gid++ {
		go rh.query(ctx, api, gid, ch)
	}

loop:
	for {
		select {
		case <-ctx.Done():
			break loop

		case repo := <-ch:

			// If we only want unique results and we've seen this one before then
			// schedule another goroutine to account for this duplicate.
			if unique && seen[repo.ID] {
				go rh.query(ctx, api, gid, ch)
				gid++
				break
			}

			seen[repo.ID] = true

			repos = append(repos, repo)

			if len(repos) == count {
				break loop
			}
		}
	}

	// Package up the response and send it.
	var result struct {
		Repositories []Repository `json:"repositories"`
	}

	result.Repositories = repos

	data, err := json.Marshal(result)
	if err != nil {
		rh.Log.Println("could not marshal results", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.Write(data)
}

// TODO add semaphore

func (rh *RepositoryHandlers) query(ctx context.Context, api string, id int, results chan<- Repository) {

	req, err := http.NewRequest(http.MethodGet, api, nil)
	if err != nil {
		rh.Log.Println("could not construct url", err)
		return
	}

	req = req.WithContext(ctx)

	res, err := rh.Client.Do(req)
	if err != nil {
		rh.Log.Println("could not call API", err)
		return
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		err := fmt.Errorf("api responded %d", res.StatusCode)
		rh.Log.Println("could not call API", err)
		return
	}

	var response struct {
		Repository Repository `json:"repository"`
	}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		rh.Log.Println("could not decode API response", err)
		return
	}

	results <- response.Repository
}
