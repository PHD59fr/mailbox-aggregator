package operations

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// Registry tracks cancellation functions for active upstream operations. It
// intentionally stores no credentials or message data.
type Registry struct {
	mu     sync.Mutex
	byUser map[uuid.UUID]map[uint64]context.CancelFunc
	next   uint64
}

func NewRegistry() *Registry {
	return &Registry{byUser: make(map[uuid.UUID]map[uint64]context.CancelFunc)}
}
func (r *Registry) Track(userID uuid.UUID, cancel context.CancelFunc) func() {
	r.mu.Lock()
	r.next++
	id := r.next
	if r.byUser[userID] == nil {
		r.byUser[userID] = make(map[uint64]context.CancelFunc)
	}
	r.byUser[userID][id] = cancel
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		if group := r.byUser[userID]; group != nil {
			delete(group, id)
			if len(group) == 0 {
				delete(r.byUser, userID)
			}
		}
		r.mu.Unlock()
	}
}
func (r *Registry) CancelUser(userID uuid.UUID) {
	r.mu.Lock()
	group := r.byUser[userID]
	delete(r.byUser, userID)
	r.mu.Unlock()
	for _, cancel := range group {
		cancel()
	}
}
func (r *Registry) Close() {
	r.mu.Lock()
	groups := r.byUser
	r.byUser = make(map[uuid.UUID]map[uint64]context.CancelFunc)
	r.mu.Unlock()
	for _, group := range groups {
		for _, cancel := range group {
			cancel()
		}
	}
}
