package balancer

import (
	"context"
	"encoding/json"
	"net/http"

	"loopfs/pkg/log"
	"loopfs/pkg/models"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/labstack/echo/v4"
)

// FileInfoHandler handles file info requests.
//
//nolint:cyclop,funlen
func (b *Balancer) FileInfoHandler(ctx echo.Context) error {
	hash := ctx.Param("hash")
	if hash == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "Hash parameter is required",
		})
	}

	type infoResult struct {
		backend string
		info    *models.FileInfo
		status  int
		err     error
	}

	results := make(chan infoResult, len(b.backends))
	cancelCtx, cancel := context.WithCancel(ctx.Request().Context())
	defer cancel()

	// Query all backends in parallel
	for _, backend := range b.backends {
		go func(url string) {
			reqCtx, reqCancel := context.WithTimeout(cancelCtx, b.requestTimeout)
			defer reqCancel()

			req, err := retryablehttp.NewRequestWithContext(reqCtx, "GET", url+"/file/"+hash+"/info", nil)
			if err != nil {
				results <- infoResult{backend: url, err: err}
				return
			}

			resp, err := b.client.Do(req)
			if err != nil {
				results <- infoResult{backend: url, err: err}
				return
			}
			defer func() {
				if closeErr := resp.Body.Close(); closeErr != nil {
					log.Warn().Err(closeErr).Str("backend", url).Msg("Failed to close info response body")
				}
			}()

			if resp.StatusCode == http.StatusOK {
				var info models.FileInfo
				if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
					results <- infoResult{backend: url, err: err}
					return
				}
				results <- infoResult{
					backend: url,
					info:    &info,
					status:  resp.StatusCode,
				}
				cancel() // Cancel other requests on success
			} else {
				results <- infoResult{
					backend: url,
					status:  resp.StatusCode,
				}
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
			log.Warn().Err(result.err).Str("backend", result.backend).Msg("Info request failed")
			continue
		}

		if result.status == http.StatusOK && result.info != nil {
			return ctx.JSON(http.StatusOK, result.info)
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
			"error": "Info request failed: " + lastError.Error(),
		})
	}

	return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
		"error": "Info request failed from all backends",
	})
}
