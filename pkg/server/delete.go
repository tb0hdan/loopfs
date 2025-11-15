package server

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"loopfs/pkg/log"
	"loopfs/pkg/store"
)

// deleteFile handles DELETE /file/{hash}/delete requests.
func (cas *CASServer) deleteFile(ctx echo.Context) error {
	hash := ctx.Param("hash")

	log.Info().
		Str("hash", hash).
		Str("method", "DELETE").
		Str("path", ctx.Request().URL.Path).
		Msg("File delete request")

	// Validate hash format
	if !cas.store.ValidateHash(hash) {
		log.Warn().Str("hash", hash).Msg("Invalid hash format for delete")
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid hash format",
		})
	}

	// Delete the file
	if err := cas.store.Delete(hash); err != nil {
		var fileNotFoundErr store.FileNotFoundError
		var invalidHashErr store.InvalidHashError

		if errors.As(err, &fileNotFoundErr) {
			log.Warn().Str("hash", hash).Msg("File not found for delete")
			return ctx.JSON(http.StatusNotFound, map[string]string{
				"error": "File not found",
			})
		} else if errors.As(err, &invalidHashErr) {
			log.Warn().Str("hash", hash).Msg("Invalid hash for delete")
			return ctx.JSON(http.StatusBadRequest, map[string]string{
				"error": "Invalid hash format",
			})
		} else {
			log.Error().Err(err).Str("hash", hash).Msg("Delete failed")
			return ctx.JSON(http.StatusInternalServerError, map[string]string{
				"error": "Internal server error",
			})
		}
	}

	log.Info().Str("hash", hash).Msg("File deleted successfully")
	return ctx.JSON(http.StatusOK, map[string]string{
		"message": "File deleted successfully",
		"hash":    hash,
	})
}