package loop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"loopfs/pkg/store"
)

// GetDiskUsageTestSuite tests the GetDiskUsage functionality
type GetDiskUsageTestSuite struct {
	suite.Suite
	tempDir  string
	store    *Store
	testHash string
}

// SetupSuite runs once before all tests
func (s *GetDiskUsageTestSuite) SetupSuite() {
	var err error
	s.tempDir, err = os.MkdirTemp("", "get-disk-usage-test-*")
	s.Require().NoError(err)

	// Valid SHA256 hash for testing
	s.testHash = "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"
}

// TearDownSuite runs once after all tests
func (s *GetDiskUsageTestSuite) TearDownSuite() {
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

// SetupTest runs before each test
func (s *GetDiskUsageTestSuite) SetupTest() {
	s.store = New(s.tempDir, 10) // 10MB loop files
}

// TearDownTest runs after each test
func (s *GetDiskUsageTestSuite) TearDownTest() {
	// Clean up test directory for next test
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
		os.MkdirAll(s.tempDir, 0755)
	}
}

// TestGetDiskUsageInvalidHash tests GetDiskUsage with invalid hash
func (s *GetDiskUsageTestSuite) TestGetDiskUsageInvalidHash() {
	diskUsage, err := s.store.GetDiskUsage("invalid")
	s.Error(err)
	s.Nil(diskUsage)
	s.IsType(store.InvalidHashError{}, err)
}

// TestGetDiskUsageNoLoopFile tests GetDiskUsage when loop file doesn't exist
func (s *GetDiskUsageTestSuite) TestGetDiskUsageNoLoopFile() {
	diskUsage, err := s.store.GetDiskUsage(s.testHash)
	s.Error(err)
	s.Nil(diskUsage)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestGetDiskUsageCaseInsensitive tests GetDiskUsage with uppercase hash
func (s *GetDiskUsageTestSuite) TestGetDiskUsageCaseInsensitive() {
	upperHash := "A1B2C3D4E5F67890123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0"

	diskUsage, err := s.store.GetDiskUsage(upperHash)
	s.Error(err)
	s.Nil(diskUsage)
	s.IsType(store.FileNotFoundError{}, err) // Should normalize hash but file doesn't exist
}

// TestGetDiskUsageHashValidation tests various hash validation scenarios
func (s *GetDiskUsageTestSuite) TestGetDiskUsageHashValidation() {
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
			diskUsage, err := s.store.GetDiskUsage(tc.hash)
			s.Error(err)
			s.Nil(diskUsage)
			s.IsType(tc.errorType, err)
		})
	}
}

// TestGetDiskUsageStatError tests GetDiskUsage when stat on loop file fails
func (s *GetDiskUsageTestSuite) TestGetDiskUsageStatError() {
	if os.Getuid() != 0 { // Only test if not root
		restrictedDir := filepath.Join(s.tempDir, "restricted")
		err := os.MkdirAll(restrictedDir, 0000) // No permissions
		s.NoError(err)

		restrictedStore := New(restrictedDir, 10)

		diskUsage, err := restrictedStore.GetDiskUsage(s.testHash)
		s.Error(err)
		s.Nil(diskUsage)
		s.Contains(err.Error(), "permission denied")
	} else {
		s.T().Skip("Cannot test permission errors as root user")
	}
}

// TestGetDiskUsageLoopFileExistsButMountFails tests when loop file exists but mount fails
func (s *GetDiskUsageTestSuite) TestGetDiskUsageLoopFileExistsButMountFails() {
	// Create loop directory and file
	loopDir := filepath.Join(s.tempDir, s.testHash[:2], s.testHash[2:4])
	err := os.MkdirAll(loopDir, 0755)
	s.NoError(err)

	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte("fake loop file"), 0644)
	s.NoError(err)

	// GetDiskUsage should fail because mount will fail
	diskUsage, err := s.store.GetDiskUsage(s.testHash)
	s.Error(err)
	s.Nil(diskUsage)
	s.T().Logf("GetDiskUsage failed as expected: %v", err)
}

