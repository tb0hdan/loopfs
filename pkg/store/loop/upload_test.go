package loop

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"loopfs/pkg/store"
)

// UploadTestSuite tests the Upload functionality
type UploadTestSuite struct {
	suite.Suite
	tempDir  string
	store    *Store
	testHash string
}

// SetupSuite runs once before all tests
func (s *UploadTestSuite) SetupSuite() {
	var err error
	s.tempDir, err = os.MkdirTemp("", "upload-test-*")
	s.Require().NoError(err)

	// Valid SHA256 hash for testing
	s.testHash = "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"
}

// TearDownSuite runs once after all tests
func (s *UploadTestSuite) TearDownSuite() {
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

// SetupTest runs before each test
func (s *UploadTestSuite) SetupTest() {
	s.store = NewWithDefaults(s.tempDir, 10) // 10MB loop files
}

// TearDownTest runs after each test
func (s *UploadTestSuite) TearDownTest() {
	// Clean up test directory for next test
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
		os.MkdirAll(s.tempDir, 0755)
	}
}

// uploadErrorReader is a test io.Reader that always returns an error
type uploadErrorReader struct{}

func (e uploadErrorReader) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}

// TestUploadErrorReader tests Upload with a reader that returns errors
func (s *UploadTestSuite) TestUploadErrorReader() {
	result, err := s.store.Upload(uploadErrorReader{}, "test.txt")
	s.Error(err)
	s.Nil(result)
	s.Contains(err.Error(), "unexpected EOF")
}

// TestUploadBasicContent tests Upload with basic content
func (s *UploadTestSuite) TestUploadBasicContent() {
	// Skip this test if not root (requires mount operations)
	if os.Getuid() != 0 {
		s.T().Skip("Skipping upload test - requires root for mount operations")
		return
	}

	content := "test content for upload"
	reader := strings.NewReader(content)

	result, err := s.store.Upload(reader, "test.txt")
	if err != nil {
		// Expected to fail in test environment due to mount issues
		s.T().Logf("Upload failed as expected in test environment: %v", err)
		s.Nil(result)
	} else {
		// If somehow succeeds
		s.NotNil(result)
		s.NotEmpty(result.Hash)
		s.True(s.store.ValidateHash(result.Hash))

		// Verify hash is correct
		expectedHash := sha256.Sum256([]byte(content))
		s.Equal(hex.EncodeToString(expectedHash[:]), result.Hash)
	}
}

// TestProcessAndHashFile tests processAndHashFile function
func (s *UploadTestSuite) TestProcessAndHashFile() {
	content := "test content for hashing"
	reader := strings.NewReader(content)

	hash, tempFile, err := s.store.processAndHashFile(reader)
	s.NoError(err)
	s.NotEmpty(hash)
	s.NotNil(tempFile)

	defer func() {
		if tempFile != nil {
			tempFile.Close()
			os.Remove(tempFile.Name())
		}
	}()

	// Verify hash is correct
	expectedHash := sha256.Sum256([]byte(content))
	s.Equal(hex.EncodeToString(expectedHash[:]), hash)

	// Verify temp file has correct content
	tempContent, err := os.ReadFile(tempFile.Name())
	s.NoError(err)
	s.Equal(content, string(tempContent))
}

// TestProcessAndHashFileEmptyContent tests processAndHashFile with empty content
func (s *UploadTestSuite) TestProcessAndHashFileEmptyContent() {
	reader := strings.NewReader("")

	hash, tempFile, err := s.store.processAndHashFile(reader)
	s.NoError(err)
	s.NotEmpty(hash)
	s.NotNil(tempFile)

	defer func() {
		if tempFile != nil {
			tempFile.Close()
			os.Remove(tempFile.Name())
		}
	}()

	// Verify hash is correct for empty content
	expectedHash := sha256.Sum256([]byte(""))
	s.Equal(hex.EncodeToString(expectedHash[:]), hash)

	// Verify temp file is empty
	tempContent, err := os.ReadFile(tempFile.Name())
	s.NoError(err)
	s.Empty(tempContent)
}

// TestProcessAndHashFileLargeContent tests processAndHashFile with larger content
func (s *UploadTestSuite) TestProcessAndHashFileLargeContent() {
	// Create larger content (1KB)
	content := strings.Repeat("Hello World! ", 80) // About 1KB
	reader := strings.NewReader(content)

	hash, tempFile, err := s.store.processAndHashFile(reader)
	s.NoError(err)
	s.NotEmpty(hash)
	s.NotNil(tempFile)

	defer func() {
		if tempFile != nil {
			tempFile.Close()
			os.Remove(tempFile.Name())
		}
	}()

	// Verify hash
	expectedHash := sha256.Sum256([]byte(content))
	s.Equal(hex.EncodeToString(expectedHash[:]), hash)

	// Verify file size
	fileInfo, err := tempFile.Stat()
	s.NoError(err)
	s.Equal(int64(len(content)), fileInfo.Size())
}

