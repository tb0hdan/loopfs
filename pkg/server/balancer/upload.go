package balancer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"loopfs/pkg/log"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/labstack/echo/v4"
)

// UploadHandler handles file upload requests.
//
//nolint:cyclop,funlen
func (b *Balancer) UploadHandler(ctx echo.Context) error {
	// Check if any backends are online
	if !b.backendManager.HasOnlineBackends() {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": ErrAllBackendsDown.Error(),
		})
	}

	// Parse multipart form
	file, err := ctx.FormFile("file")
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "No file provided",
		})
	}

	// Read file into buffer to determine size
	fileSize := file.Size

	// Select backend with most available space
	backend, err := b.backendManager.GetBackendForUpload(fileSize)
	if err != nil {
		if errors.Is(err, ErrNoBackendAvailable) {
			return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
				"error": ErrAllBackendsDown.Error(),
			})
		}
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": err.Error(),
		})
	}

	reqCtx, cancel := context.WithTimeout(ctx.Request().Context(), b.requestTimeout)
	defer cancel()

	// Prepare streaming multipart request
	boundary := fmt.Sprintf("loopfs-%d", time.Now().UnixNano())
	uploadBody, contentType, err := createStreamingBody(reqCtx, file, boundary)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to prepare upload body",
		})
	}
	defer func() {
		if closeErr := uploadBody.Close(); closeErr != nil && !errors.Is(closeErr, io.ErrClosedPipe) {
			log.Warn().Err(closeErr).Msg("Failed to close upload body")
		}
	}()

	// Create request
	req, err := retryablehttp.NewRequestWithContext(reqCtx, "POST", backend+"/file/upload", uploadBody)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to create request",
		})
	}
	req.Header.Set("Content-Type", contentType)

	// Execute request
	resp, err := b.client.Do(req)
	if err != nil {
		// Mark backend as dead on timeout or connection errors
		if isTimeoutOrConnectionError(err) {
			b.backendManager.MarkBackendDead(backend, err)
		}
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "Upload failed: " + err.Error(),
		})
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close upload response body")
		}
	}()

	// Forward response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to read response",
		})
	}

	ctx.Response().Header().Set(echo.HeaderContentType, resp.Header.Get(echo.HeaderContentType))
	return ctx.JSONBlob(resp.StatusCode, respBody)
}

func createStreamingBody(ctx context.Context, file *multipart.FileHeader, boundary string) (io.ReadCloser, string, error) {
	if err := validateBoundary(boundary); err != nil {
		return nil, "", err
	}

	pipeReader, pipeWriter := io.Pipe()
	contentType := "multipart/form-data; boundary=" + boundary

	go func() {
		defer func() {
			if closeErr := pipeWriter.Close(); closeErr != nil && !errors.Is(closeErr, io.ErrClosedPipe) {
				log.Warn().Err(closeErr).Msg("Failed to close pipe writer")
			}
		}()

		writer := multipart.NewWriter(pipeWriter)
		if err := writer.SetBoundary(boundary); err != nil {
			pipeWriter.CloseWithError(err)
			return
		}

		part, err := writer.CreateFormFile("file", file.Filename)
		if err != nil {
			pipeWriter.CloseWithError(err)
			return
		}

		src, err := file.Open()
		if err != nil {
			pipeWriter.CloseWithError(err)
			return
		}
		defer func() {
			if closeErr := src.Close(); closeErr != nil {
				log.Warn().Err(closeErr).Msg("Failed to close uploaded file reader")
			}
		}()

		if _, err := io.Copy(part, src); err != nil {
			pipeWriter.CloseWithError(err)
			return
		}

		if err := writer.Close(); err != nil {
			pipeWriter.CloseWithError(err)
			return
		}
	}()

	go func() {
		<-ctx.Done()
		pipeWriter.CloseWithError(ctx.Err())
	}()

	return pipeReader, contentType, nil
}

func validateBoundary(boundary string) error {
	writer := multipart.NewWriter(io.Discard)
	defer func() {
		if closeErr := writer.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("Failed to close boundary validator")
		}
	}()
	return writer.SetBoundary(boundary)
}
