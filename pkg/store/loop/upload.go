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

	hash, tempFile, err := s.processAndHashFile(reader)
	if err != nil {
		return nil, err
	}
	defer s.cleanupTempFile(tempFile)

	// Check if file already exists
	if exists, err := s.Exists(hash); err != nil {
		return nil, err
	} else if exists {
		log.Info().Str("hash", hash).Msg("File already exists")
		return nil, store.FileExistsError{Hash: hash}
	}

	// Upload the file using mounted loop file
	err = s.withMountedLoop(hash, func() error {
		return s.saveFileToLoop(hash, tempFile)
	})

	if err != nil {
		return nil, err
	}

	log.Info().Str("hash", hash).Str("filename", filename).Msg("File uploaded successfully")
	return &store.UploadResult{Hash: hash}, nil
}

// processAndHashFile reads from reader, hashes content and saves to temp file.
func (s *Store) processAndHashFile(reader io.Reader) (string, *os.File, error) {
	hasher := sha256.New()
	tempFile, err := os.CreateTemp("", "cas-upload-*")
	if err != nil {
		log.Error().Err(err).Msg("Failed to create temporary file")
		return "", nil, err
	}

	writer := io.MultiWriter(hasher, tempFile)
	if _, err := io.Copy(writer, reader); err != nil {
		log.Error().Err(err).Msg("Failed to process file")
		s.cleanupTempFile(tempFile)
		return "", nil, err
	}

	hash := hex.EncodeToString(hasher.Sum(nil))
	return hash, tempFile, nil
}

// saveFileToLoop saves the temporary file to the loop filesystem.
func (s *Store) saveFileToLoop(hash string, tempFile *os.File) error {
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
}

// UploadWithHash stores a file using a pre-calculated hash and temp file path.
// This method is more efficient as it avoids redundant hashing and temp file creation.
func (s *Store) UploadWithHash(tempFilePath, hash, filename string) (*store.UploadResult, error) {
	log.Info().Str("filename", filename).Str("hash", hash).Msg("Processing file upload with pre-calculated hash")

	// Validate the provided hash
	if !s.ValidateHash(hash) {
		return nil, store.InvalidHashError{Hash: hash}
	}

	// Check if file already exists
	if exists, err := s.Exists(hash); err != nil {
		return nil, err
	} else if exists {
		log.Info().Str("hash", hash).Msg("File already exists")
		return nil, store.FileExistsError{Hash: hash}
	}

	// Upload the file using mounted loop file
	err := s.withMountedLoop(hash, func() error {
		return s.saveFileToLoopFromPath(hash, tempFilePath)
	})

	if err != nil {
		return nil, err
	}

	log.Info().Str("hash", hash).Str("filename", filename).Msg("File uploaded successfully")
	return &store.UploadResult{Hash: hash}, nil
}

// saveFileToLoopFromPath saves a file from the given path to the loop filesystem.
func (s *Store) saveFileToLoopFromPath(hash string, sourcePath string) error {
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

	//nolint:gosec // sourcePath comes from validated temp file, not user input
	src, err := os.Open(sourcePath)
	if err != nil {
		log.Error().Err(err).Str("source_path", sourcePath).Msg("Failed to open source file")
		return err
	}
	defer func() {
		if err := src.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close source file")
		}
	}()

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

	if _, err := io.Copy(dst, src); err != nil {
		if err := os.Remove(targetPath); err != nil {
			log.Error().Err(err).Str("target_path", targetPath).Msg("Failed to remove target file after copy error")
		}
		log.Error().Err(err).Str("target_path", targetPath).Msg("Failed to save file")
		return err
	}

	return nil
}

// cleanupTempFile closes and removes the temporary file.
func (s *Store) cleanupTempFile(tempFile *os.File) {
	if tempFile == nil {
		return
	}

	if err := tempFile.Close(); err != nil {
		log.Error().Err(err).Msg("Failed to close temporary file")
	}

	if err := os.Remove(tempFile.Name()); err != nil {
		log.Error().Err(err).Str("temp_file", tempFile.Name()).Msg("Failed to remove temporary file")
	}
}
