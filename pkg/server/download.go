package server

import (
	"errors"
	"net/http"

	"loopfs/pkg/log"
	"loopfs/pkg/store"

	"github.com/labstack/echo/v4"
)

func (cas *CASServer) downloadFile(ctx echo.Context) error {
	hash := ctx.Param("hash")
	log.Debug().Str("hash", hash).Msg("File download request")

	reader, err := cas.store.DownloadStream(hash)
	if err != nil {
		var notFoundErr store.FileNotFoundError
		if errors.As(err, &notFoundErr) {
			return ctx.JSON(http.StatusNotFound, map[string]string{
				"error": "file not found",
			})
		}
		var invalidHashErr store.InvalidHashError
		if errors.As(err, &invalidHashErr) {
			return ctx.JSON(http.StatusBadRequest, map[string]string{
				"error": "invalid hash format",
			})
		}
		log.Error().Err(err).Msg("Failed to download file")
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to download file",
		})
	}

	// Ensure reader is closed after serving to cleanup mount resources
	defer func() {
		if err := reader.Close(); err != nil {
			log.Error().Err(err).Str("hash", hash).Msg("Failed to close streaming reader")
		}
	}()

	log.Debug().Str("hash", hash).Msg("Serving streaming file download")

	// Stream the file directly to the client
	return ctx.Stream(http.StatusOK, "application/octet-stream", reader)
}
