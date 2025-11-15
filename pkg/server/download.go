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
	log.Info().Str("hash", hash).Msg("File download request")

	filePath, err := cas.store.Download(hash)
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

	log.Info().Str("hash", hash).Str("file_path", filePath).Msg("Serving file download")
	return ctx.File(filePath)
}
