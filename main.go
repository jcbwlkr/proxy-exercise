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

	"github.com/pkg/errors"
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

// Repository is the main data record we are proxying from the upstream API.
type Repository struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	FetchedAt time.Time `json:"fetchedAt"`
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

	// sem is used to prevent the proxy from starting more than cap(sem)
	// concurrent requests upstream.
	// TODO: decide on the appropriate size for this.
	sem := make(chan struct{}, 10)

	var gid int
	for gid = 0; gid < count; gid++ {
		go rh.query(ctx, gid, sem, ch)
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
				go rh.query(ctx, gid, sem, ch)
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

func (rh *RepositoryHandlers) query(ctx context.Context, id int, sem chan struct{}, results chan<- Repository) {

	// Driver loop that retries until the context is canceled and respects the semaphore.
	for {

		// If the context is done then give up.
		select {
		case <-ctx.Done():
			return

		// If we can push a value onto the semaphore then we can start calling.
		case sem <- struct{}{}:
			rh.Log.Printf("%d : started", id)
			repo, err := rh.queryCall(ctx)

			// Take a value out of the semaphore to let another goroutine in.
			<-sem

			if err != nil {
				rh.Log.Printf("%d : ERROR %v", id, err)
				continue
			}

			// Success!
			results <- repo
			rh.Log.Printf("%d : completed", id)
			return
		}
	}
}

func (rh *RepositoryHandlers) queryCall(ctx context.Context) (Repository, error) {
	api := rh.CodeHostURL + "/repository"
	req, err := http.NewRequest(http.MethodGet, api, nil)
	if err != nil {
		return Repository{}, errors.Wrap(err, "constructing url")
	}

	req = req.WithContext(ctx)

	res, err := rh.Client.Do(req)
	if err != nil {
		return Repository{}, errors.Wrap(err, "calling API")
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return Repository{}, fmt.Errorf("api responded %d", res.StatusCode)
	}

	var response struct {
		Repository Repository `json:"repository"`
	}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return Repository{}, errors.Wrap(err, "decoding response")
	}

	return response.Repository, nil
}
