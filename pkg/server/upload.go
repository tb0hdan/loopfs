package server

import (
	"errors"
	"net/http"

	"loopfs/pkg/log"
	"loopfs/pkg/store"

	"github.com/labstack/echo/v4"
)

func (cas *CASServer) uploadFile(ctx echo.Context) error {
	log.Info().Msg("File upload request received")

	file, err := ctx.FormFile("file")
	if err != nil {
		log.Error().Err(err).Msg("File parameter is required")
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "file parameter is required",
		})
	}

	src, err := file.Open()
	if err != nil {
		log.Error().Err(err).Msg("Failed to open uploaded file")
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to open uploaded file",
		})
	}
	defer func() {
		if err := src.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close source file")
		}
	}()

	result, err := cas.store.Upload(src, file.Filename)
	if err != nil {
		var fileExistsErr store.FileExistsError
		if errors.As(err, &fileExistsErr) {
			return ctx.JSON(http.StatusConflict, map[string]string{
				"error": "file already exists",
				"hash":  fileExistsErr.Hash,
			})
		}
		var invalidHashErr store.InvalidHashError
		if errors.As(err, &invalidHashErr) {
			return ctx.JSON(http.StatusBadRequest, map[string]string{
				"error": "invalid hash",
			})
		}
		log.Error().Err(err).Msg("Failed to upload file")
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to upload file",
		})
	}

	return ctx.JSON(http.StatusOK, map[string]string{
		"hash": result.Hash,
	})
}