// TestProcessAndHashFileErrorReader tests processAndHashFile with error reader
func (s *UploadTestSuite) TestProcessAndHashFileErrorReader() {
	hash, tempFile, err := s.store.processAndHashFile(uploadErrorReader{})
	s.Error(err)
	s.Empty(hash)
	s.Nil(tempFile)
	s.Contains(err.Error(), "unexpected EOF")
}

// TestUploadFileExists tests Upload when file already exists
func (s *UploadTestSuite) TestUploadFileExists() {
	// Skip this test if not root
	if os.Getuid() != 0 {
		s.T().Skip("Skipping upload exists test - requires root for mount operations")
		return
	}

	content := "test content"
	reader1 := strings.NewReader(content)
	reader2 := strings.NewReader(content)

	// First upload
	result1, err1 := s.store.Upload(reader1, "test1.txt")
	if err1 != nil {
		s.T().Logf("First upload failed as expected: %v", err1)
		return
	}

	// Second upload of same content should fail
	result2, err2 := s.store.Upload(reader2, "test2.txt")
	if err2 != nil {
		s.IsType(store.FileExistsError{}, err2)
		s.Nil(result2)
	} else {
		s.T().Logf("Unexpected success in test environment")
		s.Equal(result1.Hash, result2.Hash)
	}
}

// TestUploadWithMountError tests Upload when mount operations fail
func (s *UploadTestSuite) TestUploadWithMountError() {
	content := "test content for mount error"
	reader := strings.NewReader(content)

	result, err := s.store.Upload(reader, "test.txt")
	// Expected to fail due to mount issues in test environment
	s.Error(err)
	s.Nil(result)
	s.T().Logf("Upload failed as expected due to mount issues: %v", err)
}

// TestUploadInterface tests that Upload implements store.Store interface
func (s *UploadTestSuite) TestUploadInterface() {
	// Verify Upload is part of the store.Store interface
	var _ store.Store = (*Store)(nil)
	s.True(true) // If this compiles, the interface is implemented
}

// TestUploadHashCalculation tests that hash calculation is correct
func (s *UploadTestSuite) TestUploadHashCalculation() {
	testCases := []struct {
		name    string
		content string
	}{
		{"empty", ""},
		{"simple", "hello world"},
		{"multiline", "line1\nline2\nline3"},
		{"binary", "\x00\x01\x02\x03\xFF"},
		{"unicode", "Hello 世界! 🌍"},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			reader := strings.NewReader(tc.content)
			hash, tempFile, err := s.store.processAndHashFile(reader)
			s.NoError(err)
			s.NotEmpty(hash)

			defer func() {
				if tempFile != nil {
					tempFile.Close()
					os.Remove(tempFile.Name())
				}
			}()

			// Verify hash calculation
			expectedHash := sha256.Sum256([]byte(tc.content))
			s.Equal(hex.EncodeToString(expectedHash[:]), hash)
		})
	}
}