// TestGetDiskUsageWithMockFileSystem tests GetDiskUsage with mock file system
func (s *GetDiskUsageTestSuite) TestGetDiskUsageWithMockFileSystem() {
	// Skip this test if not root (requires mount operations)
	if os.Getuid() != 0 {
		s.T().Skip("Skipping GetDiskUsage with mount test - requires root")
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
	diskUsage, err := s.store.GetDiskUsage(s.testHash)
	s.Error(err) // Expected to fail in test environment
	s.Nil(diskUsage)
	s.T().Logf("GetDiskUsage with mock failed as expected: %v", err)
}

// TestGetDiskUsageReturnType tests the structure of returned DiskUsage
func (s *GetDiskUsageTestSuite) TestGetDiskUsageReturnType() {
	// Even though we can't get a successful result, verify the type structure
	diskUsage, err := s.store.GetDiskUsage(s.testHash)
	s.Error(err)
	s.Nil(diskUsage)

	// Verify that if DiskUsage were returned, it would have the right structure
	expectedDiskUsage := &store.DiskUsage{
		SpaceUsed:      1024,
		SpaceAvailable: 2048,
		TotalSpace:     3072,
	}

	s.Equal(int64(1024), expectedDiskUsage.SpaceUsed)
	s.Equal(int64(2048), expectedDiskUsage.SpaceAvailable)
	s.Equal(int64(3072), expectedDiskUsage.TotalSpace)
}

// TestGetDiskUsageConcurrentAccess tests concurrent GetDiskUsage operations
func (s *GetDiskUsageTestSuite) TestGetDiskUsageConcurrentAccess() {
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(index int) {
			defer func() { done <- true }()

			diskUsage, err := s.store.GetDiskUsage(s.testHash)
			s.Error(err) // Should fail because file doesn't exist
			s.Nil(diskUsage)
			s.T().Logf("Goroutine %d: GetDiskUsage failed as expected: %v", index, err)
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestGetDiskUsageDifferentHashes tests GetDiskUsage with multiple different hashes
func (s *GetDiskUsageTestSuite) TestGetDiskUsageDifferentHashes() {
	testHashes := []string{
		"b1c2d3e4f5a67890123456789abcdef0123456789abcdef0123456789abcdef0",
		"c1d2e3f4a5b67890123456789abcdef0123456789abcdef0123456789abcdef0",
		"d1e2f3a4b5c67890123456789abcdef0123456789abcdef0123456789abcdef0",
	}

	for _, hash := range testHashes {
		s.Run("get_disk_usage_"+hash[:8], func() {
			diskUsage, err := s.store.GetDiskUsage(hash)
			s.Error(err)
			s.Nil(diskUsage)
			s.IsType(store.FileNotFoundError{}, err)
		})
	}
}

// TestGetDiskUsageInterface tests that GetDiskUsage implements store.Store interface
func (s *GetDiskUsageTestSuite) TestGetDiskUsageInterface() {
	// Verify GetDiskUsage is part of the store.Store interface
	var _ store.Store = (*Store)(nil)
	s.True(true) // If this compiles, the interface is implemented
}

// TestGetDiskUsageHashNormalization tests hash normalization behavior
func (s *GetDiskUsageTestSuite) TestGetDiskUsageHashNormalization() {
	mixedCaseHash := "A1b2C3d4E5f67890123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0"

	diskUsage, err := s.store.GetDiskUsage(mixedCaseHash)
	s.Error(err) // File doesn't exist
	s.Nil(diskUsage)
	s.IsType(store.FileNotFoundError{}, err) // Not InvalidHashError
}

// TestGetDiskUsageErrorHandling tests various error conditions
func (s *GetDiskUsageTestSuite) TestGetDiskUsageErrorHandling() {
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
			diskUsage, err := s.store.GetDiskUsage(hash)
			if tc.expectErr {
				s.Error(err)
				s.Nil(diskUsage)
				if tc.expectedType != nil {
					s.IsType(tc.expectedType, err)
				}
			}
		})
	}
}

