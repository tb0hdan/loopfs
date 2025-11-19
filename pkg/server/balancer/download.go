package balancer

import (
	"context"
	"io"
	"net/http"

	"loopfs/pkg/log"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/labstack/echo/v4"
)

// DownloadHandler handles file download requests.
//
//nolint:cyclop,funlen
func (b *Balancer) DownloadHandler(ctx echo.Context) error {
	hash := ctx.Param("hash")
	if hash == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "Hash parameter is required",
		})
	}

	type downloadResult struct {
		backend   string
		resp      *http.Response
		status    int
		err       error
		ctxCancel context.CancelFunc
	}

	results := make(chan downloadResult, len(b.backends))
	cancelCtx, cancel := context.WithCancel(ctx.Request().Context())
	defer cancel()

	// Try all backends in parallel
	for _, backend := range b.backends {
		go func(url string) {
			reqCtx, reqCancel := context.WithTimeout(cancelCtx, b.requestTimeout)

			req, err := retryablehttp.NewRequestWithContext(reqCtx, "GET", url+"/file/"+hash+"/download", nil)
			if err != nil {
				reqCancel()
				results <- downloadResult{backend: url, err: err}
				return
			}

			resp, err := b.client.Do(req)
			if err != nil {
				reqCancel()
				results <- downloadResult{backend: url, err: err}
				return
			}

			if resp.StatusCode == http.StatusOK {
				// Pass the cancel function so it can be called after the response is fully read
				results <- downloadResult{backend: url, status: resp.StatusCode, resp: resp, ctxCancel: reqCancel}
				return
			}

			reqCancel()
			results <- downloadResult{
				backend: url,
				status:  resp.StatusCode,
			}

			if closeErr := resp.Body.Close(); closeErr != nil {
				log.Warn().Err(closeErr).Str("backend", url).Msg("Failed to close download response body")
			}
		}(backend)
	}

	// Find successful result
	var lastError error
	notFoundCount := 0

	for range b.backends {
		result := <-results
		if result.err != nil {
			lastError = result.err
			log.Warn().Err(result.err).Str("backend", result.backend).Msg("Download failed")
			continue
		}

		if result.status == http.StatusOK && result.resp != nil {
			for k, v := range result.resp.Header {
				for _, val := range v {
					ctx.Response().Header().Add(k, val)
				}
			}

			if result.resp.Header.Get(echo.HeaderContentType) == "" {
				ctx.Response().Header().Set(echo.HeaderContentType, "application/octet-stream")
			}

			ctx.Response().WriteHeader(http.StatusOK)

			if _, err := io.Copy(ctx.Response().Writer, result.resp.Body); err != nil {
				if closeErr := result.resp.Body.Close(); closeErr != nil {
					log.Warn().Err(closeErr).Str("backend", result.backend).Msg("Failed to close download response body")
				}
				if result.ctxCancel != nil {
					result.ctxCancel()
				}
				cancel()
				return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
					"error": "Download failed: " + err.Error(),
				})
			}

			if closeErr := result.resp.Body.Close(); closeErr != nil {
				log.Warn().Err(closeErr).Str("backend", result.backend).Msg("Failed to close download response body")
			}

			// Clean up contexts after successfully reading the response
			if result.ctxCancel != nil {
				result.ctxCancel()
			}
			cancel()
			return nil
		}

		if result.status == http.StatusNotFound {
			notFoundCount++
		}
	}

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
