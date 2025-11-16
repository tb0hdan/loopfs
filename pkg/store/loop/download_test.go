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

// TestDownloadInvalidHash tests Download with invalid hash
func (s *DownloadTestSuite) TestDownloadInvalidHash() {
	tempFile, err := s.store.Download("invalid")
	s.Error(err)
	s.Empty(tempFile)
	s.IsType(store.InvalidHashError{}, err)
}

// TestDownloadNoLoopFile tests Download when loop file doesn't exist
func (s *DownloadTestSuite) TestDownloadNoLoopFile() {
	tempFile, err := s.store.Download(s.testHash)
	s.Error(err)
	s.Empty(tempFile)
	s.IsType(store.FileNotFoundError{}, err)
}

// TestDownloadCaseInsensitive tests Download with uppercase hash
func (s *DownloadTestSuite) TestDownloadCaseInsensitive() {
	upperHash := "A1B2C3D4E5F67890123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0"

	tempFile, err := s.store.Download(upperHash)
	s.Error(err)
	s.Empty(tempFile)
	s.IsType(store.FileNotFoundError{}, err) // Should normalize hash but file doesn't exist
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

// TestValidateHashAndLoopFile tests the validateHashAndLoopFile helper function
func (s *DownloadTestSuite) TestValidateHashAndLoopFile() {
	// Test invalid hash
	err := s.store.validateHashAndLoopFile("invalid")
	s.Error(err)
	s.IsType(store.InvalidHashError{}, err)

	// Test valid hash but non-existent loop file
	err = s.store.validateHashAndLoopFile(s.testHash)
	s.Error(err)
	s.IsType(store.FileNotFoundError{}, err)

	// Test with existing loop file
	loopDir := filepath.Join(s.tempDir, s.testHash[:2], s.testHash[2:4])
	err = os.MkdirAll(loopDir, 0755)
	s.NoError(err)

	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte("fake loop file"), 0644)
	s.NoError(err)

	err = s.store.validateHashAndLoopFile(s.testHash)
	s.NoError(err) // Should succeed now that loop file exists
}

// TestCopyFileToTempNonExistentFile tests copyFileToTemp with non-existent source
func (s *DownloadTestSuite) TestCopyFileToTempNonExistentFile() {
	tempPath, err := s.store.copyFileToTemp("/nonexistent/file", s.testHash)
	s.Error(err)
	s.Empty(tempPath)
	s.Contains(err.Error(), "no such file or directory")
}

// TestCopyFileToTempValidFile tests copyFileToTemp with valid source file
func (s *DownloadTestSuite) TestCopyFileToTempValidFile() {
	// Create a source file
	sourceFile := filepath.Join(s.tempDir, "source.txt")
	testContent := "test file content for download"
	err := os.WriteFile(sourceFile, []byte(testContent), 0644)
	s.NoError(err)

	// Test copying to temp
	tempPath, err := s.store.copyFileToTemp(sourceFile, s.testHash)
	s.NoError(err)
	s.NotEmpty(tempPath)

	// Verify temp file exists and has correct content
	_, err = os.Stat(tempPath)
	s.NoError(err)

	content, err := os.ReadFile(tempPath)
	s.NoError(err)
	s.Equal(testContent, string(content))

	// Clean up temp file
	os.Remove(tempPath)
}

// TestCopyFileToTempEmptyFile tests copyFileToTemp with empty source file
func (s *DownloadTestSuite) TestCopyFileToTempEmptyFile() {
	// Create an empty source file
	sourceFile := filepath.Join(s.tempDir, "empty.txt")
	err := os.WriteFile(sourceFile, []byte(""), 0644)
	s.NoError(err)

	// Test copying to temp
	tempPath, err := s.store.copyFileToTemp(sourceFile, s.testHash)
	s.NoError(err)
	s.NotEmpty(tempPath)

	// Verify temp file exists and is empty
	content, err := os.ReadFile(tempPath)
	s.NoError(err)
	s.Empty(content)

	// Clean up temp file
	os.Remove(tempPath)
}

// TestDownloadWithLoopFileButNoTargetFile tests when loop file exists but target doesn't
func (s *DownloadTestSuite) TestDownloadWithLoopFileButNoTargetFile() {
	// Create loop directory and file
	loopDir := filepath.Join(s.tempDir, s.testHash[:2], s.testHash[2:4])
	err := os.MkdirAll(loopDir, 0755)
	s.NoError(err)

	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte("fake loop file"), 0644)
	s.NoError(err)

	// Download should fail because file doesn't exist in loop
	tempFile, err := s.store.Download(s.testHash)
	s.Error(err)
	s.Empty(tempFile)
	s.T().Logf("Download failed as expected: %v", err)
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

