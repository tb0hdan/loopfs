package balancer

import (
	"context"
	"errors"
	"io"
	"net/http"

	"loopfs/pkg/log"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/labstack/echo/v4"
)

type downloadData struct {
	resp *http.Response
}

// DownloadHandler handles file download requests.
func (b *Balancer) DownloadHandler(ctx echo.Context) error {
	hash := ctx.Param("hash")
	if hash == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "Hash parameter is required",
		})
	}

	// Check if any backends are online
	backends := b.backendManager.GetOnlineBackends()
	if len(backends) == 0 {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": ErrAllBackendsDown.Error(),
		})
	}

	// Execute download request across online backends
	results := executeBackendRequests(ctx.Request().Context(), backends, b.requestTimeout,
		func(reqCtx context.Context, backend string) (downloadData, int, error) {
			return b.executeDownloadRequest(reqCtx, backend, hash)
		},
		false, // Don't cancel on success - we need to stream the response body first
	)

	return b.processDownloadResults(ctx, results)
}

func (b *Balancer) executeDownloadRequest(reqCtx context.Context, backend, hash string) (downloadData, int, error) {
	req, err := retryablehttp.NewRequestWithContext(reqCtx, "GET", backend+"/file/"+hash+"/download", nil)
	if err != nil {
		return downloadData{}, 0, err
	}

	resp, err := b.client.Do(req)
	if err != nil {
		// Mark backend as dead on timeout or connection errors
		if isTimeoutOrConnectionError(err) {
			b.backendManager.MarkBackendDead(backend, err)
		}
		return downloadData{}, 0, err
	}

	if resp.StatusCode == http.StatusOK {
		// Return response without closing body - it will be streamed to the client
		return downloadData{resp: resp}, resp.StatusCode, nil
	}

	// Close body for non-success responses
	if closeErr := resp.Body.Close(); closeErr != nil {
		log.Warn().Err(closeErr).Str("backend", backend).Msg("Failed to close download response body")
	}

	return downloadData{}, resp.StatusCode, nil
}

func (b *Balancer) processDownloadResults(ctx echo.Context, results <-chan RequestResult[downloadData]) error {
	var lastError error
	notFoundCount := 0
	backendCount := b.backendManager.BackendCount()

	for result := range results {
		if result.Error != nil {
			lastError = result.Error
			log.Warn().Err(result.Error).Str("backend", result.Backend).Msg("Download failed")
			continue
		}

		if result.Status == http.StatusOK && result.Data.resp != nil {
			return b.streamDownloadResponse(ctx, result, results)
		}

		if result.Status == http.StatusNotFound {
			notFoundCount++
		}
	}

	return b.buildDownloadErrorResponse(ctx, notFoundCount, backendCount, lastError)
}

func (b *Balancer) streamDownloadResponse(ctx echo.Context, result RequestResult[downloadData], results <-chan RequestResult[downloadData]) error {
	resp := result.Data.resp
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Str("backend", result.Backend).Msg("Failed to close download response body")
		}
		// Clean up the request context after streaming is complete
		if result.CtxCancel != nil {
			result.CtxCancel()
		}
	}()

	b.copyResponseHeaders(ctx, resp)
	ctx.Response().WriteHeader(http.StatusOK)

	// Drain remaining results in background to prevent goroutine leaks
	go b.drainRemainingResults(results)

	if _, err := io.Copy(ctx.Response().Writer, resp.Body); err != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "Download failed: " + err.Error(),
		})
	}

	return nil
}

func (b *Balancer) copyResponseHeaders(ctx echo.Context, resp *http.Response) {
	for k, v := range resp.Header {
		for _, val := range v {
			ctx.Response().Header().Add(k, val)
		}
	}

	if resp.Header.Get(echo.HeaderContentType) == "" {
		ctx.Response().Header().Set(echo.HeaderContentType, "application/octet-stream")
	}
}

func (b *Balancer) drainRemainingResults(results <-chan RequestResult[downloadData]) {
	for result := range results {
		// Clean up any remaining cancel functions
		if result.CtxCancel != nil {
			result.CtxCancel()
		}
		if result.Data.resp != nil {
			if closeErr := result.Data.resp.Body.Close(); closeErr != nil {
				log.Warn().Err(closeErr).Str("backend", result.Backend).Msg("Failed to close download response body in drain")
			}
		}
	}
}

func (b *Balancer) buildDownloadErrorResponse(ctx echo.Context, notFoundCount, backendCount int, lastError error) error {
	// If all backends returned not found, return 404
	if notFoundCount == backendCount {
		return ctx.JSON(http.StatusNotFound, map[string]string{
			"error": "File not found",
		})
	}

	if lastError != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "Download failed: " + lastError.Error(),
		})
	}

	return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
		"error": "Download failed from all backends",
	})
}

// connectionErrorPatterns contains common error message patterns for timeout/connection errors.
var connectionErrorPatterns = []string{
	"timeout",
	"deadline exceeded",
	"connection refused",
	"no such host",
	"network is unreachable",
	"i/o timeout",
	"dial tcp", // Connection errors (includes DNS failures)
	"dial udp", // UDP connection errors
}

// isTimeoutOrConnectionError checks if the error is a timeout or connection error.
// It returns false for context.Canceled since that indicates intentional cancellation
// (e.g., when another backend succeeded), not a backend failure.
func isTimeoutOrConnectionError(err error) bool {
	if err == nil {
		return false
	}

	// context.Canceled means intentional cancellation (e.g., another backend succeeded)
	// This should NOT mark the backend as dead
	if errors.Is(err, context.Canceled) {
		return false
	}

	// Also check the error string for "context canceled" since wrapped errors
	// may contain this text even if errors.Is doesn't match
	errStr := err.Error()
	if contains(errStr, "context canceled") {
		return false
	}

	// context.DeadlineExceeded means timeout - this IS a connection issue
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Check error message for common patterns
	for _, pattern := range connectionErrorPatterns {
		if contains(errStr, pattern) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
