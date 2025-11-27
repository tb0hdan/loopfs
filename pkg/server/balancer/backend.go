package balancer

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"

	"loopfs/pkg/log"
	"loopfs/pkg/models"

	"github.com/hashicorp/go-retryablehttp"
)

const (
	defaultHealthCheckInterval = 5 * time.Second
	defaultHealthCheckTimeout  = 5 * time.Second
	maxConsecutiveFailures     = 3
)

// BackendManager manages backend servers and their health status.
type BackendManager struct {
	backends            map[string]*models.BackendStatus
	mu                  sync.RWMutex
	client              *http.Client
	healthCheckInterval time.Duration
	healthCheckTimeout  time.Duration
	stopCh              chan struct{}
	wg                  sync.WaitGroup
}

// NewBackendManager creates a new backend manager with the given backend URLs.
func NewBackendManager(backendURLs []string, healthCheckInterval, healthCheckTimeout time.Duration) *BackendManager {
	if healthCheckInterval <= 0 {
		healthCheckInterval = defaultHealthCheckInterval
	}
	if healthCheckTimeout <= 0 {
		healthCheckTimeout = defaultHealthCheckTimeout
	}

	backends := make(map[string]*models.BackendStatus, len(backendURLs))
	for _, url := range backendURLs {
		backends[url] = &models.BackendStatus{
			URL:    url,
			Online: true, // Assume online until proven otherwise
		}
	}

	return &BackendManager{
		backends:            backends,
		client:              &http.Client{Timeout: healthCheckTimeout},
		healthCheckInterval: healthCheckInterval,
		healthCheckTimeout:  healthCheckTimeout,
		stopCh:              make(chan struct{}),
	}
}

// Start begins the background health check goroutines.
func (bm *BackendManager) Start() {
	// Perform initial health check synchronously
	bm.checkAllBackends()

	// Start background health checker
	bm.wg.Add(1)
	go bm.healthCheckLoop()

	log.Info().
		Int("backend_count", len(bm.backends)).
		Dur("interval", bm.healthCheckInterval).
		Msg("Backend manager started")
}

// Stop gracefully stops the backend manager.
func (bm *BackendManager) Stop() {
	close(bm.stopCh)
	bm.wg.Wait()
	log.Info().Msg("Backend manager stopped")
}

// MarkBackendDead immediately marks a backend as offline.
func (bm *BackendManager) MarkBackendDead(backendURL string, err error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	status, exists := bm.backends[backendURL]
	if !exists {
		return
	}

	if status.Online {
		log.Warn().
			Str("backend", backendURL).
			Err(err).
			Msg("Backend marked dead due to request failure")
	}

	status.Online = false
	status.ConsecFails = maxConsecutiveFailures
	status.LastError = err.Error()
	status.LastCheck = time.Now()
}

// GetOnlineBackends returns a list of online backend URLs sorted by available space (descending).
func (bm *BackendManager) GetOnlineBackends() []string {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	type backendInfo struct {
		url       string
		available uint64
		latency   int64
	}

	online := make([]backendInfo, 0, len(bm.backends))
	for url, status := range bm.backends {
		if status.Online {
			online = append(online, backendInfo{
				url:       url,
				available: status.AvailableSpace,
				latency:   status.Latency,
			})
		}
	}

	// Sort by available space descending, then by latency ascending
	sort.Slice(online, func(i, j int) bool {
		if online[i].available != online[j].available {
			return online[i].available > online[j].available
		}
		return online[i].latency < online[j].latency
	})

	urls := make([]string, len(online))
	for i, b := range online {
		urls[i] = b.url
	}

	return urls
}

// GetBackendForUpload returns the best backend for uploading a file of the given size.
// It returns the online backend with the most available space that can fit the file.
func (bm *BackendManager) GetBackendForUpload(fileSize int64) (string, error) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	var bestBackend string
	var maxAvailable uint64

	for url, status := range bm.backends {
		if !status.Online {
			continue
		}

		// Check if file will fit
		if fileSize > 0 && status.AvailableSpace < uint64(fileSize) {
			log.Debug().
				Str("backend", url).
				Int64("file_size", fileSize).
				Uint64("available", status.AvailableSpace).
				Msg("Backend does not have enough space")
			continue
		}

		if status.AvailableSpace > maxAvailable {
			maxAvailable = status.AvailableSpace
			bestBackend = url
		}
	}

	if bestBackend == "" {
		return "", ErrNoBackendAvailable
	}

	return bestBackend, nil
}

// GetAllBackendStatus returns status information for all backends.
func (bm *BackendManager) GetAllBackendStatus() []models.BackendStatus {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	statuses := make([]models.BackendStatus, 0, len(bm.backends))
	for _, status := range bm.backends {
		statuses = append(statuses, *status)
	}

	return statuses
}

// GetBackendStatus returns the status of a specific backend.
func (bm *BackendManager) GetBackendStatus(backendURL string) (*models.BackendStatus, bool) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	status, exists := bm.backends[backendURL]
	if !exists {
		return nil, false
	}

	// Return a copy to avoid race conditions
	statusCopy := *status
	return &statusCopy, true
}

