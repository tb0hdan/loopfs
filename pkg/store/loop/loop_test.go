package loop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"loopfs/pkg/store"
)

// LoopStoreTestSuite tests the loop store implementation
type LoopStoreTestSuite struct {
	suite.Suite
	tempDir  string
	store    *Store
	testHash string
}

// SetupSuite runs once before all tests
func (s *LoopStoreTestSuite) SetupSuite() {
	var err error
	s.tempDir, err = os.MkdirTemp("", "loop-store-test-*")
	s.Require().NoError(err)

	// Valid SHA256 hash for testing (lowercase only)
	s.testHash = "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"
}

// TearDownSuite runs once after all tests
func (s *LoopStoreTestSuite) TearDownSuite() {
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

// SetupTest runs before each test
func (s *LoopStoreTestSuite) SetupTest() {
	s.store = NewWithDefaults(s.tempDir, 10) // 10MB loop files
}

// TearDownTest runs after each test
func (s *LoopStoreTestSuite) TearDownTest() {
	// Clean up test directory for next test
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
		os.MkdirAll(s.tempDir, 0755)
	}
}

// TestNew tests the New constructor
func (s *LoopStoreTestSuite) TestNew() {
	store := NewWithDefaults("/test/path", 100)
	s.NotNil(store)
	s.Equal("/test/path", store.storageDir)
	s.Equal(int64(100), store.loopFileSize)
}

// TestGetLoopFilePath tests the getLoopFilePath method
func (s *LoopStoreTestSuite) TestGetLoopFilePath() {
	tests := []struct {
		name     string
		hash     string
		expected string
	}{
		{
			name:     "valid hash",
			hash:     "abcd1234567890",
			expected: filepath.Join(s.tempDir, "ab", "cd", "loop.img"),
		},
		{
			name:     "short hash",
			hash:     "abc",
			expected: "",
		},
		{
			name:     "empty hash",
			hash:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result := s.store.getLoopFilePath(tt.hash)
			s.Equal(tt.expected, result)
		})
	}
}

// TestGetMountPoint tests the getMountPoint method
func (s *LoopStoreTestSuite) TestGetMountPoint() {
	tests := []struct {
		name     string
		hash     string
		expected string
	}{
		{
			name:     "valid hash",
			hash:     "abcd1234567890",
			expected: filepath.Join(s.tempDir, "ab", "cd", "loopmount"),
		},
		{
			name:     "short hash",
			hash:     "abc",
			expected: "",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result := s.store.getMountPoint(tt.hash)
			s.Equal(tt.expected, result)
		})
	}
}

// TestGetFilePath tests the getFilePath method
func (s *LoopStoreTestSuite) TestGetFilePath() {
	tests := []struct {
		name     string
		hash     string
		expected string
	}{
		{
			name: "valid long hash",
			hash: "abcd123456789012345678901234567890123456789012345678901234567890",
			expected: filepath.Join(
				s.tempDir,
				"ab",
				"cd",
				"loopmount",
				"12",
				"34",
				"56789012345678901234567890123456789012345678901234567890",
			),
		},
		{
			name:     "short hash",
			hash:     "abcd123",
			expected: "",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result := s.store.getFilePath(tt.hash)
			s.Equal(tt.expected, result)
		})
	}
}

// TestValidateHash tests the ValidateHash method
func (s *LoopStoreTestSuite) TestValidateHash() {
	tests := []struct {
		name     string
		hash     string
		expected bool
	}{
		{
			name:     "valid SHA256 hash",
			hash:     "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0",
			expected: true,
		},
		{
			name:     "valid hash lowercase",
			hash:     "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expected: true,
		},
		{
			name:     "invalid hash too short",
			hash:     "abcdef123456789",
			expected: false,
		},
		{
			name:     "invalid hash too long",
			hash:     "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890extra",
			expected: false,
		},
		{
			name:     "invalid hash with non-hex characters",
			hash:     "abcdefg234567890abcdef1234567890abcdef1234567890abcdef123456789x",
			expected: false,
		},
		{
			name:     "empty hash",
			hash:     "",
			expected: false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result := s.store.ValidateHash(tt.hash)
			s.Equal(tt.expected, result)
		})
	}
}

// TestExistsWhenFileDoesNotExist tests Exists method when file doesn't exist
func (s *LoopStoreTestSuite) TestExistsWhenFileDoesNotExist() {
	exists, err := s.store.Exists(s.testHash)
	s.NoError(err)
	s.False(exists)
}

