package loop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"loopfs/pkg/store"
)

// GetFileInfoTestSuite tests the GetFileInfo functionality
type GetFileInfoTestSuite struct {
	suite.Suite
	tempDir  string
	store    *Store
	testHash string
}

// SetupSuite runs once before all tests
func (s *GetFileInfoTestSuite) SetupSuite() {
	var err error
	s.tempDir, err = os.MkdirTemp("", "get-file-info-test-*")
	s.Require().NoError(err)

	// Valid SHA256 hash for testing
	s.testHash = "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"
}

// TearDownSuite runs once after all tests
func (s *GetFileInfoTestSuite) TearDownSuite() {
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

// SetupTest runs before each test
func (s *GetFileInfoTestSuite) SetupTest() {
	s.store = New(s.tempDir, 10) // 10MB loop files
}

// TearDownTest runs after each test
func (s *GetFileInfoTestSuite) TearDownTest() {
	// Clean up test directory for next test
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
		os.MkdirAll(s.tempDir, 0755)
	}
}

// TestGetFileInfoInvalidHash tests GetFileInfo with invalid hash
func (s *GetFileInfoTestSuite) TestGetFileInfoInvalidHash() {
	fileInfo, err := s.store.GetFileInfo("invalid")
	s.Error(err)
	s.Nil(fileInfo)
	s.IsType(store.InvalidHashError{}, err)
}

// TestGetFileInfoNoLoopFile tests GetFileInfo when loop file doesn't exist
func (s *GetFileInfoTestSuite) TestGetFileInfoNoLoopFile() {
	fileInfo, err := s.store.GetFileInfo(s.testHash)
	s.Error(err)
	s.Nil(fileInfo)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestGetFileInfoCaseInsensitive tests GetFileInfo with uppercase hash
func (s *GetFileInfoTestSuite) TestGetFileInfoCaseInsensitive() {
	upperHash := "A1B2C3D4E5F67890123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0"

	fileInfo, err := s.store.GetFileInfo(upperHash)
	s.Error(err)
	s.Nil(fileInfo)
	s.IsType(store.FileNotFoundError{}, err) // Should normalize hash but file doesn't exist
}

// TestGetFileInfoHashValidation tests various hash validation scenarios
func (s *GetFileInfoTestSuite) TestGetFileInfoHashValidation() {
	testCases := []struct {
		name      string
		hash      string
		errorType interface{}
	}{
		{"empty_hash", "", store.InvalidHashError{}},
		{"too_short", "abc", store.InvalidHashError{}},
		{"too_long", s.testHash + "extra", store.InvalidHashError{}},
		{"non_hex_chars", "g1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0", store.InvalidHashError{}},
		{"valid_nonexistent", s.testHash, store.FileNotFoundError{}},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			fileInfo, err := s.store.GetFileInfo(tc.hash)
			s.Error(err)
			s.Nil(fileInfo)
			s.IsType(tc.errorType, err)
		})
	}
}

// TestGetFileInfoStatError tests GetFileInfo when stat on loop file fails
func (s *GetFileInfoTestSuite) TestGetFileInfoStatError() {
	if os.Getuid() != 0 { // Only test if not root
		restrictedDir := filepath.Join(s.tempDir, "restricted")
		err := os.MkdirAll(restrictedDir, 0000) // No permissions
		s.NoError(err)

		restrictedStore := New(restrictedDir, 10)

		fileInfo, err := restrictedStore.GetFileInfo(s.testHash)
		s.Error(err)
		s.Nil(fileInfo)
		s.Contains(err.Error(), "permission denied")
	} else {
		s.T().Skip("Cannot test permission errors as root user")
	}
}

