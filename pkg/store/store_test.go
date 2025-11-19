package store

import (
	"testing"
	"time"

	"loopfs/pkg/models"

	"github.com/stretchr/testify/suite"
)

// StoreTestSuite tests the store package types and errors
type StoreTestSuite struct {
	suite.Suite
}

// SetupTest is called before each test method
func (s *StoreTestSuite) SetupTest() {
	// Setup code before each test
}

// TearDownTest is called after each test method
func (s *StoreTestSuite) TearDownTest() {
	// Cleanup code after each test
}

// TestFileInfo tests the FileInfo struct
func (s *StoreTestSuite) TestFileInfo() {
	now := time.Now()
	fileInfo := &models.FileInfo{
		Hash:      "abcd1234567890",
		Size:      1024,
		CreatedAt: now,
	}

	s.Equal("abcd1234567890", fileInfo.Hash)
	s.Equal(int64(1024), fileInfo.Size)
	s.Equal(now, fileInfo.CreatedAt)
}

// Testmodels.DiskUsage tests the models.DiskUsage struct
func (s *StoreTestSuite) TestDiskUsage() {
	usage := &models.DiskUsage{
		SpaceUsed:      1024 * 1024,
		SpaceAvailable: 10 * 1024 * 1024,
		TotalSpace:     11 * 1024 * 1024,
	}

	s.Equal(int64(1024*1024), usage.SpaceUsed)
	s.Equal(int64(10*1024*1024), usage.SpaceAvailable)
	s.Equal(int64(11*1024*1024), usage.TotalSpace)
}

// Testmodels.UploadResponse tests the models.UploadResponse struct
func (s *StoreTestSuite) TestUploadResponse() {
	result := &models.UploadResponse{
		Hash: "abcd1234567890",
	}

	s.Equal("abcd1234567890", result.Hash)
}

// TestFileExistsError tests the FileExistsError type
func (s *StoreTestSuite) TestFileExistsError() {
	err := FileExistsError{Hash: "abcd1234"}
	s.Equal("file already exists", err.Error())
	s.Equal("abcd1234", err.Hash)
}

// TestFileNotFoundError tests the FileNotFoundError type
func (s *StoreTestSuite) TestFileNotFoundError() {
	err := FileNotFoundError{Hash: "abcd1234"}
	s.Equal("file not found", err.Error())
	s.Equal("abcd1234", err.Hash)
}

// TestInvalidHashError tests the InvalidHashError type
func (s *StoreTestSuite) TestInvalidHashError() {
	err := InvalidHashError{Hash: "invalid"}
	s.Equal("invalid hash format", err.Error())
	s.Equal("invalid", err.Hash)
}

// TestSuite runs the store test suite
func TestStoreSuite(t *testing.T) {
	suite.Run(t, new(StoreTestSuite))
}
