package loop

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"loopfs/pkg/store"
)

// DownloadTestSuite tests the Download and DownloadStream functionality
type DownloadTestSuite struct {
	suite.Suite
	tempDir  string
	store    *Store
	testHash string
}

// SetupSuite runs once before all tests
func (s *DownloadTestSuite) SetupSuite() {
	var err error
	s.tempDir, err = os.MkdirTemp("", "download-test-*")
	s.Require().NoError(err)

	// Valid SHA256 hash for testing
	s.testHash = "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"
}

// TearDownSuite runs once after all tests
func (s *DownloadTestSuite) TearDownSuite() {
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

// SetupTest runs before each test
func (s *DownloadTestSuite) SetupTest() {
	s.store = NewWithDefaults(s.tempDir, 10) // 10MB loop files
}

// TearDownTest runs after each test
func (s *DownloadTestSuite) TearDownTest() {
	// Clean up test directory for next test
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
		os.MkdirAll(s.tempDir, 0755)
	}
}

// TestDownloadStreamInvalidHash tests DownloadStream with invalid hash
func (s *DownloadTestSuite) TestDownloadStreamInvalidHash() {
	reader, err := s.store.DownloadStream("invalid")
	s.Error(err)
	s.Nil(reader)
	s.IsType(store.InvalidHashError{}, err)
}

