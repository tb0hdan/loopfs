package loop

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"

	"loopfs/pkg/log"
	"loopfs/pkg/store"
)

// Upload stores a file from the given reader and returns its hash.
func (s *Store) Upload(reader io.Reader, filename string) (*store.UploadResult, error) {
	log.Info().Str("filename", filename).Msg("Processing file upload")

	hasher := sha256.New()
	tempFile, err := os.CreateTemp("", "cas-upload-*")
	if err != nil {
		log.Error().Err(err).Msg("Failed to create temporary file")
		return nil, err
	}

	defer func() {
		if err := os.Remove(tempFile.Name()); err != nil {
			log.Error().Err(err).Str("temp_file", tempFile.Name()).Msg("Failed to remove temporary file")
		}
	}()
	defer func() {
		if err := tempFile.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close temporary file")
		}
	}()

	writer := io.MultiWriter(hasher, tempFile)
	if _, err := io.Copy(writer, reader); err != nil {
		log.Error().Err(err).Msg("Failed to process file")
		return nil, err
	}

	hash := hex.EncodeToString(hasher.Sum(nil))

	// Check if file already exists
	if exists, err := s.Exists(hash); err != nil {
		return nil, err
	} else if exists {
		log.Info().Str("hash", hash).Msg("File already exists")
		return nil, store.FileExistsError{Hash: hash}
	}

	// Upload the file using mounted loop file
	err = s.withMountedLoop(hash, func() error {
		targetPath := s.getFilePath(hash)
		if targetPath == "" {
			log.Error().Str("hash", hash).Msg("Invalid hash generated")
			return store.InvalidHashError{Hash: hash}
		}

		// Create directory structure for the file
		targetDir := filepath.Dir(targetPath)
		if err := os.MkdirAll(targetDir, dirPerm); err != nil {
			log.Error().Err(err).Str("target_dir", targetDir).Msg("Failed to create target directory")
			return err
		}

		if _, err := tempFile.Seek(0, 0); err != nil {
			log.Error().Err(err).Msg("Failed to seek to beginning of temporary file")
			return err
		}

		//nolint:gosec // targetPath is constructed from validated hash, not user input
		dst, err := os.Create(targetPath)
		if err != nil {
			log.Error().Err(err).Str("target_path", targetPath).Msg("Failed to create destination file")
			return err
		}

		defer func() {
			if err := dst.Close(); err != nil {
				log.Error().Err(err).Msg("Failed to close destination file")
			}
		}()

		if _, err := io.Copy(dst, tempFile); err != nil {
			if err := os.Remove(targetPath); err != nil {
				log.Error().Err(err).Str("target_path", targetPath).Msg("Failed to remove target file after copy error")
			}
			log.Error().Err(err).Str("target_path", targetPath).Msg("Failed to save file")
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	log.Info().Str("hash", hash).Str("filename", filename).Msg("File uploaded successfully")
	return &store.UploadResult{Hash: hash}, nil
}
