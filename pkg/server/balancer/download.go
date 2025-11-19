package balancer

import (
	"context"
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

	// Execute download request across all backends
	results := executeBackendRequests(ctx.Request().Context(), b.backends, b.requestTimeout,
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

	return b.buildDownloadErrorResponse(ctx, notFoundCount, lastError)
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

	if _, err := io.Copy(ctx.Response().Writer, resp.Body); err != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "Download failed: " + err.Error(),
		})
	}

	// Drain remaining results in background to prevent goroutine leaks
	go b.drainRemainingResults(results)

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
	for r := range results {
		// Clean up any remaining cancel functions
		if r.CtxCancel != nil {
			r.CtxCancel()
		}
	}
}

func (b *Balancer) buildDownloadErrorResponse(ctx echo.Context, notFoundCount int, lastError error) error {
	// If all backends returned not found, return 404
	if notFoundCount == len(b.backends) {
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