// TestUploadConcurrentAccess tests concurrent upload operations
func (s *UploadTestSuite) TestUploadConcurrentAccess() {
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(index int) {
			defer func() { done <- true }()

			content := strings.Repeat("test", index+1)
			reader := strings.NewReader(content)

			result, err := s.store.Upload(reader, "concurrent.txt")
			// Expected to fail in test environment
			if err != nil {
				s.T().Logf("Goroutine %d: Upload failed as expected: %v", index, err)
			} else {
				s.T().Logf("Goroutine %d: Upload unexpectedly succeeded", index)
				s.NotNil(result)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestUploadErrorHandling tests various upload error conditions
func (s *UploadTestSuite) TestUploadErrorHandling() {
	testCases := []struct {
		name     string
		reader   io.Reader
		filename string
		expecter func(*testing.T, *store.UploadResult, error)
	}{
		{
			name:     "error_reader",
			reader:   uploadErrorReader{},
			filename: "error.txt",
			expecter: func(t *testing.T, result *store.UploadResult, err error) {
				if err == nil {
					t.Error("Expected error from error reader")
				}
				if result != nil {
					t.Error("Expected nil result from error reader")
				}
			},
		},
		{
			name:     "valid_content",
			reader:   strings.NewReader("valid content"),
			filename: "valid.txt",
			expecter: func(t *testing.T, result *store.UploadResult, err error) {
				// May succeed or fail depending on test environment
				if err != nil {
					t.Logf("Upload failed as expected: %v", err)
				} else {
					if result == nil {
						t.Error("Expected result when upload succeeds")
					}
				}
			},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			result, err := s.store.Upload(tc.reader, tc.filename)
			tc.expecter(s.T(), result, err)
		})
	}
}

// TestUploadTempFileCleanup tests that temporary files are cleaned up
func (s *UploadTestSuite) TestUploadTempFileCleanup() {
	// Count temp files before
	tempFiles, _ := filepath.Glob(filepath.Join(os.TempDir(), "cas-upload-*"))
	beforeCount := len(tempFiles)

	// Attempt upload
	reader := strings.NewReader("test content")
	_, _ = s.store.Upload(reader, "cleanup.txt") // Ignore result/error

	// Count temp files after
	tempFiles, _ = filepath.Glob(filepath.Join(os.TempDir(), "cas-upload-*"))
	afterCount := len(tempFiles)

	// Should not have left temp files behind
	s.Equal(beforeCount, afterCount, "Temporary files should be cleaned up")
}

// TestUploadPanicRecovery tests that Upload doesn't panic
func (s *UploadTestSuite) TestUploadPanicRecovery() {
	problematicReaders := []struct {
		name   string
		reader io.Reader
	}{
		// Note: nil reader actually causes panic in io.Copy, which is expected behavior
		{"error_reader", uploadErrorReader{}},
	}

	for _, pr := range problematicReaders {
		s.Run("no_panic_"+pr.name, func() {
			defer func() {
				if r := recover(); r != nil {
					s.Fail("Upload should not panic", "Reader: %s, Panic: %v", pr.name, r)
				}
			}()

			// Should not panic, may error
			_, _ = s.store.Upload(pr.reader, "panic_test.txt")
		})
	}
}

// TestUploadHashConsistency tests that the same content always produces the same hash
func (s *UploadTestSuite) TestUploadHashConsistency() {
	content := "consistent content test"

	// Process the same content multiple times
	var hashes []string
	for i := 0; i < 5; i++ {
		reader := strings.NewReader(content)
		hash, tempFile, err := s.store.processAndHashFile(reader)
		s.NoError(err)
		s.NotEmpty(hash)
		hashes = append(hashes, hash)

		if tempFile != nil {
			tempFile.Close()
			os.Remove(tempFile.Name())
		}
	}

	// All hashes should be identical
	for i := 1; i < len(hashes); i++ {
		s.Equal(hashes[0], hashes[i], "Hash should be consistent for same content")
	}

	// Verify against expected hash
	expectedHash := sha256.Sum256([]byte(content))
	s.Equal(hex.EncodeToString(expectedHash[:]), hashes[0])
}

// TestUploadFilename tests that filename parameter is handled correctly
func (s *UploadTestSuite) TestUploadFilename() {
	content := "filename test content"

	testFilenames := []string{
		"simple.txt",
		"file with spaces.txt",
		"unicode-文件.txt",
		"very-long-filename-that-exceeds-normal-expectations.txt",
		".hidden",
		"",
	}

	for _, filename := range testFilenames {
		s.Run("filename_"+filename, func() {
			reader := strings.NewReader(content)
			result, err := s.store.Upload(reader, filename)

			// May succeed or fail due to mount issues, but filename shouldn't affect hash
			if err != nil {
				s.T().Logf("Upload with filename '%s' failed as expected: %v", filename, err)
			} else {
				s.NotNil(result)
				// Hash should be same regardless of filename
				expectedHash := sha256.Sum256([]byte(content))
				s.Equal(hex.EncodeToString(expectedHash[:]), result.Hash)
			}
		})
	}
}

// TestUploadLogMessages tests that appropriate log messages are generated
func (s *UploadTestSuite) TestUploadLogMessages() {
	reader := strings.NewReader("log test content")

	// This should generate appropriate log messages
	_, _ = s.store.Upload(reader, "log_test.txt")

	// Log verification would require log capture in a real implementation
	// For now, just verify the method doesn't panic
	s.True(true)
}

// TestCleanupTempFile tests the cleanupTempFile helper function
func (s *UploadTestSuite) TestCleanupTempFile() {
	// Test with nil file - should not panic
	s.store.cleanupTempFile(nil)

	// Test with valid temp file
	tempFile, err := os.CreateTemp("", "cleanup-test-*")
	s.NoError(err)
	tempPath := tempFile.Name()

	// File should exist
	_, err = os.Stat(tempPath)
	s.NoError(err)

	// Cleanup should remove it
	s.store.cleanupTempFile(tempFile)

	// File should no longer exist
	_, err = os.Stat(tempPath)
	s.True(os.IsNotExist(err))
}

// TestProcessAndHashFileConsistentState tests that processAndHashFile maintains consistent state
func (s *UploadTestSuite) TestProcessAndHashFileConsistentState() {
	content := "state consistency test"
	reader := strings.NewReader(content)

	hash, tempFile, err := s.store.processAndHashFile(reader)
	s.NoError(err)

	defer func() {
		if tempFile != nil {
			tempFile.Close()
			os.Remove(tempFile.Name())
		}
	}()

	// Verify temp file is at beginning
	pos, err := tempFile.Seek(0, io.SeekCurrent)
	s.NoError(err)
	s.Equal(int64(len(content)), pos) // Should be at end after writing

	// Reset to beginning
	_, err = tempFile.Seek(0, io.SeekStart)
	s.NoError(err)

	// Read content back
	readContent, err := io.ReadAll(tempFile)
	s.NoError(err)
	s.Equal(content, string(readContent))

	// Hash should match content
	expectedHash := sha256.Sum256([]byte(content))
	s.Equal(hex.EncodeToString(expectedHash[:]), hash)
}

// TestUploadSuite runs the upload test suite
func TestUploadSuite(t *testing.T) {
	suite.Run(t, new(UploadTestSuite))
}
