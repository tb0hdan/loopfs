package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"

	"loopfs/pkg/log"
	"loopfs/pkg/store"

	"github.com/labstack/echo/v4"
)

// copyAndHashToTempFile copies the reader content to a temp file while calculating SHA256 hash.
func (cas *CASServer) copyAndHashToTempFile(src io.Reader, tempFile *os.File) (string, error) {
	hasher := sha256.New()
	writer := io.MultiWriter(hasher, tempFile)

	if _, err := io.Copy(writer, src); err != nil {
		log.Error().Err(err).Msg("Failed to copy and hash file")
		return "", err
	}

	hash := hex.EncodeToString(hasher.Sum(nil))
	log.Debug().Str("hash", hash).Msg("Calculated hash for uploaded file")
	return hash, nil
}

// prepareUploadWithVerification handles the verification process for uploads when Store Manager is available.
// Returns the hash, temp file path, and cleanup function for efficient upload.
func (cas *CASServer) prepareUploadWithVerification(src io.Reader) (string, string, func(), error) {
	// Create a temporary file to save the upload for verification
	tempFile, err := os.CreateTemp("", "upload-*.tmp")
	if err != nil {
		log.Error().Err(err).Msg("Failed to create temporary file")
		return "", "", nil, err
	}
	tempPath := tempFile.Name()

	cleanup := func() {
		if removeErr := os.Remove(tempPath); removeErr != nil {
			log.Warn().Err(removeErr).Str("temp_file", tempPath).Msg("Failed to remove temp file")
		}
	}

	// Copy the uploaded content to the temp file and calculate hash
	hash, err := cas.copyAndHashToTempFile(src, tempFile)
	if closeErr := tempFile.Close(); closeErr != nil {
		log.Warn().Err(closeErr).Str("temp_file", tempPath).Msg("Failed to close temp file")
	}
	if err != nil {
		cleanup()
		log.Error().Err(err).Msg("Failed to save uploaded file to temp")
		return "", "", nil, err
	}

	// Call VerifyBlock with the actual hash to ensure there's enough space (resize if needed)
	if err := cas.storeMgr.VerifyBlock(tempPath, hash); err != nil {
		cleanup()
		log.Error().Err(err).Str("hash", hash).Msg("Failed to verify block space")
		return "", "", nil, err
	}

	return hash, tempPath, cleanup, nil
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

	result, err := cas.processUpload(src, file.Filename)
	if err != nil {
		return cas.handleUploadError(ctx, err)
	}

	return ctx.JSON(http.StatusOK, map[string]string{
		"hash": result.Hash,
	})
}

// processUpload handles the core upload logic with store manager verification.
func (cas *CASServer) processUpload(src io.Reader, filename string) (*store.UploadResult, error) {
	// If we have a Store Manager, use the efficient single-pass upload flow
	if cas.storeMgr != nil {
		hash, tempPath, cleanup, prepErr := cas.prepareUploadWithVerification(src)
		if prepErr != nil {
			return nil, prepErr
		}
		defer cleanup()

		// Use the efficient UploadWithHash method to avoid redundant temp files and hashing
		return cas.storeMgr.UploadWithHash(tempPath, hash, filename)
	}

	// Fallback to traditional upload flow for stores without manager
	return cas.store.Upload(src, filename)
}

// handleUploadError handles different types of upload errors and returns appropriate JSON responses.
func (cas *CASServer) handleUploadError(ctx echo.Context, err error) error {
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
