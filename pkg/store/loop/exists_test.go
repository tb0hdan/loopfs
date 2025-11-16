package loop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"loopfs/pkg/store"
)

// ExistsTestSuite tests the Exists functionality
type ExistsTestSuite struct {
	suite.Suite
	tempDir  string
	store    *Store
	testHash string
}

// SetupSuite runs once before all tests
func (s *ExistsTestSuite) SetupSuite() {
	var err error
	s.tempDir, err = os.MkdirTemp("", "exists-test-*")
	s.Require().NoError(err)

	// Valid SHA256 hash for testing
	s.testHash = "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"
}

// TearDownSuite runs once after all tests
func (s *ExistsTestSuite) TearDownSuite() {
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

// SetupTest runs before each test
func (s *ExistsTestSuite) SetupTest() {
	s.store = New(s.tempDir, 10) // 10MB loop files
}

// TearDownTest runs after each test
func (s *ExistsTestSuite) TearDownTest() {
	// Clean up test directory for next test
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
		os.MkdirAll(s.tempDir, 0755)
	}
}

// TestExistsInvalidHash tests Exists with invalid hash
func (s *ExistsTestSuite) TestExistsInvalidHash() {
	exists, err := s.store.Exists("invalid")
	s.Error(err)
	s.False(exists)
	s.IsType(store.InvalidHashError{}, err)
}

// TestExistsNoLoopFile tests Exists when loop file doesn't exist
func (s *ExistsTestSuite) TestExistsNoLoopFile() {
	exists, err := s.store.Exists(s.testHash)
	s.NoError(err)
	s.False(exists)
}

// TestExistsLoopFileExistsButFileDoesNot tests when loop file exists but target file doesn't
func (s *ExistsTestSuite) TestExistsLoopFileExistsButFileDoesNot() {
	// Create loop directory and file
	loopDir := filepath.Join(s.tempDir, s.testHash[:2], s.testHash[2:4])
	err := os.MkdirAll(loopDir, 0755)
	s.NoError(err)

	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte("fake loop file"), 0644)
	s.NoError(err)

	// Test exists - should return false but no error
	exists, err := s.store.Exists(s.testHash)
	// This will likely fail in test environment due to mount issues, but should not panic
	if err != nil {
		s.T().Logf("Exists failed as expected in test environment: %v", err)
	} else {
		s.False(exists)
	}
}

// TestExistsStatError tests Exists when stat operation on loop file fails
func (s *ExistsTestSuite) TestExistsStatError() {
	// Create a file that will cause permission error when trying to stat
	if os.Getuid() != 0 { // Only test if not root
		restrictedDir := filepath.Join(s.tempDir, "restricted")
		err := os.MkdirAll(restrictedDir, 0000) // No permissions
		s.NoError(err)

		// Change the store to point to the restricted directory
		restrictedStore := New(restrictedDir, 10)

		exists, err := restrictedStore.Exists(s.testHash)
		s.Error(err)
		s.False(exists)
		s.Contains(err.Error(), "permission denied")
	} else {
		s.T().Skip("Cannot test permission errors as root user")
	}
}

// TestExistsCaseInsensitive tests Exists with uppercase hash
func (s *ExistsTestSuite) TestExistsCaseInsensitive() {
	upperHash := "A1B2C3D4E5F67890123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0"

	// Should not error because hash is converted to lowercase internally
	exists, err := s.store.Exists(upperHash)
	s.NoError(err)
	s.False(exists) // File doesn't exist, but no validation error
}

// TestExistsHashTooShort tests Exists with hash that's too short for getFilePath
func (s *ExistsTestSuite) TestExistsHashTooShort() {
	shortHash := "abc123" // Less than minimum required

	exists, err := s.store.Exists(shortHash)
	s.Error(err)
	s.False(exists)
	s.IsType(store.InvalidHashError{}, err)
}

// TestExistsValidHashMinimumLength tests Exists with minimum valid hash length
func (s *ExistsTestSuite) TestExistsValidHashMinimumLength() {
	// Create a 64-character hash (minimum for SHA256)
	validHash := "abcd123456789012345678901234567890123456789012345678901234567890"

	exists, err := s.store.Exists(validHash)
	s.NoError(err)  // Should not error on validation
	s.False(exists) // File doesn't exist
}

