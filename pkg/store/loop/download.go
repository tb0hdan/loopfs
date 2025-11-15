package loop

import (
	"io"
	"os"
	"strings"

	"loopfs/pkg/log"
	"loopfs/pkg/store"
)

// Download retrieves a file by its hash and returns a temporary file path.
func (s *Store) Download(hash string) (string, error) {
	hash = strings.ToLower(hash)
	if !s.ValidateHash(hash) {
		log.Error().Str("hash", hash).Msg("Invalid hash format")
		return "", store.InvalidHashError{Hash: hash}
	}

	// Check if loop file exists first
	loopFilePath := s.getLoopFilePath(hash)
	if _, err := os.Stat(loopFilePath); os.IsNotExist(err) {
		log.Info().Str("hash", hash).Str("loop_file", loopFilePath).Msg("Loop file not found")
		return "", store.FileNotFoundError{Hash: hash}
	} else if err != nil {
		return "", err
	}

	var tempFilePath string
	err := s.withMountedLoop(hash, func() error {
		filePath, err := s.findFileInLoop(hash)
		if err != nil {
			log.Info().Str("hash", hash).Msg("File not found in loop")
			return err
		}

		// Create a temporary file to copy the content
		tempFile, err := os.CreateTemp("", "cas-download-*")
		if err != nil {
			log.Error().Err(err).Msg("Failed to create temporary file for download")
			return err
		}
		defer func() {
			if err := tempFile.Close(); err != nil {
				log.Error().Err(err).Msg("Failed to close temporary download file")
			}
		}()

		// Copy file content to temporary file
		srcFile, err := os.Open(filePath) //nolint:gosec // filePath is constructed from validated hash, not user input
		if err != nil {
			log.Error().Err(err).Str("source_file", filePath).Msg("Failed to open source file for download")
			return err
		}
		defer func() {
			if err := srcFile.Close(); err != nil {
				log.Error().Err(err).Msg("Failed to close source file")
			}
		}()

		if _, err := io.Copy(tempFile, srcFile); err != nil {
			log.Error().Err(err).Msg("Failed to copy file for download")
			if removeErr := os.Remove(tempFile.Name()); removeErr != nil {
				log.Error().Err(removeErr).Str("temp_file", tempFile.Name()).Msg("Failed to remove temporary file after copy error")
			}
			return err
		}

		tempFilePath = tempFile.Name()
		log.Info().Str("hash", hash).Str("temp_file", tempFilePath).Msg("File copied for download")
		return nil
	})

	if err != nil {
		return "", err
	}

	return tempFilePath, nil
}
