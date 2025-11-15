package loop

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"loopfs/pkg/log"
	"loopfs/pkg/store"
)

const (
	minHashLength = 4
	hashLength    = 64
	dirPerm       = 0750
)

// Store implements the store.Store interface for Loop CAS storage.
type Store struct {
	storageDir string
}

// New creates a new Loop store with the specified storage directory.
func New(storageDir string) *Store {
	return &Store{
		storageDir: storageDir,
	}
}

// ValidateHash checks if a hash string is valid format.
func (s *Store) ValidateHash(hash string) bool {
	if len(hash) != hashLength {
		return false
	}

	for _, char := range hash {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}

	return true
}

// Exists checks if a file with the given hash exists in storage.
func (s *Store) Exists(hash string) (bool, error) {
	if !s.ValidateHash(hash) {
		return false, store.InvalidHashError{Hash: hash}
	}

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
	targetPath := s.getFilePath(hash)

	if targetPath == "" {
		log.Error().Str("hash", hash).Msg("Invalid hash generated")
		return nil, store.InvalidHashError{Hash: hash}
	}

	// Check if file already exists
	if exists, err := s.Exists(hash); err != nil {
		return nil, err
	} else if exists {
		log.Info().Str("hash", hash).Msg("File already exists")
		return nil, store.FileExistsError{Hash: hash}
	}

	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, dirPerm); err != nil {
		log.Error().Err(err).Str("target_dir", targetDir).Msg("Failed to create storage directory")
		return nil, err
	}

	if _, err := tempFile.Seek(0, 0); err != nil {
		log.Error().Err(err).Msg("Failed to seek to beginning of temporary file")
		return nil, err
	}

	//nolint:gosec // targetPath is constructed from validated hash, not user input
	dst, err := os.Create(targetPath)
	if err != nil {
		log.Error().Err(err).Str("target_path", targetPath).Msg("Failed to create destination file")
		return nil, err
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
		return nil, err
	}

	log.Info().Str("hash", hash).Str("filename", filename).Msg("File uploaded successfully")
	return &store.UploadResult{Hash: hash}, nil
}

// Download retrieves a file by its hash and returns the file path.
func (s *Store) Download(hash string) (string, error) {
	hash = strings.ToLower(hash)
	if !s.ValidateHash(hash) {
		log.Error().Str("hash", hash).Msg("Invalid hash format")
		return "", store.InvalidHashError{Hash: hash}
	}

	filePath := s.getFilePath(hash)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Info().Str("hash", hash).Str("file_path", filePath).Msg("File not found")
		return "", store.FileNotFoundError{Hash: hash}
	}

	log.Info().Str("hash", hash).Str("file_path", filePath).Msg("File found for download")
	return filePath, nil
}

// GetFileInfo retrieves metadata about a stored file.
func (s *Store) GetFileInfo(hash string) (*store.FileInfo, error) {
	hash = strings.ToLower(hash)
	if !s.ValidateHash(hash) {
		log.Error().Str("hash", hash).Msg("Invalid hash format")
		return nil, store.InvalidHashError{Hash: hash}
	}

	filePath := s.getFilePath(hash)
	fileInfo, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		log.Info().Str("hash", hash).Str("file_path", filePath).Msg("File not found")
		return nil, store.FileNotFoundError{Hash: hash}
	}
	if err != nil {
		log.Error().Err(err).Str("file_path", filePath).Msg("Failed to get file info")
		return nil, err
	}

	log.Info().Str("hash", hash).Int64("size", fileInfo.Size()).Msg("File info retrieved")
	return &store.FileInfo{
		Hash:      hash,
		Size:      fileInfo.Size(),
		CreatedAt: fileInfo.ModTime(),
	}, nil
}

// getFilePath returns the file path for a given hash.
func (s *Store) getFilePath(hash string) string {
	if len(hash) < minHashLength {
		return ""
	}
	return filepath.Join(s.storageDir, hash[:2], hash[2:4], hash)
}