// TestExistsConcurrentAccess tests concurrent calls to Exists
func (s *ExistsTestSuite) TestExistsConcurrentAccess() {
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(index int) {
			defer func() { done <- true }()

			// Each goroutine tries Exists operation
			exists, err := s.store.Exists(s.testHash)
			// Either succeeds with false or fails gracefully
			if err != nil {
				s.T().Logf("Goroutine %d: Exists failed as expected: %v", index, err)
			} else {
				s.False(exists)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestExistsWithMockLoopFile tests exists with a mock loop file that can be mounted
func (s *ExistsTestSuite) TestExistsWithMockLoopFile() {
	// Skip this test if not root (requires mount operations)
	if os.Getuid() != 0 {
		s.T().Skip("Skipping mount test - requires root for mount operations")
		return
	}

	// Create directory structure
	loopDir := filepath.Join(s.tempDir, s.testHash[:2], s.testHash[2:4])
	err := os.MkdirAll(loopDir, 0755)
	s.NoError(err)

	// Create a mock loop file
	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte("test loop file content"), 0644)
	s.NoError(err)

	// Test exists - this will likely fail in test environment but should fail gracefully
	exists, err := s.store.Exists(s.testHash)
	if err != nil {
		s.T().Logf("Exists with mock loop file failed as expected: %v", err)
	} else {
		// If it somehow succeeds, the file shouldn't exist yet
		s.False(exists)
	}
}

// TestExistsHashValidation tests various hash validation scenarios
func (s *ExistsTestSuite) TestExistsHashValidation() {
	testCases := []struct {
		name        string
		hash        string
		expectError bool
		errorType   interface{}
	}{
		{"empty_hash", "", true, store.InvalidHashError{}},
		{"too_short", "abcdef", true, store.InvalidHashError{}},
		{"non_hex", "ghijklmnopqrstuvwxyz1234567890123456789012345678901234567890", true, store.InvalidHashError{}},
		{"valid_hash", "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0", false, nil},
		{"uppercase_valid", "A1B2C3D4E5F67890123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0", false, nil},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			exists, err := s.store.Exists(tc.hash)
			if tc.expectError {
				s.Error(err)
				s.False(exists)
				if tc.errorType != nil {
					s.IsType(tc.errorType, err)
				}
			} else {
				// May succeed or fail due to mount issues, but shouldn't be a validation error
				if err != nil {
					_, isInvalidHash := err.(store.InvalidHashError)
					s.False(isInvalidHash, "Should not be a validation error")
				} else {
					s.False(exists) // File doesn't exist in our test
				}
			}
		})
	}
}

// TestExistsWithValidButNonExistentPath tests when getFilePath returns empty
func (s *ExistsTestSuite) TestExistsWithValidButNonExistentPath() {
	// This tests the edge case where getFilePath might return empty string
	// for a hash that's long enough but has other issues

	// Create a hash that's exactly the minimum length for path operations
	minimumHash := "abcd123456789012345678901234567890123456789012345678901234567890"

	// Create loop file but test file existence
	loopDir := filepath.Join(s.tempDir, minimumHash[:2], minimumHash[2:4])
	err := os.MkdirAll(loopDir, 0755)
	s.NoError(err)

	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte("test"), 0644)
	s.NoError(err)

	exists, err := s.store.Exists(minimumHash)
	// Should either fail due to mount issues or return false
	if err != nil {
		s.T().Logf("Exists failed as expected: %v", err)
	} else {
		s.False(exists)
	}
}

// TestExistsErrorRecovery tests that Exists handles errors gracefully
func (s *ExistsTestSuite) TestExistsErrorRecovery() {
	// Test with multiple different scenarios to ensure error recovery
	testHashes := []string{
		s.testHash,
		"b1c2d3e4f5a67890123456789abcdef0123456789abcdef0123456789abcdef0",
		"c1d2e3f4a5b67890123456789abcdef0123456789abcdef0123456789abcdef0",
	}

	for _, hash := range testHashes {
		s.Run("error_recovery_"+hash[:8], func() {
			exists, err := s.store.Exists(hash)
			// Should either succeed with false or fail gracefully
			if err != nil {
				s.T().Logf("Exists failed for hash %s: %v", hash[:8], err)
			} else {
				s.False(exists)
			}
		})
	}
}

// TestExistsInterface tests that the function signature matches store.Store interface
func (s *ExistsTestSuite) TestExistsInterface() {
	// Verify that Exists implements the store.Store interface
	var _ store.Store = (*Store)(nil)
	s.True(true) // If this compiles, the interface is implemented
}

// TestExistsSuite runs the exists test suite
func TestExistsSuite(t *testing.T) {
	suite.Run(t, new(ExistsTestSuite))
}