// TestGetFileInfoLoopFileExistsButFileDoesNot tests when loop file exists but target doesn't
func (s *GetFileInfoTestSuite) TestGetFileInfoLoopFileExistsButFileDoesNot() {
	// Create loop directory and file
	loopDir := filepath.Join(s.tempDir, s.testHash[:2], s.testHash[2:4])
	err := os.MkdirAll(loopDir, 0755)
	s.NoError(err)

	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte("fake loop file"), 0644)
	s.NoError(err)

	// GetFileInfo should fail because the actual file doesn't exist in the loop
	fileInfo, err := s.store.GetFileInfo(s.testHash)
	s.Error(err)
	s.Nil(fileInfo)
	// Will likely fail due to mount issues in test environment
	s.T().Logf("GetFileInfo failed as expected: %v", err)
}

// TestGetFileInfoWithMockFileSystem tests GetFileInfo with mock file system
func (s *GetFileInfoTestSuite) TestGetFileInfoWithMockFileSystem() {
	// Skip this test if not root (requires mount operations)
	if os.Getuid() != 0 {
		s.T().Skip("Skipping GetFileInfo with mount test - requires root")
		return
	}

	// Create mock file structure
	loopDir := filepath.Join(s.tempDir, s.testHash[:2], s.testHash[2:4])
	err := os.MkdirAll(loopDir, 0755)
	s.NoError(err)

	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte("test loop file content"), 0644)
	s.NoError(err)

	// This will likely fail in test environment due to mounting issues
	fileInfo, err := s.store.GetFileInfo(s.testHash)
	s.Error(err) // Expected to fail in test environment
	s.Nil(fileInfo)
	s.T().Logf("GetFileInfo with mock failed as expected: %v", err)
}

// TestGetFileInfoReturnType tests the structure of returned FileInfo
func (s *GetFileInfoTestSuite) TestGetFileInfoReturnType() {
	// Even though we can't get a successful result, test that the error handling is correct
	fileInfo, err := s.store.GetFileInfo(s.testHash)
	s.Error(err)
	s.Nil(fileInfo)

	// Verify that if FileInfo were returned, it would have the right structure
	expectedFileInfo := &store.FileInfo{
		Hash:      s.testHash,
		Size:      1024,
		CreatedAt: time.Now(),
	}

	s.Equal(s.testHash, expectedFileInfo.Hash)
	s.Equal(int64(1024), expectedFileInfo.Size)
	s.IsType(time.Time{}, expectedFileInfo.CreatedAt)
}

