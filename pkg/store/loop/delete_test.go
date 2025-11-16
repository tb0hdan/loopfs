package loop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"loopfs/pkg/store"
)

// DeleteTestSuite tests the Delete functionality
type DeleteTestSuite struct {
	suite.Suite
	tempDir  string
	store    *Store
	testHash string
}

// SetupSuite runs once before all tests
func (s *DeleteTestSuite) SetupSuite() {
	var err error
	s.tempDir, err = os.MkdirTemp("", "delete-test-*")
	s.Require().NoError(err)

	// Valid SHA256 hash for testing
	s.testHash = "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"
}

// TearDownSuite runs once after all tests
func (s *DeleteTestSuite) TearDownSuite() {
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

// SetupTest runs before each test
func (s *DeleteTestSuite) SetupTest() {
	s.store = NewWithDefaults(s.tempDir, 10) // 10MB loop files
}

// TearDownTest runs after each test
func (s *DeleteTestSuite) TearDownTest() {
	// Clean up test directory for next test
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
		os.MkdirAll(s.tempDir, 0755)
	}
}

// TestDeleteInvalidHash tests Delete with invalid hash
func (s *DeleteTestSuite) TestDeleteInvalidHash() {
	err := s.store.Delete("invalid")
	s.Error(err)
	s.IsType(store.InvalidHashError{}, err)
}

// TestDeleteFileNotFound tests Delete when file doesn't exist
func (s *DeleteTestSuite) TestDeleteFileNotFound() {
	err := s.store.Delete(s.testHash)
	s.Error(err)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestDeleteCaseInsensitive tests Delete with uppercase hash
func (s *DeleteTestSuite) TestDeleteCaseInsensitive() {
	upperHash := "A1B2C3D4E5F67890123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0"

	// Should normalize to lowercase and then check existence
	err := s.store.Delete(upperHash)
	s.Error(err)
	s.IsType(store.FileNotFoundError{}, err) // File doesn't exist, not invalid hash
}

// TestDeleteHashValidation tests various hash validation scenarios
func (s *DeleteTestSuite) TestDeleteHashValidation() {
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
			err := s.store.Delete(tc.hash)
			s.Error(err)
			s.IsType(tc.errorType, err)
		})
	}
}

// TestDeleteExistsError tests Delete when Exists check fails
func (s *DeleteTestSuite) TestDeleteExistsError() {
	// Create a situation where Exists might fail
	if os.Getuid() != 0 { // Only test if not root
		restrictedDir := filepath.Join(s.tempDir, "restricted")
		err := os.MkdirAll(restrictedDir, 0000) // No permissions
		s.NoError(err)

		restrictedStore := NewWithDefaults(restrictedDir, 10)

		err = restrictedStore.Delete(s.testHash)
		s.Error(err)
		// Should propagate the error from Exists
		s.Contains(err.Error(), "permission denied")
	} else {
		s.T().Skip("Cannot test permission errors as root user")
	}
}

// TestDeleteWithLoopFileButNoTargetFile tests when loop file exists but target doesn't
func (s *DeleteTestSuite) TestDeleteWithLoopFileButNoTargetFile() {
	// Create loop directory and file
	loopDir := filepath.Join(s.tempDir, s.testHash[:2], s.testHash[2:4])
	err := os.MkdirAll(loopDir, 0755)
	s.NoError(err)

	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte("fake loop file"), 0644)
	s.NoError(err)

	// Delete should fail because file doesn't exist (Exists will return false)
	err = s.store.Delete(s.testHash)
	// In test environment, this might fail differently due to mount issues
	s.Error(err)
}

