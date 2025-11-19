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

// selectBackendForUpload chooses the backend with the most available storage space.
func (b *Balancer) selectBackendForUpload(ctx context.Context, fileSize int64) (string, error) {
	type backendInfo struct {
		url       string
		available uint64
		err       error
	}

	results := make(chan backendInfo, len(b.backends))
	var waitGroup sync.WaitGroup

	// Query all backends in parallel
	for _, backend := range b.backends {
		waitGroup.Add(1)
		go func(url string) {
			defer waitGroup.Done()
			info, err := b.getNodeInfo(ctx, url)
			if err != nil {
				results <- backendInfo{url: url, err: err}
				return
			}
			results <- backendInfo{url: url, available: info.Storage.Available}
		}(backend)
	}

	waitGroup.Wait()
	close(results)

	var (
		bestBackend  string
		maxAvailable uint64
		lastError    error
	)

	for result := range results {
		if result.err != nil {
			log.Warn().Err(result.err).Str("backend", result.url).Msg("Failed to get node info")
			lastError = result.err
			continue
		}

		// Check if file will fit (ensure fileSize is not negative)
		if fileSize < 0 || result.available < uint64(fileSize) {
			log.Warn().Str("backend", result.url).
				Int64("file_size", fileSize).
				Uint64("available", result.available).
				Msg("Backend does not have enough space")
			continue
		}

		if result.available > maxAvailable {
			maxAvailable = result.available
			bestBackend = result.url
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