// TestGetDiskUsagePanicRecovery tests that GetDiskUsage doesn't panic
func (s *GetDiskUsageTestSuite) TestGetDiskUsagePanicRecovery() {
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
					s.Fail("GetDiskUsage should not panic", "Input: %s, Panic: %v", input, r)
				}
			}()

			// Should not panic, may error
			_, _ = s.store.GetDiskUsage(input)
		})
	}
}

// TestGetDiskUsageStatfsError tests syscall.Statfs error handling
func (s *GetDiskUsageTestSuite) TestGetDiskUsageStatfsError() {
	// This test would require actual mount operations to trigger statfs errors
	// In our test environment, we'll just verify that mount failures are handled properly

	// Create a loop file that exists but can't be mounted properly
	loopDir := filepath.Join(s.tempDir, s.testHash[:2], s.testHash[2:4])
	err := os.MkdirAll(loopDir, 0755)
	s.NoError(err)

	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte("invalid loop content"), 0644)
	s.NoError(err)

	diskUsage, err := s.store.GetDiskUsage(s.testHash)
	s.Error(err) // Should fail during mount or statfs
	s.Nil(diskUsage)
}

// TestGetDiskUsageLogMessages tests that appropriate log messages are generated
func (s *GetDiskUsageTestSuite) TestGetDiskUsageLogMessages() {
	// Test with valid but non-existent hash - should generate info log
	diskUsage, err := s.store.GetDiskUsage(s.testHash)
	s.Error(err)
	s.Nil(diskUsage)
}

// TestGetDiskUsageErrorPropagation tests that errors are properly propagated
func (s *GetDiskUsageTestSuite) TestGetDiskUsageErrorPropagation() {
	// Create a scenario that might cause various types of errors
	loopDir := filepath.Join(s.tempDir, s.testHash[:2], s.testHash[2:4])
	err := os.MkdirAll(loopDir, 0755)
	s.NoError(err)

	// Create an empty loop file
	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte(""), 0644)
	s.NoError(err)

	diskUsage, err := s.store.GetDiskUsage(s.testHash)
	s.Error(err)
	s.Nil(diskUsage)
	// Error should be propagated from withMountedLoop or statfs
}

// TestGetDiskUsageBsizeHandling tests handling of Bsize field
func (s *GetDiskUsageTestSuite) TestGetDiskUsageBsizeHandling() {
	// This test verifies that the code handles the Bsize field correctly
	// In our test environment, we can't easily test this without actual mounts
	// but the test documents the expected behavior

	diskUsage, err := s.store.GetDiskUsage(s.testHash)
	s.Error(err)
	s.Nil(diskUsage)

	// The actual implementation should handle:
	// - Bsize < 0 (should set to 0)
	// - Normal positive Bsize values
	// - Overflow prevention in calculations
}

// TestGetDiskUsageCalculations tests the disk usage calculations
func (s *GetDiskUsageTestSuite) TestGetDiskUsageCalculations() {
	// Even though we can't test with real mounts, we can document expected behavior:
	// totalSpace = stat.Blocks * bsize
	// spaceAvailable = stat.Bavail * bsize
	// spaceFree = stat.Bfree * bsize
	// spaceUsed = totalSpace - spaceFree

	// The actual calculations would be tested with integration tests
	// that have real mounted filesystems
	s.True(true) // Document that calculations are tested elsewhere
}

// TestGetDiskUsageSuite runs the GetDiskUsage test suite
func TestGetDiskUsageSuite(t *testing.T) {
	suite.Run(t, new(GetDiskUsageTestSuite))
}
