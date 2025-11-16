package server

import (
	"errors"
	"io"
	"net/http"
	"os"

	"loopfs/pkg/log"
	"loopfs/pkg/store"

	"github.com/labstack/echo/v4"
)

// prepareUploadWithVerification handles the verification process for uploads when Store Manager is available.
func (cas *CASServer) prepareUploadWithVerification(src io.Reader) (io.Reader, func(), error) {
	// Create a temporary file to save the upload for verification
	tempFile, err := os.CreateTemp("", "upload-*.tmp")
	if err != nil {
		log.Error().Err(err).Msg("Failed to create temporary file")
		return nil, nil, err
	}
	tempPath := tempFile.Name()

	cleanup := func() {
		if removeErr := os.Remove(tempPath); removeErr != nil {
			log.Warn().Err(removeErr).Str("temp_file", tempPath).Msg("Failed to remove temp file")
		}
	}

	// Copy the uploaded content to the temp file
	_, err = io.Copy(tempFile, src)
	if closeErr := tempFile.Close(); closeErr != nil {
		log.Warn().Err(closeErr).Str("temp_file", tempPath).Msg("Failed to close temp file")
	}
	if err != nil {
		cleanup()
		log.Error().Err(err).Msg("Failed to save uploaded file to temp")
		return nil, nil, err
	}

	// Call VerifyBlock to ensure there's enough space (resize if needed)
	// Note: We pass empty hash since this is a new upload
	if err := cas.storeMgr.VerifyBlock(tempPath, ""); err != nil {
		cleanup()
		log.Error().Err(err).Msg("Failed to verify block space")
		return nil, nil, err
	}

	// Reopen the temp file for upload
	//nolint:gosec // tempPath is created by os.CreateTemp, not user input
	newSrc, err := os.Open(tempPath)
	if err != nil {
		cleanup()
		log.Error().Err(err).Msg("Failed to reopen temp file")
		return nil, nil, err
	}

	// Extended cleanup to also close the file
	extendedCleanup := func() {
		if closeErr := newSrc.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Str("temp_file", tempPath).Msg("Failed to close reopened temp file")
		}
		cleanup()
	}

	return newSrc, extendedCleanup, nil
}

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

	// If we have a Store Manager, we need to verify the block has enough space
	var uploadSrc io.Reader = src
	var cleanup func()
	if cas.storeMgr != nil {
		var prepErr error
		uploadSrc, cleanup, prepErr = cas.prepareUploadWithVerification(src)
		if prepErr != nil {
			return ctx.JSON(http.StatusInternalServerError, map[string]string{
				"error": "failed to prepare upload",
			})
		}
		defer cleanup()
	}

	result, err := cas.store.Upload(uploadSrc, file.Filename)
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
