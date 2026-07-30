package session

import (
	"context"
	stderrors "errors"
	"sync"
)

const (
	maxActiveSearchRequests = 16
	maxQueuedSearchRequests = 32
)

var (
	errSearchQueueFull     = stderrors.New("knowledge search queue is full")
	sessionSearchAdmission = newSearchAdmission(maxActiveSearchRequests, maxQueuedSearchRequests)
)

// searchAdmission bounds synchronous retrieval before it can fan into the
// search pipeline. Active requests consume a slot; excess requests wait in a
// cancellation-aware queue with a separately bounded capacity.
type searchAdmission struct {
	active chan struct{}
	total  chan struct{}
}

func newSearchAdmission(maxActive, maxQueued int) *searchAdmission {
	if maxActive <= 0 || maxQueued < 0 {
		panic("search admission capacity must be positive")
	}
	return &searchAdmission{
		active: make(chan struct{}, maxActive),
		total:  make(chan struct{}, maxActive+maxQueued),
	}
}

func (a *searchAdmission) acquire(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Total capacity remains owned while a request moves from waiting to active,
	// so promotion cannot transiently double-count one request and reject early.
	select {
	case a.total <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, errSearchQueueFull
	}

	select {
	case a.active <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-a.active
			<-a.total
			return nil, err
		}
		return a.releaseFunc(), nil
	case <-ctx.Done():
		<-a.total
		return nil, ctx.Err()
	}
}

func (a *searchAdmission) releaseFunc() func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			<-a.total
			<-a.active
		})
	}
}
