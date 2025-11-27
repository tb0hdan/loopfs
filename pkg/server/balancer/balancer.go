package balancer

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

// Balancer manages multiple CAS server backends.
type Balancer struct {
	backendManager *BackendManager
	client         *retryablehttp.Client
	requestTimeout time.Duration
}

// NewBalancer creates a new load balancer instance.
func NewBalancer(backendManager *BackendManager, retryMax int, retryWaitMin, retryWaitMax, requestTimeout time.Duration) *Balancer {
	client := CreateRetryableClient(retryMax, retryWaitMin, retryWaitMax)

	return &Balancer{
		backendManager: backendManager,
		client:         client,
		requestTimeout: requestTimeout,
	}
}

// BackendManager returns the backend manager for this balancer.
func (b *Balancer) BackendManager() *BackendManager {
	return b.backendManager
}

// RequestResult is a generic result from a backend request.
type RequestResult[T any] struct {
	Backend   string
	Data      T
	Status    int
	Error     error
	CtxCancel context.CancelFunc // Optional cancel function for the request context
}

// BackendRequestFunc defines a function that makes a request to a single backend.
type BackendRequestFunc[T any] func(ctx context.Context, backend string) (T, int, error)

// executeBackendRequests executes requests across specified backends in parallel using waitgroups.
// It returns a channel that will be closed when all requests complete.
// If cancelOnSuccess is true, other requests will be cancelled when the first successful response is received.
//
//nolint:govet,cyclop // cancel is intentionally not called on all paths to avoid canceling streaming downloads
func executeBackendRequests[T any](
	ctx context.Context,
	backends []string,
	requestTimeout time.Duration,
	requestFunc BackendRequestFunc[T],
	cancelOnSuccess bool,
) <-chan RequestResult[T] {
	results := make(chan RequestResult[T], len(backends))

	if len(backends) == 0 {
		close(results)
		return results
	}

	var waitGroup sync.WaitGroup
	cancelCtx, cancel := context.WithCancel(ctx)

	for _, backend := range backends {
		waitGroup.Add(1)
		go func(url string) {
			defer waitGroup.Done()

			reqCtx, reqCancel := context.WithTimeout(cancelCtx, requestTimeout)

			data, status, err := requestFunc(reqCtx, url)
			result := RequestResult[T]{
				Backend:   url,
				Data:      data,
				Status:    status,
				Error:     err,
				CtxCancel: reqCancel,
			}

			// Only cancel immediately if there was an error or non-OK status
			// For successful responses, pass the cancel function to the handler
			if err != nil || status != http.StatusOK {
				reqCancel()
				result.CtxCancel = nil
			}

			select {
			case results <- result:
				// If we got a success and should cancel on success, do so
				if cancelOnSuccess && err == nil && status == http.StatusOK {
					cancel()
				}
			case <-cancelCtx.Done():
				// If context was canceled, clean up
				if result.CtxCancel != nil {
					result.CtxCancel()
				}
			}
		}(backend)
	}

	// Close channel when all goroutines complete
	go func() {
		waitGroup.Wait()
		close(results)
		// Note: We intentionally don't call cancel() here because handlers may still
		// be using the response (e.g., streaming download response body). The context
		// will be cleaned up when the parent HTTP request context is done.
		// Individual handlers are responsible for calling their reqCancel functions.
	}()

	return results
}