// TestDownloadStreamNoLoopFile tests DownloadStream when loop file doesn't exist
func (s *DownloadTestSuite) TestDownloadStreamNoLoopFile() {
	reader, err := s.store.DownloadStream(s.testHash)
	s.Error(err)
	s.Nil(reader)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestDownloadStreamCaseInsensitive tests DownloadStream with uppercase hash
func (s *DownloadTestSuite) TestDownloadStreamCaseInsensitive() {
	upperHash := "A1B2C3D4E5F67890123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0"

	reader, err := s.store.DownloadStream(upperHash)
	s.Error(err)
	s.Nil(reader)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestValidateHash tests the ValidateHash function
func (s *DownloadTestSuite) TestValidateHash() {
	// Test invalid hash
	result := s.store.ValidateHash("invalid")
	s.False(result)

	// Test valid hash
	result = s.store.ValidateHash(s.testHash)
	s.True(result)
}

// TestDownloadStreamWithLoopFileButNoTargetFile tests stream when loop exists but target doesn't
func (s *DownloadTestSuite) TestDownloadStreamWithLoopFileButNoTargetFile() {
	// Create loop directory and file
	loopDir := filepath.Join(s.tempDir, s.testHash[:2], s.testHash[2:4])
	err := os.MkdirAll(loopDir, 0755)
	s.NoError(err)

	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte("fake loop file"), 0644)
	s.NoError(err)

	// DownloadStream should fail because file doesn't exist in loop
	reader, err := s.store.DownloadStream(s.testHash)
	s.Error(err)
	s.Nil(reader)
	s.T().Logf("DownloadStream failed as expected: %v", err)
}

// TestStreamingReader tests the streamingReader implementation
func (s *DownloadTestSuite) TestStreamingReader() {
	// Create a test file for the streaming reader
	testFile := filepath.Join(s.tempDir, "test_stream.txt")
	testContent := "streaming test content"
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	s.NoError(err)

	// Open the file
	file, err := os.Open(testFile)
	s.NoError(err)

	// Create a streaming reader
	sr := &streamingReader{
		file:       file,
		store:      s.store,
		hash:       s.testHash,
		mountPoint: "/test/mount",
	}

	// Test reading
	buffer := make([]byte, len(testContent))
	n, err := sr.Read(buffer)
	s.NoError(err)
	s.Equal(len(testContent), n)
	s.Equal(testContent, string(buffer))

	// Test closing
	err = sr.Close()
	s.NoError(err)
}

// TestStreamingReaderMultipleReads tests streaming reader with multiple reads
func (s *DownloadTestSuite) TestStreamingReaderMultipleReads() {
	testContent := "This is a longer test content for multiple reads"
	testFile := filepath.Join(s.tempDir, "multi_read.txt")
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	s.NoError(err)

	file, err := os.Open(testFile)
	s.NoError(err)

	sr := &streamingReader{
		file:       file,
		store:      s.store,
		hash:       s.testHash,
		mountPoint: "/test/mount",
	}

	// Read in chunks
	var result strings.Builder
	buffer := make([]byte, 10)
	for {
		n, err := sr.Read(buffer)
		if err == io.EOF {
			break
		}
		s.NoError(err)
		result.Write(buffer[:n])
	}

	s.Equal(testContent, result.String())
	sr.Close()
}

// TestStreamingReaderCloseWithoutFile tests closing when file is nil
func (s *DownloadTestSuite) TestStreamingReaderCloseWithoutFile() {
	sr := &streamingReader{
		file:       nil, // No file
		store:      s.store,
		hash:       s.testHash,
		mountPoint: "/test/mount",
	}

	// Should not panic when closing with nil file
	err := sr.Close()
	s.NoError(err) // Should handle nil file gracefully
}

// TestEnsureLoopFileExistsUnlocked tests ensureLoopFileExistsUnlocked helper function
func (s *DownloadTestSuite) TestEnsureLoopFileExistsUnlocked() {
	// Test with non-existent loop file - will try to create it
	err := s.store.ensureLoopFileExistsUnlocked(s.testHash)
	// May succeed or fail depending on test environment
	if err != nil {
		s.T().Logf("ensureLoopFileExistsUnlocked failed as expected: %v", err)
	}
}

// TestPrepareMountForStreaming tests prepareMountForStreaming helper
func (s *DownloadTestSuite) TestPrepareMountForStreaming() {
	mountPoint := s.store.getMountPoint(s.testHash)

	// This will likely fail in test environment due to mount issues
	err := s.store.prepareMountForStreaming(s.testHash, mountPoint)
	s.Error(err) // Expected to fail in test environment
	s.T().Logf("prepareMountForStreaming failed as expected: %v", err)
}

// TestCleanupAfterErrorWithLock tests cleanupAfterErrorWithLock helper
func (s *DownloadTestSuite) TestCleanupAfterErrorWithLock() {
	mountPoint := s.store.getMountPoint(s.testHash)
	loopFilePath := s.store.getLoopFilePath(s.testHash)
	resizeLock := s.store.getResizeLock(loopFilePath)

	// Acquire the lock before cleanup
	resizeLock.RLock()
	// This should not panic or cause issues and will release the lock
	s.store.cleanupAfterErrorWithLock(mountPoint, resizeLock)
	// No assertions needed - just verify it doesn't panic
}

// TestOpenStreamingReaderWithLock tests the openStreamingReaderWithLock method
func (s *DownloadTestSuite) TestOpenStreamingReaderWithLock() {
	loopFilePath := s.store.getLoopFilePath(s.testHash)
	resizeLock := s.store.getResizeLock(loopFilePath)

	// Acquire the lock as the function expects it to be held
	resizeLock.RLock()
	// Test with invalid hash (should return error and release the lock)
	reader, err := s.store.openStreamingReaderWithLock("invalidhash", "/tmp/mount", resizeLock)
	s.Error(err)
	s.Nil(reader)
	// Lock should have been released by cleanupAfterErrorWithLock

	// Acquire the lock again for the next test
	resizeLock.RLock()
	// Test with valid hash but no loop file (should return error and release the lock)
	reader, err = s.store.openStreamingReaderWithLock(s.testHash, "/tmp/mount", resizeLock)
	s.Error(err)
	s.Nil(reader)
	// Lock should have been released by cleanupAfterErrorWithLock

	// The method expects to find a file in a mounted loop, which won't exist in test environment
	// This still exercises the method path and error handling
}

// TestDownloadSuite runs the download test suite
func TestDownloadSuite(t *testing.T) {
	suite.Run(t, new(DownloadTestSuite))
}
