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

type infoData struct {
	info *models.FileInfo
}

// FileInfoHandler handles file info requests.
func (b *Balancer) FileInfoHandler(ctx echo.Context) error {
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

	// Execute info request across online backends with early cancellation on success
	results := executeBackendRequests(ctx.Request().Context(), backends, b.requestTimeout,
		func(reqCtx context.Context, backend string) (infoData, int, error) {
			return b.executeInfoRequest(reqCtx, backend, hash)
		},
		true, // Cancel on success - we only need one response
	)

	return b.processInfoResults(ctx, results)
}

func (b *Balancer) executeInfoRequest(reqCtx context.Context, backend, hash string) (infoData, int, error) {
	req, err := retryablehttp.NewRequestWithContext(reqCtx, "GET", backend+"/file/"+hash+"/info", nil)
	if err != nil {
		return infoData{}, 0, err
	}

	resp, err := b.client.Do(req)
	if err != nil {
		// Mark backend as dead on timeout or connection errors
		if isTimeoutOrConnectionError(err) {
			b.backendManager.MarkBackendDead(backend, err)
		}
		return infoData{}, 0, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Str("backend", backend).Msg("Failed to close info response body")
		}
	}()

	if resp.StatusCode == http.StatusOK {
		var info models.FileInfo
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			return infoData{}, resp.StatusCode, err
		}
		return infoData{info: &info}, resp.StatusCode, nil
	}

	return infoData{}, resp.StatusCode, nil
}

func (b *Balancer) processInfoResults(ctx echo.Context, results <-chan RequestResult[infoData]) error {
	var lastError error
	notFoundCount := 0
	backendCount := b.backendManager.BackendCount()

	for result := range results {
		// Clean up cancel function if present
		if result.CtxCancel != nil {
			defer result.CtxCancel()
		}

		if result.Error != nil {
			lastError = result.Error
			log.Warn().Err(result.Error).Str("backend", result.Backend).Msg("Info request failed")
			continue
		}

		if result.Status == http.StatusOK && result.Data.info != nil {
			return ctx.JSON(http.StatusOK, result.Data.info)
		}

		if result.Status == http.StatusNotFound {
			notFoundCount++
		}
	}

	return b.buildInfoErrorResponse(ctx, notFoundCount, backendCount, lastError)
}

func (b *Balancer) buildInfoErrorResponse(ctx echo.Context, notFoundCount, backendCount int, lastError error) error {
	// If all backends returned not found, return 404
	if notFoundCount == backendCount {
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