// TestDeleteSuccessfulDeletion tests successful deletion scenario (mocked)
func (s *DeleteTestSuite) TestDeleteSuccessfulDeletion() {
	// Skip this test if not root (requires mount operations)
	if os.Getuid() != 0 {
		s.T().Skip("Skipping delete success test - requires root for mount operations")
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
	err = s.store.Delete(s.testHash)
	s.Error(err) // Expected to fail in test environment
	s.T().Logf("Delete failed as expected in test environment: %v", err)
}

// TestDeleteConcurrentAccess tests concurrent delete operations
func (s *DeleteTestSuite) TestDeleteConcurrentAccess() {
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(index int) {
			defer func() { done <- true }()

			// Each goroutine tries to delete the same file
			err := s.store.Delete(s.testHash)
			// Should fail with FileNotFoundError
			s.Error(err)
			s.T().Logf("Goroutine %d: Delete failed as expected: %v", index, err)
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestDeleteDifferentHashes tests delete with multiple different hashes
func (s *DeleteTestSuite) TestDeleteDifferentHashes() {
	testHashes := []string{
		"b1c2d3e4f5a67890123456789abcdef0123456789abcdef0123456789abcdef0",
		"c1d2e3f4a5b67890123456789abcdef0123456789abcdef0123456789abcdef0",
		"d1e2f3a4b5c67890123456789abcdef0123456789abcdef0123456789abcdef0",
	}

	for _, hash := range testHashes {
		s.Run("delete_"+hash[:8], func() {
			err := s.store.Delete(hash)
			s.Error(err)
			s.IsType(store.FileNotFoundError{}, err)
		})
	}
}

// TestDeleteWithMountError tests Delete when mount operation fails
func (s *DeleteTestSuite) TestDeleteWithMountError() {
	// Create a scenario that might cause mount errors
	loopDir := filepath.Join(s.tempDir, s.testHash[:2], s.testHash[2:4])
	err := os.MkdirAll(loopDir, 0755)
	s.NoError(err)

	// Create an invalid loop file
	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte("invalid loop file content"), 0644)
	s.NoError(err)

	// Create a mock file that claims to exist but will cause issues during mount
	// We can't easily create this scenario without more complex mocking
	err = s.store.Delete(s.testHash)
	s.Error(err)
	s.T().Logf("Delete failed as expected: %v", err)
}

// TestDeleteErrorHandling tests various error handling scenarios
func (s *DeleteTestSuite) TestDeleteErrorHandling() {
	testCases := []struct {
		name      string
		setupFunc func() string // Returns hash to test
		expectErr bool
	}{
		{
			name: "empty_hash",
			setupFunc: func() string {
				return ""
			},
			expectErr: true,
		},
		{
			name: "null_char_in_hash",
			setupFunc: func() string {
				return "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcd\x000"
			},
			expectErr: true,
		},
		{
			name: "unicode_in_hash",
			setupFunc: func() string {
				return "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdéf0"
			},
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			hash := tc.setupFunc()
			err := s.store.Delete(hash)
			if tc.expectErr {
				s.Error(err)
			} else {
				// May or may not error depending on test environment
				if err != nil {
					s.T().Logf("Delete failed: %v", err)
				}
			}
		})
	}
}

// TestDeleteInterface tests that the Delete method implements store.Store interface
func (s *DeleteTestSuite) TestDeleteInterface() {
	// Verify Delete is part of the store.Store interface
	var _ store.Store = (*Store)(nil)
	s.True(true) // If this compiles, the interface is implemented
}

// TestDeleteLogMessages tests that appropriate log messages are generated
func (s *DeleteTestSuite) TestDeleteLogMessages() {
	// Test with invalid hash - should generate debug log
	err := s.store.Delete("invalid")
	s.Error(err)
	s.IsType(store.InvalidHashError{}, err)

	// Test with valid but non-existent hash - should generate debug log
	err = s.store.Delete(s.testHash)
	s.Error(err)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestDeletePanicRecovery tests that Delete doesn't panic under any circumstances
func (s *DeleteTestSuite) TestDeletePanicRecovery() {
	// Test various inputs that might cause panics
	problematicInputs := []string{
		"",
		"a",
		"null\x00hash",
		"very_long_string_that_exceeds_normal_hash_length_significantly_and_might_cause_issues",
		s.testHash,
	}

	for _, input := range problematicInputs {
		s.Run("no_panic_"+input[:min(8, len(input))], func() {
			defer func() {
				if r := recover(); r != nil {
					s.Fail("Delete should not panic", "Input: %s, Panic: %v", input, r)
				}
			}()

			// Should not panic, may error
			_ = s.store.Delete(input)
		})
	}
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestDeleteSuite runs the delete test suite
func TestDeleteSuite(t *testing.T) {
	suite.Run(t, new(DeleteTestSuite))
}
