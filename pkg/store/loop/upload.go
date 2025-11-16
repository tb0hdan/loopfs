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

	// Atomic check-and-create with single mount operation to prevent race conditions
	created, err := s.atomicCheckAndCreateWithTempFile(hash, tempFile)
	if err != nil {
		return nil, err
	}

	if !created {
		// File already existed, return conflict
		log.Info().Str("hash", hash).Msg("File already exists")
		return nil, store.FileExistsError{Hash: hash}
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

// atomicCheckAndCreate performs atomic check-and-create operation for deduplication.
// Uses a single mount operation to minimize expensive mount/unmount cycles.
// The createFunc should perform the actual file creation within the mounted filesystem.
func (s *Store) atomicCheckAndCreate(hash string, createFunc func() error) (bool, error) {
	// Get per-hash mutex for atomic check-and-create
	deduplicationMutex := s.getDeduplicationMutex(hash)
	deduplicationMutex.Lock()
	defer func() {
		deduplicationMutex.Unlock()
		s.cleanupDeduplicationMutex(hash)
	}()

	// Pre-check: if loop file doesn't exist, file definitely doesn't exist
	loopFilePath := s.getLoopFilePath(hash)
	if _, err := os.Stat(loopFilePath); os.IsNotExist(err) {
		// Loop file doesn't exist, so we need to create it and the file within it
		return true, s.withMountedLoop(hash, createFunc)
	} else if err != nil {
		return false, err
	}

	// Loop file exists, perform check-and-create within single mount operation
	var created bool
	err := s.withMountedLoop(hash, func() error {
		// Check if file exists within the mounted filesystem
		exists, err := s.existsWithinMountedLoop(hash)
		if err != nil {
			return err
		}
		if exists {
			log.Info().Str("hash", hash).Msg("File already exists (detected after acquiring lock)")
			created = false
			return nil // File exists, will return conflict
		}

		// File doesn't exist, create it within the same mount
		if err := createFunc(); err != nil {
			return err
		}

		created = true
		return nil
	})

	return created, err
}

// atomicCheckAndCreateWithTempFile performs atomic check-and-create operation for deduplication
// using a temp file. Returns true if the file was created, false if it already existed.
func (s *Store) atomicCheckAndCreateWithTempFile(hash string, tempFile *os.File) (bool, error) {
	return s.atomicCheckAndCreate(hash, func() error {
		return s.saveFileWithinMountedLoop(hash, tempFile)
	})
}

// atomicCheckAndCreateWithPath performs atomic check-and-create operation for deduplication
// using a file path. Returns true if the file was created, false if it already existed.
func (s *Store) atomicCheckAndCreateWithPath(hash, sourcePath string) (bool, error) {
	return s.atomicCheckAndCreate(hash, func() error {
		return s.saveFileFromPathWithinMountedLoop(hash, sourcePath)
	})
}


// existsWithinMountedLoop checks if a file exists within an already-mounted loop filesystem.
// This assumes the loop filesystem for the hash is already mounted and does not perform mount operations.
func (s *Store) existsWithinMountedLoop(hash string) (bool, error) {
	filePath := s.getFilePath(hash)
	if filePath == "" {
		return false, store.InvalidHashError{Hash: hash}
	}

	_, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

// saveFileWithinMountedLoop saves a temp file to an already-mounted loop filesystem.
// This assumes the loop filesystem for the hash is already mounted.
func (s *Store) saveFileWithinMountedLoop(hash string, tempFile *os.File) error {
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

// saveFileFromPathWithinMountedLoop saves a file from a given path to an already-mounted loop filesystem.
// This assumes the loop filesystem for the hash is already mounted.
func (s *Store) saveFileFromPathWithinMountedLoop(hash string, sourcePath string) error {
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

// saveFileToLoop saves the temporary file to the loop filesystem.
// This is a wrapper that maintains backward compatibility.
//nolint:unused // Used in tests
func (s *Store) saveFileToLoop(hash string, tempFile *os.File) error {
	return s.saveFileWithinMountedLoop(hash, tempFile)
}

// UploadWithHash stores a file using a pre-calculated hash and temp file path.
// This method is more efficient as it avoids redundant hashing and temp file creation.
func (s *Store) UploadWithHash(tempFilePath, hash, filename string) (*store.UploadResult, error) {
	log.Info().Str("filename", filename).Str("hash", hash).Msg("Processing file upload with pre-calculated hash")

	// Validate the provided hash
	if !s.ValidateHash(hash) {
		return nil, store.InvalidHashError{Hash: hash}
	}

	// Atomic check-and-create with single mount operation to prevent race conditions
	created, err := s.atomicCheckAndCreateWithPath(hash, tempFilePath)

	if err != nil {
		return nil, err
	}

	if !created {
		// File already existed, return conflict
		log.Info().Str("hash", hash).Msg("File already exists")
		return nil, store.FileExistsError{Hash: hash}
	}

	log.Info().Str("hash", hash).Str("filename", filename).Msg("File uploaded successfully")
	return &store.UploadResult{Hash: hash}, nil
}

// saveFileToLoopFromPath saves a file from the given path to the loop filesystem.
// This is a wrapper that maintains backward compatibility.
//nolint:unused // Maintained for backward compatibility
func (s *Store) saveFileToLoopFromPath(hash string, sourcePath string) error {
	return s.saveFileFromPathWithinMountedLoop(hash, sourcePath)
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
