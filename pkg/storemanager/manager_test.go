package storemanager

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"loopfs/pkg/models"
	"loopfs/pkg/store"
)

// MockResizableStore is a mock implementation of ResizableStore for testing
type MockResizableStore struct {
	mock.Mock
}

func (m *MockResizableStore) Upload(reader io.Reader, filename string) (*models.UploadResponse, error) {
	args := m.Called(reader, filename)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.UploadResponse), args.Error(1)
}

func (m *MockResizableStore) UploadWithHash(tempFilePath, hash, filename string) (*models.UploadResponse, error) {
	args := m.Called(tempFilePath, hash, filename)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.UploadResponse), args.Error(1)
}

func (m *MockResizableStore) DownloadStream(hash string) (io.ReadCloser, error) {
	args := m.Called(hash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(io.ReadCloser), args.Error(1)
}

func (m *MockResizableStore) GetFileInfo(hash string) (*models.FileInfo, error) {
	args := m.Called(hash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.FileInfo), args.Error(1)
}

func (m *MockResizableStore) Exists(hash string) (bool, error) {
	args := m.Called(hash)
	return args.Bool(0), args.Error(1)
}

func (m *MockResizableStore) ValidateHash(hash string) bool {
	args := m.Called(hash)
	return args.Bool(0)
}

func (m *MockResizableStore) Delete(hash string) error {
	args := m.Called(hash)
	return args.Error(0)
}

func (m *MockResizableStore) GetDiskUsage(hash string) (*models.DiskUsage, error) {
	args := m.Called(hash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.DiskUsage), args.Error(1)
}

func (m *MockResizableStore) ResizeBlock(hash string, newSize int64) error {
	args := m.Called(hash, newSize)
	return args.Error(0)
}

// ManagerTestSuite tests the store manager functionality
type ManagerTestSuite struct {
	suite.Suite
	mockStore *MockResizableStore
	manager   *Manager
	testHash  string
	tempFile  string
}

// SetupSuite runs once before all tests
func (s *ManagerTestSuite) SetupSuite() {
	s.testHash = "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"

	// Create a temporary file for testing
	tempFile, err := os.CreateTemp("", "manager-test-*")
	s.Require().NoError(err)
	s.tempFile = tempFile.Name()

	// Write test content
	_, err = tempFile.WriteString("test content for manager")
	s.Require().NoError(err)
	tempFile.Close()
}

// TearDownSuite runs once after all tests
func (s *ManagerTestSuite) TearDownSuite() {
	if s.tempFile != "" {
		os.Remove(s.tempFile)
	}
}

// SetupTest runs before each test
func (s *ManagerTestSuite) SetupTest() {
	s.mockStore = new(MockResizableStore)
	s.manager = New(s.mockStore, DefaultBufferSize)
}

// TearDownTest runs after each test
func (s *ManagerTestSuite) TearDownTest() {
	s.mockStore.AssertExpectations(s.T())
}

// TestNew tests the New constructor
func (s *ManagerTestSuite) TestNew() {
	// Test with default buffer size
	manager := New(s.mockStore, 0)
	s.NotNil(manager)
	s.Equal(int64(DefaultBufferSize), manager.bufferSize)
	s.Equal(s.mockStore, manager.store)

	// Test with custom buffer size
	customSize := int64(256 * 1024 * 1024)
	manager = New(s.mockStore, customSize)
	s.NotNil(manager)
	s.Equal(customSize, manager.bufferSize)

	// Test with negative buffer size (should use default)
	manager = New(s.mockStore, -1)
	s.NotNil(manager)
	s.Equal(int64(DefaultBufferSize), manager.bufferSize)
}

// TestVerifyBlockNoHash tests VerifyBlock with empty hash
func (s *ManagerTestSuite) TestVerifyBlockNoHash() {
	err := s.manager.VerifyBlock(s.tempFile, "")
	s.NoError(err) // Should succeed and skip verification
}

// TestVerifyBlockFileStatError tests VerifyBlock with file stat error
func (s *ManagerTestSuite) TestVerifyBlockFileStatError() {
	nonExistentFile := "/nonexistent/file.txt"

	err := s.manager.VerifyBlock(nonExistentFile, s.testHash)
	s.Error(err)
	s.Contains(err.Error(), "no such file or directory")
}

// TestVerifyBlockFileNotFound tests VerifyBlock when block doesn't exist
func (s *ManagerTestSuite) TestVerifyBlockFileNotFound() {
	// Mock GetDiskUsage to return FileNotFoundError
	s.mockStore.On("GetDiskUsage", s.testHash).Return(nil, store.FileNotFoundError{Hash: s.testHash})

	err := s.manager.VerifyBlock(s.tempFile, s.testHash)
	s.NoError(err) // Should succeed when block doesn't exist yet
}

// TestVerifyBlockGetDiskUsageError tests VerifyBlock with GetDiskUsage error
func (s *ManagerTestSuite) TestVerifyBlockGetDiskUsageError() {
	// Mock GetDiskUsage to return a different error
	s.mockStore.On("GetDiskUsage", s.testHash).Return(nil, errors.New("disk usage error"))

	err := s.manager.VerifyBlock(s.tempFile, s.testHash)
	s.Error(err)
	s.Contains(err.Error(), "disk usage error")
}

// TestVerifyBlockSufficientSpace tests VerifyBlock with sufficient space
func (s *ManagerTestSuite) TestVerifyBlockSufficientSpace() {
	// Get file size
	fileInfo, err := os.Stat(s.tempFile)
	s.NoError(err)
	fileSize := fileInfo.Size()

	// Mock disk usage with sufficient space
	diskUsage := &models.DiskUsage{
		SpaceUsed:      1024,
		SpaceAvailable: fileSize + DefaultBufferSize + 1000, // More than required
		TotalSpace:     2048 + fileSize + DefaultBufferSize,
	}
	s.mockStore.On("GetDiskUsage", s.testHash).Return(diskUsage, nil)

	err = s.manager.VerifyBlock(s.tempFile, s.testHash)
	s.NoError(err) // Should succeed without resize
}

// TestVerifyBlockInsufficientSpace tests VerifyBlock with insufficient space requiring resize
func (s *ManagerTestSuite) TestVerifyBlockInsufficientSpace() {
	// Get file size
	fileInfo, err := os.Stat(s.tempFile)
	s.NoError(err)
	fileSize := fileInfo.Size()

	// Mock disk usage with insufficient space
	diskUsage := &models.DiskUsage{
		SpaceUsed:      512,
		SpaceAvailable: fileSize + DefaultBufferSize - 100, // Less than required
		TotalSpace:     1024,
	}
	s.mockStore.On("GetDiskUsage", s.testHash).Return(diskUsage, nil)

	// Calculate expected new size
	expectedNewSize := diskUsage.TotalSpace + fileSize*ResizeFactor + DefaultBufferSize
	s.mockStore.On("ResizeBlock", s.testHash, expectedNewSize).Return(nil)

	err = s.manager.VerifyBlock(s.tempFile, s.testHash)
	s.NoError(err) // Should succeed after resize
}

// TestVerifyBlockResizeError tests VerifyBlock when resize fails
func (s *ManagerTestSuite) TestVerifyBlockResizeError() {
	// Get file size
	fileInfo, err := os.Stat(s.tempFile)
	s.NoError(err)
	fileSize := fileInfo.Size()

	// Mock disk usage with insufficient space
	diskUsage := &models.DiskUsage{
		SpaceUsed:      512,
		SpaceAvailable: fileSize + DefaultBufferSize - 100, // Less than required
		TotalSpace:     1024,
	}
	s.mockStore.On("GetDiskUsage", s.testHash).Return(diskUsage, nil)

	// Calculate expected new size and mock resize error
	expectedNewSize := diskUsage.TotalSpace + fileSize*ResizeFactor + DefaultBufferSize
	s.mockStore.On("ResizeBlock", s.testHash, expectedNewSize).Return(errors.New("resize failed"))

	err = s.manager.VerifyBlock(s.tempFile, s.testHash)
	s.Error(err)
	s.Contains(err.Error(), "resize failed")
}

// TestUpload tests Upload delegation
func (s *ManagerTestSuite) TestUpload() {
	reader := strings.NewReader("test content")
	filename := "test.txt"
	expectedResult := &models.UploadResponse{Hash: s.testHash}

	s.mockStore.On("Upload", reader, filename).Return(expectedResult, nil)

	result, err := s.manager.Upload(reader, filename)
	s.NoError(err)
	s.Equal(expectedResult, result)
}

// TestUploadError tests Upload delegation with error
func (s *ManagerTestSuite) TestUploadError() {
	reader := strings.NewReader("test content")
	filename := "test.txt"

	s.mockStore.On("Upload", reader, filename).Return(nil, errors.New("upload error"))

	result, err := s.manager.Upload(reader, filename)
	s.Error(err)
	s.Nil(result)
	s.Contains(err.Error(), "upload error")
}

// TestDownloadStream tests DownloadStream delegation
func (s *ManagerTestSuite) TestDownloadStream() {
	mockReader := &mockReadCloser{content: "test content"}

	s.mockStore.On("DownloadStream", s.testHash).Return(mockReader, nil)

	reader, err := s.manager.DownloadStream(s.testHash)
	s.NoError(err)
	s.Equal(mockReader, reader)
}

// TestDownloadStreamError tests DownloadStream delegation with error
func (s *ManagerTestSuite) TestDownloadStreamError() {
	s.mockStore.On("DownloadStream", s.testHash).Return(nil, errors.New("download stream error"))

	reader, err := s.manager.DownloadStream(s.testHash)
	s.Error(err)
	s.Nil(reader)
	s.Contains(err.Error(), "download stream error")
}

// TestGetFileInfo tests GetFileInfo delegation
func (s *ManagerTestSuite) TestGetFileInfo() {
	expectedFileInfo := &models.FileInfo{
		Hash:      s.testHash,
		Size:      1024,
		CreatedAt: time.Now(),
	}

	s.mockStore.On("GetFileInfo", s.testHash).Return(expectedFileInfo, nil)

	fileInfo, err := s.manager.GetFileInfo(s.testHash)
	s.NoError(err)
	s.Equal(expectedFileInfo, fileInfo)
}

// TestGetFileInfoError tests GetFileInfo delegation with error
func (s *ManagerTestSuite) TestGetFileInfoError() {
	s.mockStore.On("GetFileInfo", s.testHash).Return(nil, errors.New("get file info error"))

	fileInfo, err := s.manager.GetFileInfo(s.testHash)
	s.Error(err)
	s.Nil(fileInfo)
	s.Contains(err.Error(), "get file info error")
}

// TestExists tests Exists delegation
func (s *ManagerTestSuite) TestExists() {
	s.mockStore.On("Exists", s.testHash).Return(true, nil)

	exists, err := s.manager.Exists(s.testHash)
	s.NoError(err)
	s.True(exists)
}

// TestExistsError tests Exists delegation with error
func (s *ManagerTestSuite) TestExistsError() {
	s.mockStore.On("Exists", s.testHash).Return(false, errors.New("exists error"))

	exists, err := s.manager.Exists(s.testHash)
	s.Error(err)
	s.False(exists)
	s.Contains(err.Error(), "exists error")
}

// TestValidateHash tests ValidateHash delegation
func (s *ManagerTestSuite) TestValidateHash() {
	s.mockStore.On("ValidateHash", s.testHash).Return(true)

	valid := s.manager.ValidateHash(s.testHash)
	s.True(valid)
}

// TestValidateHashInvalid tests ValidateHash delegation with invalid hash
func (s *ManagerTestSuite) TestValidateHashInvalid() {
	invalidHash := "invalid"
	s.mockStore.On("ValidateHash", invalidHash).Return(false)

	valid := s.manager.ValidateHash(invalidHash)
	s.False(valid)
}

// TestDelete tests Delete delegation
func (s *ManagerTestSuite) TestDelete() {
	s.mockStore.On("Delete", s.testHash).Return(nil)

	err := s.manager.Delete(s.testHash)
	s.NoError(err)
}

// TestDeleteError tests Delete delegation with error
func (s *ManagerTestSuite) TestDeleteError() {
	s.mockStore.On("Delete", s.testHash).Return(errors.New("delete error"))

	err := s.manager.Delete(s.testHash)
	s.Error(err)
	s.Contains(err.Error(), "delete error")
}

// TestGetDiskUsage tests GetDiskUsage delegation
func (s *ManagerTestSuite) TestGetDiskUsage() {
	expectedDiskUsage := &models.DiskUsage{
		SpaceUsed:      1024,
		SpaceAvailable: 2048,
		TotalSpace:     3072,
	}

	s.mockStore.On("GetDiskUsage", s.testHash).Return(expectedDiskUsage, nil)

	diskUsage, err := s.manager.GetDiskUsage(s.testHash)
	s.NoError(err)
	s.Equal(expectedDiskUsage, diskUsage)
}

// TestGetDiskUsageError tests GetDiskUsage delegation with error
func (s *ManagerTestSuite) TestGetDiskUsageError() {
	s.mockStore.On("GetDiskUsage", s.testHash).Return(nil, errors.New("get disk usage error"))

	diskUsage, err := s.manager.GetDiskUsage(s.testHash)
	s.Error(err)
	s.Nil(diskUsage)
	s.Contains(err.Error(), "get disk usage error")
}

// TestGetStore tests GetStore method
func (s *ManagerTestSuite) TestGetStore() {
	store := s.manager.GetStore()
	s.Equal(s.mockStore, store)
}

// TestConstants tests package constants
func (s *ManagerTestSuite) TestConstants() {
	s.Equal(128*1024*1024, DefaultBufferSize) // 128 MB
	s.Equal(2, ResizeFactor)
}

// TestManagerInterface tests that Manager implements the expected interfaces
func (s *ManagerTestSuite) TestManagerInterface() {
	// Verify Manager implements store.Store interface
	var _ store.Store = (*Manager)(nil)
	s.True(true) // If this compiles, the interface is implemented
}

// TestResizableStoreInterface tests that MockResizableStore implements ResizableStore
func (s *ManagerTestSuite) TestResizableStoreInterface() {
	// Verify MockResizableStore implements ResizableStore interface
	var _ ResizableStore = (*MockResizableStore)(nil)
	s.True(true) // If this compiles, the interface is implemented
}

// TestVerifyBlockCustomBufferSize tests VerifyBlock with custom buffer size
func (s *ManagerTestSuite) TestVerifyBlockCustomBufferSize() {
	customBufferSize := int64(64 * 1024 * 1024) // 64 MB
	customManager := New(s.mockStore, customBufferSize)

	// Get file size
	fileInfo, err := os.Stat(s.tempFile)
	s.NoError(err)
	fileSize := fileInfo.Size()

	// Mock disk usage with insufficient space
	diskUsage := &models.DiskUsage{
		SpaceUsed:      512,
		SpaceAvailable: fileSize + customBufferSize - 100, // Less than required
		TotalSpace:     1024,
	}
	s.mockStore.On("GetDiskUsage", s.testHash).Return(diskUsage, nil)

	// Calculate expected new size with custom buffer
	expectedNewSize := diskUsage.TotalSpace + fileSize*ResizeFactor + customBufferSize
	s.mockStore.On("ResizeBlock", s.testHash, expectedNewSize).Return(nil)

	err = customManager.VerifyBlock(s.tempFile, s.testHash)
	s.NoError(err) // Should succeed after resize
}

// TestVerifyBlockExactSpaceMatch tests VerifyBlock with exact space match
func (s *ManagerTestSuite) TestVerifyBlockExactSpaceMatch() {
	// Get file size
	fileInfo, err := os.Stat(s.tempFile)
	s.NoError(err)
	fileSize := fileInfo.Size()

	// Mock disk usage with exactly the required space
	requiredSpace := fileSize + DefaultBufferSize
	diskUsage := &models.DiskUsage{
		SpaceUsed:      1024,
		SpaceAvailable: requiredSpace, // Exactly what's required
		TotalSpace:     1024 + requiredSpace,
	}
	s.mockStore.On("GetDiskUsage", s.testHash).Return(diskUsage, nil)

	err = s.manager.VerifyBlock(s.tempFile, s.testHash)
	s.NoError(err) // Should succeed without resize
}

// TestVerifyBlockLargeFile tests VerifyBlock with a large file
func (s *ManagerTestSuite) TestVerifyBlockLargeFile() {
	// Create a temporary large file
	largeFile, err := os.CreateTemp("", "large-test-*")
	s.NoError(err)
	defer os.Remove(largeFile.Name())
	defer largeFile.Close()

	// Write larger content
	content := strings.Repeat("x", 10*1024) // 10KB
	_, err = largeFile.WriteString(content)
	s.NoError(err)

	fileSize := int64(len(content))

	// Mock disk usage with insufficient space
	diskUsage := &models.DiskUsage{
		SpaceUsed:      512,
		SpaceAvailable: fileSize / 2, // Much less than required
		TotalSpace:     1024,
	}
	s.mockStore.On("GetDiskUsage", s.testHash).Return(diskUsage, nil)

	// Calculate expected new size
	expectedNewSize := diskUsage.TotalSpace + fileSize*ResizeFactor + DefaultBufferSize
	s.mockStore.On("ResizeBlock", s.testHash, expectedNewSize).Return(nil)

	err = s.manager.VerifyBlock(largeFile.Name(), s.testHash)
	s.NoError(err) // Should succeed after resize
}

// mockReadCloser is a mock implementation of io.ReadCloser for testing
type mockReadCloser struct {
	content string
	pos     int
	closed  bool
}

func (m *mockReadCloser) Read(p []byte) (n int, err error) {
	if m.closed {
		return 0, errors.New("reader closed")
	}
	if m.pos >= len(m.content) {
		return 0, io.EOF
	}
	n = copy(p, m.content[m.pos:])
	m.pos += n
	return n, nil
}

func (m *mockReadCloser) Close() error {
	m.closed = true
	return nil
}

// TestDownloadStreamUsage tests using the DownloadStream reader
func (s *ManagerTestSuite) TestDownloadStreamUsage() {
	content := "test stream content"
	mockReader := &mockReadCloser{content: content}

	s.mockStore.On("DownloadStream", s.testHash).Return(mockReader, nil)

	reader, err := s.manager.DownloadStream(s.testHash)
	s.NoError(err)
	s.NotNil(reader)

	// Read content
	data, err := io.ReadAll(reader)
	s.NoError(err)
	s.Equal(content, string(data))

	// Close reader
	err = reader.Close()
	s.NoError(err)

	// Try to read after close (should fail)
	_, err = reader.Read(make([]byte, 10))
	s.Error(err)
}

// TestVerifyBlockEdgeCases tests various edge cases for VerifyBlock
func (s *ManagerTestSuite) TestVerifyBlockEdgeCases() {
	// Test with zero-byte file
	emptyFile, err := os.CreateTemp("", "empty-test-*")
	s.NoError(err)
	defer os.Remove(emptyFile.Name())
	emptyFile.Close()

	// For empty file, available space should be sufficient even if small
	diskUsage := &models.DiskUsage{
		SpaceUsed:      100,
		SpaceAvailable: DefaultBufferSize + 1000, // Should be sufficient
		TotalSpace:     DefaultBufferSize + 1100,
	}
	s.mockStore.On("GetDiskUsage", s.testHash).Return(diskUsage, nil)

	err = s.manager.VerifyBlock(emptyFile.Name(), s.testHash)
	s.NoError(err)
}

// TestManagerSuite runs the manager test suite
func TestManagerSuite(t *testing.T) {
	suite.Run(t, new(ManagerTestSuite))
}