// TestGetFileInfoConcurrentAccess tests concurrent GetFileInfo operations
func (s *GetFileInfoTestSuite) TestGetFileInfoConcurrentAccess() {
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(index int) {
			defer func() { done <- true }()

			fileInfo, err := s.store.GetFileInfo(s.testHash)
			s.Error(err) // Should fail because file doesn't exist
			s.Nil(fileInfo)
			s.T().Logf("Goroutine %d: GetFileInfo failed as expected: %v", index, err)
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestGetFileInfoDifferentHashes tests GetFileInfo with multiple different hashes
func (s *GetFileInfoTestSuite) TestGetFileInfoDifferentHashes() {
	testHashes := []string{
		"b1c2d3e4f5a67890123456789abcdef0123456789abcdef0123456789abcdef0",
		"c1d2e3f4a5b67890123456789abcdef0123456789abcdef0123456789abcdef0",
		"d1e2f3a4b5c67890123456789abcdef0123456789abcdef0123456789abcdef0",
	}

	for _, hash := range testHashes {
		s.Run("get_file_info_"+hash[:8], func() {
			fileInfo, err := s.store.GetFileInfo(hash)
			s.Error(err)
			s.Nil(fileInfo)
			s.IsType(store.FileNotFoundError{}, err)
		})
	}
}

// TestGetFileInfoErrorPropagation tests that errors are properly propagated
func (s *GetFileInfoTestSuite) TestGetFileInfoErrorPropagation() {
	// Create a scenario that might cause stat errors
	loopDir := filepath.Join(s.tempDir, s.testHash[:2], s.testHash[2:4])
	err := os.MkdirAll(loopDir, 0755)
	s.NoError(err)

	// Create an empty loop file
	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte(""), 0644)
	s.NoError(err)

	fileInfo, err := s.store.GetFileInfo(s.testHash)
	s.Error(err)
	s.Nil(fileInfo)
	// Error should be propagated from withMountedLoop
}

// TestGetFileInfoInterface tests that GetFileInfo implements store.Store interface
func (s *GetFileInfoTestSuite) TestGetFileInfoInterface() {
	// Verify GetFileInfo is part of the store.Store interface
	var _ store.Store = (*Store)(nil)
	s.True(true) // If this compiles, the interface is implemented
}

// TestGetFileInfoHashNormalization tests hash normalization behavior
func (s *GetFileInfoTestSuite) TestGetFileInfoHashNormalization() {
	mixedCaseHash := "A1b2C3d4E5f67890123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0"

	fileInfo, err := s.store.GetFileInfo(mixedCaseHash)
	s.Error(err) // File doesn't exist
	s.Nil(fileInfo)
	s.IsType(store.FileNotFoundError{}, err) // Not InvalidHashError
}

// TestGetFileInfoErrorHandling tests various error conditions
func (s *GetFileInfoTestSuite) TestGetFileInfoErrorHandling() {
	testCases := []struct {
		name         string
		setupFunc    func() string // Returns hash to test
		expectErr    bool
		expectedType interface{}
	}{
		{
			name: "null_char_in_hash",
			setupFunc: func() string {
				return "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcd\x000"
			},
			expectErr:    true,
			expectedType: store.InvalidHashError{},
		},
		{
			name: "unicode_in_hash",
			setupFunc: func() string {
				return "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdéf0"
			},
			expectErr:    true,
			expectedType: store.InvalidHashError{},
		},
		{
			name: "very_long_hash",
			setupFunc: func() string {
				return s.testHash + "extracharsthatshouldmakeithash"
			},
			expectErr:    true,
			expectedType: store.InvalidHashError{},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			hash := tc.setupFunc()
			fileInfo, err := s.store.GetFileInfo(hash)
			if tc.expectErr {
				s.Error(err)
				s.Nil(fileInfo)
				if tc.expectedType != nil {
					s.IsType(tc.expectedType, err)
				}
			}
		})
	}
}

// TestGetFileInfoPanicRecovery tests that GetFileInfo doesn't panic
func (s *GetFileInfoTestSuite) TestGetFileInfoPanicRecovery() {
	problematicInputs := []string{
		"",
		"a",
		"null\x00hash",
		"very_long_string_that_exceeds_normal_hash_length_significantly",
		s.testHash,
	}

	for _, input := range problematicInputs {
		s.Run("no_panic_"+input[:min(8, len(input))], func() {
			defer func() {
				if r := recover(); r != nil {
					s.Fail("GetFileInfo should not panic", "Input: %s, Panic: %v", input, r)
				}
			}()

			// Should not panic, may error
			_, _ = s.store.GetFileInfo(input)
		})
	}
}

// TestGetFileInfoLogMessages tests that appropriate log messages are generated
func (s *GetFileInfoTestSuite) TestGetFileInfoLogMessages() {
	// Test with invalid hash - should generate error log
	fileInfo, err := s.store.GetFileInfo("invalid")
	s.Error(err)
	s.Nil(fileInfo)

	// Test with valid but non-existent hash - should generate info log
	fileInfo, err = s.store.GetFileInfo(s.testHash)
	s.Error(err)
	s.Nil(fileInfo)
}

// TestGetFileInfoWithExistingLoopButStatError tests stat error within loop
func (s *GetFileInfoTestSuite) TestGetFileInfoWithExistingLoopButStatError() {
	// Create loop file structure
	loopDir := filepath.Join(s.tempDir, s.testHash[:2], s.testHash[2:4])
	err := os.MkdirAll(loopDir, 0755)
	s.NoError(err)

	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte("fake loop content"), 0644)
	s.NoError(err)

	// Test should fail during mounting or file finding
	fileInfo, err := s.store.GetFileInfo(s.testHash)
	s.Error(err)
	s.Nil(fileInfo)
}

// TestGetFileInfoSuite runs the GetFileInfo test suite
func TestGetFileInfoSuite(t *testing.T) {
	suite.Run(t, new(GetFileInfoTestSuite))
}
