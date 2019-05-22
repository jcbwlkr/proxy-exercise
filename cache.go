package main

import (
	"errors"
	"math/rand"
	"sync"
)

// Cache remembers seen Repostories and provides goroutine-safe methods for
// adding values or retrieving random results.
type Cache struct {
	data map[int]Repository
	ids  []int
	lock *sync.RWMutex
}

// NewCache creates a cache ready for use.
func NewCache() *Cache {
	return &Cache{
		data: make(map[int]Repository),
		ids:  nil,
		lock: &sync.RWMutex{},
	}
}

// Add puts another repository in the cache. It does not replace duplicate records.
func (c *Cache) Add(r Repository) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if _, ok := c.data[r.ID]; ok {
		return
	}

	c.data[r.ID] = r
	c.ids = append(c.ids, r.ID)
}

// GetRandom attempts to pick a random Repository from its data. If a non-nil
// map of values to exclude is provided then those records will not be
// considered. It returns an error if it cannot find a record to return.
func (c *Cache) GetRandom(exclude map[int]bool) (Repository, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	var available []int

	if exclude == nil {
		available = c.ids
	} else {
		for _, id := range c.ids {
			if !exclude[id] {
				available = append(available, id)
			}
		}
	}

	if len(available) == 0 {
		return Repository{}, errors.New("no repositories available")
	}

	index := rand.Intn(len(available))
	id := available[index]
	r := c.data[id]

	return r, nil
}
