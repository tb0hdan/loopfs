package balancer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"loopfs/pkg/log"
	"loopfs/pkg/models"

	"github.com/hashicorp/go-retryablehttp"
)

// Balancer manages multiple CAS server backends.
type Balancer struct {
	backends       []string
	client         *retryablehttp.Client
	retryMax       int
	retryWaitMin   time.Duration
	retryWaitMax   time.Duration
	requestTimeout time.Duration
	cacheTTL       time.Duration
	nodeInfoCache  map[string]cachedNodeInfo
	cacheMu        sync.RWMutex
}

type cachedNodeInfo struct {
	info    models.NodeInfo
	expires time.Time
}

const defaultNodeInfoCacheTTL = 5 * time.Second

// NewBalancer creates a new load balancer instance.
func NewBalancer(backends []string, retryMax int, retryWaitMin, retryWaitMax, requestTimeout time.Duration) *Balancer {
	client := retryablehttp.NewClient()
	client.RetryMax = retryMax
	client.RetryWaitMin = retryWaitMin
	client.RetryWaitMax = retryWaitMax
	client.HTTPClient.Timeout = requestTimeout
	client.Logger = nil // Disable retryablehttp logging

	return &Balancer{
		backends:       backends,
		client:         client,
		retryMax:       retryMax,
		retryWaitMin:   retryWaitMin,
		retryWaitMax:   retryWaitMax,
		requestTimeout: requestTimeout,
		cacheTTL:       defaultNodeInfoCacheTTL,
		nodeInfoCache:  make(map[string]cachedNodeInfo),
	}
}

// getNodeInfo fetches node information from a specific backend.
func (b *Balancer) getNodeInfo(ctx context.Context, backend string) (*models.NodeInfo, error) {
	if info, ok := b.getCachedNodeInfo(backend); ok {
		return info, nil
	}

	reqCtx, cancel := context.WithTimeout(ctx, b.requestTimeout)
	defer cancel()

	req, err := retryablehttp.NewRequestWithContext(reqCtx, http.MethodGet, backend+"/node/info", nil)
	if err != nil {
		return nil, err
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("node info request failed with status %d", resp.StatusCode)
	}

	var nodeInfo models.NodeInfo
	if err := json.NewDecoder(resp.Body).Decode(&nodeInfo); err != nil {
		return nil, err
	}

	b.setCachedNodeInfo(backend, &nodeInfo)

	return &nodeInfo, nil
}

type backendSpaceInfo struct {
	available uint64
}

// selectBackendForUpload chooses the backend with the most available storage space.
func (b *Balancer) selectBackendForUpload(ctx context.Context, fileSize int64) (string, error) {
	// Execute node info request across all backends
	results := executeBackendRequests(ctx, b.backends, b.requestTimeout,
		func(reqCtx context.Context, backend string) (backendSpaceInfo, int, error) {
			info, err := b.getNodeInfo(reqCtx, backend)
			if err != nil {
				return backendSpaceInfo{}, 0, err
			}
			return backendSpaceInfo{available: info.Storage.Available}, http.StatusOK, nil
		},
		false, // Don't cancel on success - we need all backends to find the best one
	)

	var (
		bestBackend  string
		maxAvailable uint64
		lastError    error
	)

	for result := range results {
		if result.Error != nil {
			log.Warn().Err(result.Error).Str("backend", result.Backend).Msg("Failed to get node info")
			lastError = result.Error
			continue
		}

		// Check if file will fit (ensure fileSize is not negative)
		if fileSize < 0 || result.Data.available < uint64(fileSize) {
			log.Warn().Str("backend", result.Backend).
				Int64("file_size", fileSize).
				Uint64("available", result.Data.available).
				Msg("Backend does not have enough space")
			continue
		}

		if result.Data.available > maxAvailable {
			maxAvailable = result.Data.available
			bestBackend = result.Backend
		}
	}

	if bestBackend == "" {
		if lastError != nil {
			return "", fmt.Errorf("no suitable backend found: %w", lastError)
		}
		return "", fmt.Errorf("no backend has enough space for file of size %d", fileSize)
	}

	return bestBackend, nil
}

// getCachedNodeInfo retrieves cached node information if it is still valid.
func (b *Balancer) getCachedNodeInfo(backend string) (*models.NodeInfo, bool) {
	b.cacheMu.RLock()
	defer b.cacheMu.RUnlock()

	entry, ok := b.nodeInfoCache[backend]
	if !ok || time.Now().After(entry.expires) {
		return nil, false
	}

	info := entry.info
	return &info, true
}

// setCachedNodeInfo stores node information in the cache with an expiration time.
func (b *Balancer) setCachedNodeInfo(backend string, info *models.NodeInfo) {
	b.cacheMu.Lock()
	defer b.cacheMu.Unlock()

	b.nodeInfoCache[backend] = cachedNodeInfo{
		info:    *info,
		expires: time.Now().Add(b.cacheTTL),
	}
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

// executeBackendRequests executes requests across all backends in parallel using waitgroups.
// It returns a channel that will be closed when all requests complete.
// If cancelOnSuccess is true, other requests will be cancelled when the first successful response is received.
//
//nolint:govet // cancel is intentionally not called on all paths to avoid canceling streaming downloads
func executeBackendRequests[T any](
	ctx context.Context,
	backends []string,
	requestTimeout time.Duration,
	requestFunc BackendRequestFunc[T],
	cancelOnSuccess bool,
) <-chan RequestResult[T] {
	results := make(chan RequestResult[T], len(backends))
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
