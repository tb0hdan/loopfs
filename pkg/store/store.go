package store

import (
	"io"
	"time"
)

// FileInfo represents metadata about a stored file.
type FileInfo struct {
	Hash      string    `json:"hash"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

// DiskUsage represents disk space information.
type DiskUsage struct {
	SpaceUsed      int64 `json:"space_used"`      // Bytes used
	SpaceAvailable int64 `json:"space_available"` // Bytes available
	TotalSpace     int64 `json:"total_space"`     // Total bytes
}

// UploadResult represents the result of an upload operation.
type UploadResult struct {
	Hash string `json:"hash"`
}

// Store defines the interface for content-addressable storage operations.
type Store interface {
	// Upload stores a file from the given reader and returns its hash.
	// If the file already exists, it returns an error with the existing hash.
	Upload(reader io.Reader, filename string) (*UploadResult, error)

	// Download retrieves a file by its hash and returns the file path.
	// Returns an error if the file doesn't exist or hash is invalid.
	Download(hash string) (string, error)

	// GetFileInfo retrieves metadata about a stored file.
	// Returns an error if the file doesn't exist or hash is invalid.
	GetFileInfo(hash string) (*FileInfo, error)

	// Exists checks if a file with the given hash exists in storage.
	Exists(hash string) (bool, error)

	// ValidateHash checks if a hash string is valid format.
	ValidateHash(hash string) bool

	// Delete removes a file with the given hash from storage.
	// Returns an error if the file doesn't exist or hash is invalid.
	Delete(hash string) error

	// GetDiskUsage returns disk space information for a specific file's loop filesystem.
	// Returns an error if the file doesn't exist or hash is invalid.
	GetDiskUsage(hash string) (*DiskUsage, error)
}

// FileExistsError is returned when trying to upload a file that already exists.
type FileExistsError struct {
	Hash string
}

func (e FileExistsError) Error() string {
	return "file already exists"
}

// FileNotFoundError is returned when trying to access a file that doesn't exist.
type FileNotFoundError struct {
	Hash string
}

func (e FileNotFoundError) Error() string {
	return "file not found"
}

// InvalidHashError is returned when a hash has invalid format.
type InvalidHashError struct {
	Hash string
}

func (e InvalidHashError) Error() string {
	return "invalid hash format"
}
