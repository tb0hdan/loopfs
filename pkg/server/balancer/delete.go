package balancer

import (
	"context"
	"io"
	"net/http"

	"loopfs/pkg/log"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/labstack/echo/v4"
)

type deleteData struct {
	body []byte
}

// DeleteHandler handles file deletion requests.
func (b *Balancer) DeleteHandler(ctx echo.Context) error {
	hash := ctx.Param("hash")
	if hash == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "Hash parameter is required",
		})
	}

	// Execute delete request across all backends
	results := executeBackendRequests(ctx.Request().Context(), b.backends, b.requestTimeout,
		func(reqCtx context.Context, backend string) (deleteData, int, error) {
			return b.executeDeleteRequest(reqCtx, backend, hash)
		},
		false, // Don't cancel on success - delete from all backends
	)

	return b.processDeleteResults(ctx, results, hash)
}

func (b *Balancer) executeDeleteRequest(reqCtx context.Context, backend, hash string) (deleteData, int, error) {
	req, err := retryablehttp.NewRequestWithContext(reqCtx, "DELETE", backend+"/file/"+hash+"/delete", nil)
	if err != nil {
		return deleteData{}, 0, err
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return deleteData{}, 0, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Str("backend", backend).Msg("Failed to close delete response body")
		}
	}()

	body, _ := io.ReadAll(resp.Body)
	return deleteData{body: body}, resp.StatusCode, nil
}

func (b *Balancer) processDeleteResults(ctx echo.Context, results <-chan RequestResult[deleteData], hash string) error {
	var (
		successCount  int
		notFoundCount int
		lastError     error
		successBody   []byte
	)

	for result := range results {
		// Clean up cancel function if present
		if result.CtxCancel != nil {
			result.CtxCancel()
		}

		if result.Error != nil {
			lastError = result.Error
			log.Warn().Err(result.Error).Str("backend", result.Backend).Msg("Delete failed")
			continue
		}

		successCount, notFoundCount, successBody = b.handleDeleteResult(result, successCount, notFoundCount, successBody)
	}

	return b.buildDeleteResponse(ctx, successCount, notFoundCount, lastError, successBody, hash)
}

func (b *Balancer) handleDeleteResult(result RequestResult[deleteData], successCount, notFoundCount int, successBody []byte) (int, int, []byte) {
	switch result.Status {
	case http.StatusOK:
		successCount++
		if successBody == nil {
			successBody = result.Data.body
		}
	case http.StatusNotFound:
		notFoundCount++
	default:
		log.Warn().Str("backend", result.Backend).Int("status", result.Status).Msg("Delete returned unexpected status")
	}
	return successCount, notFoundCount, successBody
}

func (b *Balancer) buildDeleteResponse(ctx echo.Context, successCount, notFoundCount int, lastError error, successBody []byte, hash string) error {
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
