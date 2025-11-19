package balancer

import (
	"context"
	"io"
	"net/http"
	"sync"

	"loopfs/pkg/log"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/labstack/echo/v4"
)

// DeleteHandler handles file deletion requests.
//
//nolint:cyclop,funlen
func (b *Balancer) DeleteHandler(ctx echo.Context) error {
	hash := ctx.Param("hash")
	if hash == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "Hash parameter is required",
		})
	}

	type deleteResult struct {
		backend string
		status  int
		body    []byte
		err     error
	}

	results := make(chan deleteResult, len(b.backends))
	var waitGroup sync.WaitGroup

	// Try to delete from all backends
	for _, backend := range b.backends {
		waitGroup.Add(1)
		go func(url string) {
			defer waitGroup.Done()

			reqCtx, cancel := context.WithTimeout(ctx.Request().Context(), b.requestTimeout)
			defer cancel()

			req, err := retryablehttp.NewRequestWithContext(reqCtx, "DELETE", url+"/file/"+hash+"/delete", nil)
			if err != nil {
				results <- deleteResult{backend: url, err: err}
				return
			}

			resp, err := b.client.Do(req)
			if err != nil {
				results <- deleteResult{backend: url, err: err}
				return
			}
			defer func() {
				if closeErr := resp.Body.Close(); closeErr != nil {
					log.Warn().Err(closeErr).Str("backend", url).Msg("Failed to close delete response body")
				}
			}()

			body, _ := io.ReadAll(resp.Body)
			results <- deleteResult{
				backend: url,
				status:  resp.StatusCode,
				body:    body,
			}
		}(backend)
	}

	waitGroup.Wait()
	close(results)

	// Collect results
	var (
		successCount  int
		notFoundCount int
		lastError     error
		successBody   []byte
	)

	for result := range results {
		if result.err != nil {
			lastError = result.err
			log.Warn().Err(result.err).Str("backend", result.backend).Msg("Delete failed")
			continue
		}

		switch result.status {
		case http.StatusOK:
			successCount++
			if successBody == nil {
				successBody = result.body
			}
		case http.StatusNotFound:
			notFoundCount++
		default:
			log.Warn().Str("backend", result.backend).Int("status", result.status).Msg("Delete returned unexpected status")
		}
	}

	// If deleted from at least one backend, return success
	if successCount > 0 {
		if len(successBody) > 0 {
			return ctx.JSONBlob(http.StatusOK, successBody)
		}
		return ctx.JSON(http.StatusOK, map[string]string{
			"message": "File deleted successfully",
			"hash":    hash,
		})
	}

	// If all backends returned not found, return 404
	if notFoundCount == len(b.backends) {
		return ctx.JSON(http.StatusNotFound, map[string]string{
			"error": "File not found",
		})
	}

	if lastError != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "Delete failed: " + lastError.Error(),
		})
	}

	return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
		"error": "Delete failed on all backends",
	})
}
