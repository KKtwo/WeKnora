package session

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSearchAdmissionCapacityContract(t *testing.T) {
	require.Equal(t, 16, maxActiveSearchRequests)
	require.Equal(t, 32, maxQueuedSearchRequests)

	admission := newSearchAdmission(maxActiveSearchRequests, maxQueuedSearchRequests)
	releases := make([]func(), 0, maxActiveSearchRequests)
	for range maxActiveSearchRequests {
		release, err := admission.acquire(context.Background())
		require.NoError(t, err)
		releases = append(releases, release)
	}

	type acquireResult struct {
		release func()
		err     error
	}
	results := make(chan acquireResult, maxQueuedSearchRequests)
	for range maxQueuedSearchRequests {
		go func() {
			release, err := admission.acquire(context.Background())
			results <- acquireResult{release: release, err: err}
		}()
	}
	waitForAdmissionCount(t, admission, maxQueuedSearchRequests)

	release, err := admission.acquire(context.Background())
	require.Nil(t, release)
	require.ErrorIs(t, err, errSearchQueueFull)

	releases[0]()
	queued := <-results
	require.NoError(t, queued.err)
	queued.release()
	for _, release := range releases[1:] {
		release()
	}
	for range maxQueuedSearchRequests - 1 {
		queued = <-results
		require.NoError(t, queued.err)
		queued.release()
	}
}

func TestSearchAdmissionCancellationReleasesQueueSlot(t *testing.T) {
	admission := newSearchAdmission(1, 1)
	releaseActive, err := admission.acquire(context.Background())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := admission.acquire(ctx)
		result <- err
	}()
	waitForAdmissionCount(t, admission, 1)
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	waitForAdmissionCount(t, admission, 0)

	acquired := make(chan func(), 1)
	go func() {
		release, acquireErr := admission.acquire(context.Background())
		require.NoError(t, acquireErr)
		acquired <- release
	}()
	waitForAdmissionCount(t, admission, 1)
	releaseActive()
	(<-acquired)()
}

func TestSearchAdmissionRejectsInvalidCapacity(t *testing.T) {
	require.Panics(t, func() { newSearchAdmission(0, 1) })
	require.Panics(t, func() { newSearchAdmission(1, -1) })
}

func waitForAdmissionCount(t *testing.T, admission *searchAdmission, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for len(admission.total)-len(admission.active) != want {
		if time.Now().After(deadline) {
			t.Fatalf(
				"timed out waiting for admission count %d; got %d",
				want,
				len(admission.total)-len(admission.active),
			)
		}
		runtime.Gosched()
	}
}
