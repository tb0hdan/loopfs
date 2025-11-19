package manager

import (
	"errors"
	"io"
	"os"

	"loopfs/pkg/log"
	"loopfs/pkg/models"
	"loopfs/pkg/store"
)

const (
	// DefaultBufferSize is the default buffer size for block operations (128MB).
	DefaultBufferSize = 128 * 1024 * 1024 // 128 MB in bytes
	// ResizeFactor is the multiplier for resizing blocks.
	ResizeFactor = 2
)

// ResizableStore extends the Store interface with resize capability.
type ResizableStore interface {
	store.Store
	ResizeBlock(hash string, newSize int64) error
}

// Manager manages storage operations with automatic block resizing.
type Manager struct {
	store      ResizableStore
	bufferSize int64 // Buffer size in bytes for block operations
}

// New creates a new Store Manager with the given store and buffer size.
func New(store ResizableStore, bufferSize int64) *Manager {
	if bufferSize <= 0 {
		bufferSize = DefaultBufferSize
	}
	return &Manager{
		store:      store,
		bufferSize: bufferSize,
	}
}

// VerifyBlock verifies that a block has enough space for the incoming file.
// If not enough space, it resizes the block to accommodate the file.
// sourceFile is the path to the file to be uploaded.
// hash is the content hash of the file (if available, otherwise empty string).
func (m *Manager) VerifyBlock(sourceFile string, hash string) error {
	// Get file info to determine size
	fileInfo, err := os.Stat(sourceFile)
	if err != nil {
		log.Error().Err(err).Str("source_file", sourceFile).Msg("Failed to stat source file")
		return err
	}

	fileSize := fileInfo.Size()

	// If hash is empty, we can't check disk usage for a specific block
	// This would be the case for a new upload where we don't know the hash yet
	if hash == "" {
		// For new uploads, we'll handle space verification during the upload process
		log.Debug().Int64("file_size", fileSize).Msg("No hash provided, skipping block verification")
		return nil
	}

	// Get current disk usage for the block
	diskUsage, err := m.store.GetDiskUsage(hash)
	if err != nil {
		// If the block doesn't exist yet, that's okay - it will be created during upload
		var fileNotFoundErr store.FileNotFoundError
		if errors.As(err, &fileNotFoundErr) {
			log.Debug().Str("hash", hash).Msg("Block does not exist yet, will be created during upload")
			return nil
		}
		log.Error().Err(err).Str("hash", hash).Msg("Failed to get disk usage")
		return err
	}

	// Check if there's enough space (file size + buffer)
	requiredSpace := fileSize + m.bufferSize
	if diskUsage.SpaceAvailable < requiredSpace {
		// Calculate new size: original disk size + file size * ResizeFactor + buffer
		newSize := diskUsage.TotalSpace + fileSize*ResizeFactor + m.bufferSize

		log.Debug().
			Str("hash", hash).
			Int64("current_available", diskUsage.SpaceAvailable).
			Int64("required_space", requiredSpace).
			Int64("new_size", newSize).
			Msg("Resizing block to accommodate file")

		// Resize the block
		if err := m.store.ResizeBlock(hash, newSize); err != nil {
			log.Error().Err(err).Str("hash", hash).Int64("new_size", newSize).Msg("Failed to resize block")
			return err
		}

		log.Debug().Str("hash", hash).Int64("new_size", newSize).Msg("Block resized successfully")
	}

	return nil
}

// Upload delegates to the underlying store's Upload method.
func (m *Manager) Upload(reader io.Reader, filename string) (*models.UploadResponse, error) {
	return m.store.Upload(reader, filename)
}

// UploadWithHash delegates to the underlying store's UploadWithHash method.
func (m *Manager) UploadWithHash(tempFilePath, hash, filename string) (*models.UploadResponse, error) {
	return m.store.UploadWithHash(tempFilePath, hash, filename)
}

// DownloadStream delegates to the underlying store's DownloadStream method.
func (m *Manager) DownloadStream(hash string) (io.ReadCloser, error) {
	return m.store.DownloadStream(hash)
}

// GetFileInfo delegates to the underlying store's GetFileInfo method.
func (m *Manager) GetFileInfo(hash string) (*models.FileInfo, error) {
	return m.store.GetFileInfo(hash)
}

// Exists delegates to the underlying store's Exists method.
func (m *Manager) Exists(hash string) (bool, error) {
	return m.store.Exists(hash)
}

// ValidateHash delegates to the underlying store's ValidateHash method.
func (m *Manager) ValidateHash(hash string) bool {
	return m.store.ValidateHash(hash)
}

// Delete delegates to the underlying store's Delete method.
func (m *Manager) Delete(hash string) error {
	return m.store.Delete(hash)
}

// GetDiskUsage delegates to the underlying store's GetDiskUsage method.
func (m *Manager) GetDiskUsage(hash string) (*models.DiskUsage, error) {
	return m.store.GetDiskUsage(hash)
}

// GetStore returns the underlying store instance.
func (m *Manager) GetStore() ResizableStore {
	return m.store
}
