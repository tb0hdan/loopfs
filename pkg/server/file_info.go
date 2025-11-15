package server

import (
	"errors"
	"net/http"
	"time"

	"loopfs/pkg/log"
	"loopfs/pkg/store"

	"github.com/labstack/echo/v4"
)

func (cas *CASServer) getFileInfo(ctx echo.Context) error {
	hash := ctx.Param("hash")
	log.Info().Str("hash", hash).Msg("File info request")

	fileInfo, err := cas.store.GetFileInfo(hash)
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
		log.Error().Err(err).Msg("Failed to get file info")
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get file info",
		})
	}

	// Get disk usage information for this specific file's loop filesystem
	diskUsage, err := cas.store.GetDiskUsage(hash)
	if err != nil {
		log.Error().Err(err).Str("hash", hash).Msg("Failed to get disk usage")
		// Return file info without disk usage if it fails
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"hash":       fileInfo.Hash,
			"size":       fileInfo.Size,
			"created_at": fileInfo.CreatedAt.Format(time.RFC3339),
		})
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"hash":            fileInfo.Hash,
		"size":            fileInfo.Size,
		"created_at":      fileInfo.CreatedAt.Format(time.RFC3339),
		"space_used":      diskUsage.SpaceUsed,
		"space_available": diskUsage.SpaceAvailable,
	})
}