// TestDownloadHashValidation tests various hash validation scenarios
func (s *DownloadTestSuite) TestDownloadHashValidation() {
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
		s.Run("download_"+tc.name, func() {
			tempFile, err := s.store.Download(tc.hash)
			s.Error(err)
			s.Empty(tempFile)
			s.IsType(tc.errorType, err)
		})

		s.Run("download_stream_"+tc.name, func() {
			reader, err := s.store.DownloadStream(tc.hash)
			s.Error(err)
			s.Nil(reader)
			s.IsType(tc.errorType, err)
		})
	}
}

// TestDownloadConcurrentAccess tests concurrent download operations
func (s *DownloadTestSuite) TestDownloadConcurrentAccess() {
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(index int) {
			defer func() { done <- true }()

			// Test Download
			tempFile, err := s.store.Download(s.testHash)
			s.Error(err) // Should fail because file doesn't exist
			s.Empty(tempFile)

			// Test DownloadStream
			reader, err := s.store.DownloadStream(s.testHash)
			s.Error(err) // Should fail because file doesn't exist
			s.Nil(reader)

			s.T().Logf("Goroutine %d: Download operations failed as expected", index)
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestDownloadInterface tests that Download methods implement store.Store interface
func (s *DownloadTestSuite) TestDownloadInterface() {
	// Verify Download and DownloadStream are part of the store.Store interface
	var _ store.Store = (*Store)(nil)
	s.True(true) // If this compiles, the interface is implemented
}

// TestDownloadErrorHandling tests various error conditions
func (s *DownloadTestSuite) TestDownloadErrorHandling() {
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
	}

	for _, tc := range testCases {
		s.Run("download_"+tc.name, func() {
			hash := tc.setupFunc()
			tempFile, err := s.store.Download(hash)
			if tc.expectErr {
				s.Error(err)
				s.Empty(tempFile)
				if tc.expectedType != nil {
					s.IsType(tc.expectedType, err)
				}
			}
		})

		s.Run("download_stream_"+tc.name, func() {
			hash := tc.setupFunc()
			reader, err := s.store.DownloadStream(hash)
			if tc.expectErr {
				s.Error(err)
				s.Nil(reader)
				if tc.expectedType != nil {
					s.IsType(tc.expectedType, err)
				}
			}
		})
	}
}

// TestDownloadPanicRecovery tests that Download methods don't panic
func (s *DownloadTestSuite) TestDownloadPanicRecovery() {
	problematicInputs := []string{
		"",
		"a",
		"null\x00hash",
		"very_long_string_that_exceeds_normal_hash_length_significantly",
		s.testHash,
	}

	for _, input := range problematicInputs {
		s.Run("download_no_panic_"+input[:min(8, len(input))], func() {
			defer func() {
				if r := recover(); r != nil {
					s.Fail("Download should not panic", "Input: %s, Panic: %v", input, r)
				}
			}()

			// Should not panic, may error
			_, _ = s.store.Download(input)
		})

		s.Run("download_stream_no_panic_"+input[:min(8, len(input))], func() {
			defer func() {
				if r := recover(); r != nil {
					s.Fail("DownloadStream should not panic", "Input: %s, Panic: %v", input, r)
				}
			}()

			// Should not panic, may error
			reader, _ := s.store.DownloadStream(input)
			if reader != nil {
				reader.Close() // Clean up if somehow succeeded
			}
		})
	}
}

// TestEnsureLoopFileExists tests ensureLoopFileExists helper function
func (s *DownloadTestSuite) TestEnsureLoopFileExists() {
	// Test with non-existent loop file - will try to create it
	err := s.store.ensureLoopFileExists(s.testHash)
	// May succeed or fail depending on test environment
	if err != nil {
		s.T().Logf("ensureLoopFileExists failed as expected: %v", err)
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

// TestCleanupAfterError tests cleanupAfterError helper
func (s *DownloadTestSuite) TestCleanupAfterError() {
	mountPoint := s.store.getMountPoint(s.testHash)

	// This should not panic or cause issues
	s.store.cleanupAfterError(s.testHash, mountPoint)
	// No assertions needed - just verify it doesn't panic
}

// TestDownloadSuite runs the download test suite
func TestDownloadSuite(t *testing.T) {
	suite.Run(t, new(DownloadTestSuite))
}