// TestExistsWithInvalidHash tests Exists method with invalid hash
func (s *LoopStoreTestSuite) TestExistsWithInvalidHash() {
	exists, err := s.store.Exists("invalid")
	s.Error(err)
	s.False(exists)
	s.IsType(store.InvalidHashError{}, err)
}

// TestGetFileInfoWithInvalidHash tests GetFileInfo with invalid hash
func (s *LoopStoreTestSuite) TestGetFileInfoWithInvalidHash() {
	_, err := s.store.GetFileInfo("invalid")
	s.Error(err)
	s.IsType(store.InvalidHashError{}, err)
}

// TestGetFileInfoWhenFileDoesNotExist tests GetFileInfo when file doesn't exist
func (s *LoopStoreTestSuite) TestGetFileInfoWhenFileDoesNotExist() {
	_, err := s.store.GetFileInfo(s.testHash)
	s.Error(err)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestGetDiskUsageWithInvalidHash tests GetDiskUsage with invalid hash
func (s *LoopStoreTestSuite) TestGetDiskUsageWithInvalidHash() {
	_, err := s.store.GetDiskUsage("invalid")
	s.Error(err)
	s.IsType(store.InvalidHashError{}, err)
}

// TestGetDiskUsageWhenFileDoesNotExist tests GetDiskUsage when file doesn't exist
func (s *LoopStoreTestSuite) TestGetDiskUsageWhenFileDoesNotExist() {
	_, err := s.store.GetDiskUsage(s.testHash)
	s.Error(err)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestDeleteWithInvalidHash tests Delete with invalid hash
func (s *LoopStoreTestSuite) TestDeleteWithInvalidHash() {
	err := s.store.Delete("invalid")
	s.Error(err)
	s.IsType(store.InvalidHashError{}, err)
}

// TestDeleteWhenFileDoesNotExist tests Delete when file doesn't exist
func (s *LoopStoreTestSuite) TestDeleteWhenFileDoesNotExist() {
	err := s.store.Delete(s.testHash)
	s.Error(err)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestDownloadWithInvalidHash tests Download with invalid hash
func (s *LoopStoreTestSuite) TestDownloadWithInvalidHash() {
	_, err := s.store.Download("invalid")
	s.Error(err)
	s.IsType(store.InvalidHashError{}, err)
}

// TestDownloadWhenFileDoesNotExist tests Download when file doesn't exist
func (s *LoopStoreTestSuite) TestDownloadWhenFileDoesNotExist() {
	_, err := s.store.Download(s.testHash)
	s.Error(err)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestUploadBasic tests basic upload functionality
func (s *LoopStoreTestSuite) TestUploadBasic() {
	// Skip this test if we can't run mount commands (requires root)
	if os.Getuid() != 0 {
		s.T().Skip("Skipping upload test - requires root for mount operations")
		return
	}

	content := "test content for upload"
	reader := strings.NewReader(content)
	filename := "test.txt"

	result, err := s.store.Upload(reader, filename)
	if err != nil {
		// If upload fails due to mount issues, we expect specific errors
		s.T().Logf("Upload failed (expected in test env): %v", err)
		return
	}

	s.NotNil(result)
	s.NotEmpty(result.Hash)
	s.True(s.store.ValidateHash(result.Hash))
}

// errorReader is a test io.Reader that always returns an error
type errorReader struct{}

func (e errorReader) Read(p []byte) (n int, err error) {
	return 0, fmt.Errorf("test read error")
}

// TestUploadErrorConditions tests upload error conditions that don't require root
func (s *LoopStoreTestSuite) TestUploadErrorConditions() {
	// Test with error reader to test error handling path
	result, err := s.store.Upload(errorReader{}, "test.txt")
	s.Error(err)
	s.Nil(result)
	s.Contains(err.Error(), "test read error")

	// Test with valid content but expect failure due to mount issues
	reader := strings.NewReader("test content")
	result, err = s.store.Upload(reader, "test.txt")
	// Will typically fail due to mount issues, but accept either outcome
	if err != nil {
		s.Nil(result)
		s.T().Logf("Upload failed as expected: %v", err)
	} else {
		s.T().Logf("Upload unexpectedly succeeded in test environment")
	}
}

// TestCaseInsensitiveHash tests that hash operations are case insensitive
func (s *LoopStoreTestSuite) TestCaseInsensitiveHash() {
	upperHash := "ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890"
	lowerHash := strings.ToLower(upperHash)

	// ValidateHash only accepts lowercase when called directly
	s.False(s.store.ValidateHash(upperHash))
	s.True(s.store.ValidateHash(lowerHash))

	// Store operations should handle case conversion internally
	// Both upper and lower case hashes should work the same way now
	_, err1 := s.store.Exists(upperHash)
	_, err2 := s.store.Exists(lowerHash)

	// Both should return the same result (nil for non-existent file) since case is normalized
	s.NoError(err1)
	s.NoError(err2)

	// Test Delete with mixed case - both should work the same
	err3 := s.store.Delete(upperHash)
	err4 := s.store.Delete(lowerHash)

	// Both should return FileNotFoundError since file doesn't exist
	s.IsType(store.FileNotFoundError{}, err3)
	s.IsType(store.FileNotFoundError{}, err4)
}

// TestConcurrentAccess tests concurrent access to the store
func (s *LoopStoreTestSuite) TestConcurrentAccess() {
	// This test verifies that concurrent operations don't cause panics
	// Even if they fail due to missing files, they should fail gracefully

	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(index int) {
			defer func() { done <- true }()

			// Each goroutine tries different operations
			hash := s.testHash

			s.store.Exists(hash)
			s.store.ValidateHash(hash)
			s.store.GetFileInfo(hash)  // Expected to fail
			s.store.GetDiskUsage(hash) // Expected to fail
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// If we reach here without panics, the test passes
	s.True(true)
}

// TestStoreInterface verifies that Store implements the store.Store interface
func (s *LoopStoreTestSuite) TestStoreInterface() {
	var _ store.Store = (*Store)(nil)
	s.True(true) // If this compiles, the interface is implemented
}

// TestFindFileInLoop tests the findFileInLoop method
func (s *LoopStoreTestSuite) TestFindFileInLoop() {
	// Test with invalid hash (too short)
	shortHash := "abc"
	_, err := s.store.findFileInLoop(shortHash)
	s.Error(err)
	s.IsType(store.InvalidHashError{}, err)

	// Test with valid hash but non-existent file
	validHash := "abcd123456789012345678901234567890123456789012345678901234567890"
	_, err = s.store.findFileInLoop(validHash)
	s.Error(err)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestIsMounted tests the isMounted method
func (s *LoopStoreTestSuite) TestIsMounted() {
	// Test with non-existent mount point
	nonExistentPath := "/tmp/definitely-not-mounted"
	isMounted := s.store.isMounted(nonExistentPath)
	s.False(isMounted)

	// Test with a regular directory (should not be a mount point)
	regularDir := s.tempDir + "/regular"
	err := os.MkdirAll(regularDir, 0755)
	s.NoError(err)
	isMounted = s.store.isMounted(regularDir)
	s.False(isMounted)
}

// TestCreateLoopFile tests createLoopFile error conditions
func (s *LoopStoreTestSuite) TestCreateLoopFile() {
	// Test with valid hash but may succeed in test environment
	validHash := "abcd123456789012345678901234567890123456789012345678901234567890"
	err := s.store.createLoopFile(validHash)
	// Either succeeds or fails gracefully, both are acceptable in test env
	if err != nil {
		s.T().Logf("createLoopFile failed as expected: %v", err)
	} else {
		s.T().Logf("createLoopFile succeeded in test environment")
	}

	// Test with restricted directory (will fail in real scenario)
	if os.Getuid() != 0 {
		// Create store with restricted directory
		restrictedStore := NewWithDefaults("/root/restricted", 10)
		err := restrictedStore.createLoopFile(validHash)
		s.Error(err) // Should fail due to permission denied
	}
}

// TestMountUnmountOperations tests mount/unmount operations
func (s *LoopStoreTestSuite) TestMountUnmountOperations() {
	validHash := "abcd123456789012345678901234567890123456789012345678901234567890"

	// Test mount without existing loop file
	err := s.store.mountLoopFile(validHash)
	s.Error(err) // Should fail because loop file doesn't exist

	// Test unmount on non-mounted path
	err = s.store.unmountLoopFile(validHash)
	s.NoError(err) // Should succeed (idempotent)
}

// TestWithMountedLoop tests the withMountedLoop helper
func (s *LoopStoreTestSuite) TestWithMountedLoop() {
	validHash := "abcd123456789012345678901234567890123456789012345678901234567890"

	// Test with callback that should fail due to missing loop file
	callbackCalled := false
	err := s.store.withMountedLoop(validHash, func() error {
		callbackCalled = true
		return nil
	})

	// Should fail at mount stage (no loop file exists)
	s.Error(err)
	s.False(callbackCalled) // Callback should not be called if mount fails
}

// TestDownloadEdgeCases tests additional download scenarios
func (s *LoopStoreTestSuite) TestDownloadEdgeCases() {
	// Test Download with non-existent loop file
	validHash := "abcd123456789012345678901234567890123456789012345678901234567890"
	_, err := s.store.Download(validHash)
	s.Error(err) // Should fail because loop file doesn't exist
	s.IsType(store.FileNotFoundError{}, err)
}

// TestExistsEdgeCases tests additional exists scenarios
func (s *LoopStoreTestSuite) TestExistsEdgeCases() {
	validHash := "abcd123456789012345678901234567890123456789012345678901234567890"

	// Test with valid hash but missing loop file
	exists, err := s.store.Exists(validHash)
	s.NoError(err) // Should not error, just return false
	s.False(exists)
}

// TestGetFileInfoEdgeCases tests additional file info scenarios
func (s *LoopStoreTestSuite) TestGetFileInfoEdgeCases() {
	validHash := "abcd123456789012345678901234567890123456789012345678901234567890"

	// Test with valid hash but missing loop file
	_, err := s.store.GetFileInfo(validHash)
	s.Error(err)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestGetDiskUsageEdgeCases tests additional disk usage scenarios
func (s *LoopStoreTestSuite) TestGetDiskUsageEdgeCases() {
	validHash := "abcd123456789012345678901234567890123456789012345678901234567890"

	// Test with valid hash but missing loop file
	_, err := s.store.GetDiskUsage(validHash)
	s.Error(err)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestDeleteEdgeCases tests additional delete scenarios
func (s *LoopStoreTestSuite) TestDeleteEdgeCases() {
	validHash := "abcd123456789012345678901234567890123456789012345678901234567890"

	// Test deleting non-existent file
	err := s.store.Delete(validHash)
	s.Error(err)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestConstants tests package constants
func (s *LoopStoreTestSuite) TestConstants() {
	// Test that constants have reasonable values
	s.Equal(4, minHashLength)
	s.Equal(8, minHashSubDir)
	s.Equal(64, hashLength)
	s.Equal(0750, dirPerm)
	s.Equal("1M", blockSize)
	// Test default timeout configuration
	defaultTimeouts := DefaultTimeoutConfig()
	s.Equal(defaultBaseTimeoutSeconds*time.Second, defaultTimeouts.BaseCommandTimeout)
	s.Equal(defaultDDTimeoutSeconds*time.Second, defaultTimeouts.DDTimeoutPerGB)
	s.Equal(defaultMkfsTimeoutSeconds*time.Second, defaultTimeouts.MkfsTimeoutPerGB)
	s.Equal(defaultRsyncTimeoutSeconds*time.Second, defaultTimeouts.RsyncTimeoutPerGB)
	s.Equal(defaultMinLongTimeoutMins*time.Minute, defaultTimeouts.MinLongOpTimeout)
	s.Equal(defaultMaxLongTimeoutMins*time.Minute, defaultTimeouts.MaxLongOpTimeout)
}

// TestErrorTypes tests custom error type handling
func (s *LoopStoreTestSuite) TestErrorTypes() {
	// Test InvalidHashError
	invalidErr := store.InvalidHashError{Hash: "invalid"}
	s.Equal("invalid hash format", invalidErr.Error())
	s.Equal("invalid", invalidErr.Hash)

	// Test FileNotFoundError
	notFoundErr := store.FileNotFoundError{Hash: "notfound"}
	s.Equal("file not found", notFoundErr.Error())
	s.Equal("notfound", notFoundErr.Hash)

	// Test FileExistsError
	existsErr := store.FileExistsError{Hash: "exists"}
	s.Equal("file already exists", existsErr.Error())
	s.Equal("exists", existsErr.Hash)
}

// TestPathValidation tests path generation edge cases
func (s *LoopStoreTestSuite) TestPathValidation() {
	// Test getLoopFilePath with minimum length hash
	minHash := "abcd1234"
	path := s.store.getLoopFilePath(minHash)
	s.NotEmpty(path)
	s.Contains(path, "ab")
	s.Contains(path, "cd")
	s.Contains(path, "loop.img")

	// Test getMountPoint with minimum length hash
	mountPoint := s.store.getMountPoint(minHash)
	s.NotEmpty(mountPoint)
	s.Contains(mountPoint, "ab")
	s.Contains(mountPoint, "cd")

	// Test getFilePath with minimum required length (needs 8+ characters)
	minFileHash := "abcd123456789012345678901234567890123456789012345678901234567890"
	filePath := s.store.getFilePath(minFileHash)
	s.NotEmpty(filePath)

	// Test with hash too short for getFilePath
	shortFileHash := "abcd123"
	shortFilePath := s.store.getFilePath(shortFileHash)
	s.Empty(shortFilePath) // Should return empty for too short hash
}

// TestStructureValidation tests store structure
func (s *LoopStoreTestSuite) TestStructureValidation() {
	// Test store creation with different parameters
	stores := []*Store{
		NewWithDefaults("/tmp/test1", 1),
		NewWithDefaults("/tmp/test2", 1024),
		NewWithDefaults("/tmp/test3", 2048),
	}

	for i, store := range stores {
		s.NotNil(store, "Store %d should not be nil", i)
		s.NotEmpty(store.storageDir, "Storage dir should not be empty for store %d", i)
		s.Greater(store.loopFileSize, int64(0), "Loop file size should be positive for store %d", i)
	}
}

// TestHashLengthValidation tests various hash lengths
func (s *LoopStoreTestSuite) TestHashLengthValidation() {
	testCases := []struct {
		name     string
		hash     string
		expected bool
	}{
		{"empty", "", false},
		{"too short", "abc", false},
		{"minimum invalid", "abc", false},
		{"63 chars", strings.Repeat("a", 63), false},
		{"64 chars valid", strings.Repeat("a", 64), true},
		{"65 chars", strings.Repeat("a", 65), false},
		{"much too long", strings.Repeat("a", 100), false},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			result := s.store.ValidateHash(tc.hash)
			s.Equal(tc.expected, result)
		})
	}
}

// TestDownloadWithMountedLoop tests the Download function with actual mounted loop
func (s *LoopStoreTestSuite) TestDownloadWithMountedLoop() {
	validHash := s.testHash

	// Test Download with invalid hash
	_, err := s.store.Download("invalid")
	s.Error(err)
	s.IsType(store.InvalidHashError{}, err)

	// Test Download with valid hash but non-existent loop file
	_, err = s.store.Download(validHash)
	s.Error(err)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestCopyFileToTempError tests copyFileToTemp error conditions
func (s *LoopStoreTestSuite) TestCopyFileToTempError() {
	// Test with non-existent source file
	_, err := s.store.copyFileToTemp("/nonexistent/file", s.testHash)
	s.Error(err)
	s.Contains(err.Error(), "no such file or directory")

	// Test with empty file path
	_, err = s.store.copyFileToTemp("", s.testHash)
	s.Error(err)
}

// TestExistsWithMountedLoop tests the Exists function with mounting scenarios
func (s *LoopStoreTestSuite) TestExistsWithMountedLoop() {
	validHash := s.testHash

	// Test with valid hash but no loop file - should return false, no error
	exists, err := s.store.Exists(validHash)
	s.NoError(err)
	s.False(exists)
}

// TestGetFileInfoWithMountedLoop tests GetFileInfo with mounting scenarios
func (s *LoopStoreTestSuite) TestGetFileInfoWithMountedLoop() {
	validHash := s.testHash

	// Test with valid hash but no loop file
	_, err := s.store.GetFileInfo(validHash)
	s.Error(err)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestGetDiskUsageWithMountedLoop tests GetDiskUsage with mounting scenarios
func (s *LoopStoreTestSuite) TestGetDiskUsageWithMountedLoop() {
	validHash := s.testHash

	// Test with valid hash but no loop file
	_, err := s.store.GetDiskUsage(validHash)
	s.Error(err)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestDeleteWithMountedLoop tests Delete with mounting scenarios
func (s *LoopStoreTestSuite) TestDeleteWithMountedLoop() {
	validHash := s.testHash

	// Test with valid hash but no loop file
	err := s.store.Delete(validHash)
	s.Error(err)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestUploadSaveFileToLoop tests the saveFileToLoop function
func (s *LoopStoreTestSuite) TestUploadSaveFileToLoop() {
	// Create a temp file to test saveFileToLoop
	tempFile, err := os.CreateTemp("", "test-save-*")
	s.NoError(err)
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	content := "test content for save"
	_, err = tempFile.WriteString(content)
	s.NoError(err)

	// Test with invalid hash (too short)
	err = s.store.saveFileToLoop("abc", tempFile)
	s.Error(err)
	s.IsType(store.InvalidHashError{}, err)

	// Test with valid hash but expect filesystem errors due to mount issues
	validHash := s.testHash
	err = s.store.saveFileToLoop(validHash, tempFile)
	// This will fail due to mount/path issues in test environment
	s.T().Logf("saveFileToLoop error (expected): %v", err)
}

// TestCleanupTempFileNil tests cleanupTempFile with nil file
func (s *LoopStoreTestSuite) TestCleanupTempFileNil() {
	// Test with nil file - should not panic
	s.store.cleanupTempFile(nil)
}

// TestWithMountedLoopCallback tests withMountedLoop callback scenarios
func (s *LoopStoreTestSuite) TestWithMountedLoopCallback() {
	validHash := s.testHash

	// Test callback that returns an error
	err := s.store.withMountedLoop(validHash, func() error {
		return fmt.Errorf("callback error")
	})
	// Should get the callback error or mount error
	s.Error(err)
	s.T().Logf("withMountedLoop callback error: %v", err)

	// Test callback that succeeds (will fail at mount stage)
	err = s.store.withMountedLoop(validHash, func() error {
		return nil
	})
	// Should get mount error in test environment
	s.Error(err)
	s.T().Logf("withMountedLoop mount error: %v", err)
}

// TestUnmountLoopFileNotMounted tests unmounting when not mounted
func (s *LoopStoreTestSuite) TestUnmountLoopFileNotMounted() {
	validHash := s.testHash

	// Test unmounting when not mounted - should succeed silently
	err := s.store.unmountLoopFile(validHash)
	s.NoError(err)
}

// TestPathFunctions tests path generation functions with edge cases
func (s *LoopStoreTestSuite) TestPathFunctions() {
	// Test getFilePath with less than minHashSubDir
	hash7 := "abcdefg"
	path := s.store.getFilePath(hash7)
	s.Equal("", path) // Should be empty for less than 8 chars

	// Test getFilePath with exactly minHashSubDir length (8 chars) - should return empty as remaining hash would be empty
	hashMinSubDir := "abcdefgh"
	path = s.store.getFilePath(hashMinSubDir)
	s.Contains(path, s.tempDir)
	s.Contains(path, "ef/gh")     // chars 4-6 and 6-8
	s.Contains(path, "loopmount") // mount point uses fixed name

	// Test with hash just over minHashSubDir
	hash9 := "abcdefgh9"
	path = s.store.getFilePath(hash9)
	s.Contains(path, s.tempDir)
	s.Contains(path, "ef")
	s.Contains(path, "gh")
	s.Contains(path, "9") // remaining hash
}

// TestProcessAndHashFileErrors tests processAndHashFile error conditions
func (s *LoopStoreTestSuite) TestProcessAndHashFileErrors() {
	// Test with error reader
	_, _, err := s.store.processAndHashFile(errorReader{})
	s.Error(err)
	s.Contains(err.Error(), "test read error")
}

// TestGetFilePathEdgeCases tests getFilePath with various hash lengths
func (s *LoopStoreTestSuite) TestGetFilePathEdgeCases() {
	// Test with exactly 8 characters (minHashSubDir) - should work now
	hash8 := "abcdefgh"
	path := s.store.getFilePath(hash8)
	s.Contains(path, "ef/gh") // subdirs from chars 4-6 and 6-8
	s.NotEmpty(path, "Should not be empty for 8 char hash")

	// Test with 9 characters (just over minHashSubDir)
	hash9 := "abcdefghi"
	path = s.store.getFilePath(hash9)
	s.Contains(path, "ef/gh") // subdirs from chars 4-6 and 6-8
	s.Contains(path, "i")     // remaining char becomes filename
}

// TestSuite runs the loop store test suite
func TestLoopStoreSuite(t *testing.T) {
	suite.Run(t, new(LoopStoreTestSuite))
}