// HasOnlineBackends returns true if at least one backend is online.
func (bm *BackendManager) HasOnlineBackends() bool {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	for _, status := range bm.backends {
		if status.Online {
			return true
		}
	}
	return false
}

// AllBackendURLs returns all backend URLs regardless of status.
func (bm *BackendManager) AllBackendURLs() []string {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	urls := make([]string, 0, len(bm.backends))
	for url := range bm.backends {
		urls = append(urls, url)
	}
	return urls
}

// BackendCount returns the total number of backends.
func (bm *BackendManager) BackendCount() int {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return len(bm.backends)
}

// healthCheckLoop runs periodic health checks on all backends.
func (bm *BackendManager) healthCheckLoop() {
	defer bm.wg.Done()

	ticker := time.NewTicker(bm.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-bm.stopCh:
			return
		case <-ticker.C:
			bm.checkAllBackends()
		}
	}
}

// checkAllBackends performs health checks on all backends concurrently.
func (bm *BackendManager) checkAllBackends() {
	bm.mu.RLock()
	urls := make([]string, 0, len(bm.backends))
	for url := range bm.backends {
		urls = append(urls, url)
	}
	bm.mu.RUnlock()

	var waitGroup sync.WaitGroup
	for _, url := range urls {
		waitGroup.Add(1)
		go func(backendURL string) {
			defer waitGroup.Done()
			bm.checkBackend(backendURL)
		}(url)
	}
	waitGroup.Wait()
}

// checkBackend performs a health check on a single backend.
func (bm *BackendManager) checkBackend(backendURL string) {
	ctx, cancel := context.WithTimeout(context.Background(), bm.healthCheckTimeout)
	defer cancel()

	start := time.Now()
	nodeInfo, err := bm.fetchNodeInfo(ctx, backendURL)
	latency := time.Since(start)

	bm.mu.Lock()
	defer bm.mu.Unlock()

	status, exists := bm.backends[backendURL]
	if !exists {
		return
	}

	status.LastCheck = time.Now()
	status.Latency = latency.Milliseconds()

	if err != nil {
		status.LastError = err.Error()

		// Only increment failure count for timeout/connection errors, not HTTP errors
		// HTTP errors (404, 500, etc.) mean the backend is reachable but returned an error
		if isTimeoutOrConnectionError(err) {
			status.ConsecFails++
			if status.ConsecFails >= maxConsecutiveFailures {
				if status.Online {
					log.Warn().
						Str("backend", backendURL).
						Int("consecutive_failures", status.ConsecFails).
						Err(err).
						Msg("Backend marked offline")
				}
				status.Online = false
			}
		}
		return
	}

	// Success - reset failure count and update info
	wasOffline := !status.Online
	status.Online = true
	status.ConsecFails = 0
	status.LastError = ""
	status.NodeInfo = nodeInfo
	status.AvailableSpace = nodeInfo.Storage.Available

	if wasOffline {
		log.Info().
			Str("backend", backendURL).
			Int64("latency_ms", status.Latency).
			Msg("Backend back online")
	}
}

// fetchNodeInfo fetches node information from a backend.
func (bm *BackendManager) fetchNodeInfo(ctx context.Context, backendURL string) (*models.NodeInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, backendURL+"/node/info", nil)
	if err != nil {
		return nil, err
	}

	resp, err := bm.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close health check response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, &BackendError{StatusCode: resp.StatusCode}
	}

	var nodeInfo models.NodeInfo
	if err := json.NewDecoder(resp.Body).Decode(&nodeInfo); err != nil {
		return nil, err
	}

	return &nodeInfo, nil
}

// BackendError represents an error from a backend server.
type BackendError struct {
	StatusCode int
}

func (e *BackendError) Error() string {
	return "backend returned status " + http.StatusText(e.StatusCode)
}

// CreateRetryableClient creates a retryable HTTP client for backend requests.
func CreateRetryableClient(retryMax int, retryWaitMin, retryWaitMax time.Duration) *retryablehttp.Client {
	client := retryablehttp.NewClient()
	client.RetryMax = retryMax
	client.RetryWaitMin = retryWaitMin
	client.RetryWaitMax = retryWaitMax
	client.Logger = nil // Disable retryablehttp logging
	// Custom retry policy: only retry on connection/timeout errors, not HTTP errors
	// This ensures we forward backend error responses instead of retrying them
	client.CheckRetry = customRetryPolicy
	return client
}

// customRetryPolicy only retries on connection/timeout errors, not HTTP status errors.
// This allows us to properly forward backend error responses (400, 404, 500, etc.)
// instead of retrying them and eventually returning a generic error.
func customRetryPolicy(ctx context.Context, resp *http.Response, err error) (bool, error) {
	// Do not retry if context is cancelled
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	// If we got a response, don't retry - forward the response as-is
	// This includes 4xx and 5xx errors from the backend
	if resp != nil {
		return false, nil
	}

	// Only retry if there's a connection/timeout error (no response received)
	// We intentionally return nil error here because retryablehttp will handle
	// the retry or final error reporting. The error is preserved internally.
	if err != nil {
		return true, nil //nolint:nilerr // intentionally returning nil - retryablehttp handles the error
	}

	return false, nil
}
