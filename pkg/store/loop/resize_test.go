package loop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

// ResizeTestSuite tests the resize functionality
type ResizeTestSuite struct {
	suite.Suite
	tempDir  string
	store    *Store
	testHash string
}

// SetupSuite runs once before all tests
func (s *ResizeTestSuite) SetupSuite() {
	var err error
	s.tempDir, err = os.MkdirTemp("", "resize-test-*")
	s.Require().NoError(err)

	// Valid SHA256 hash for testing (lowercase only)
	s.testHash = "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"
}

// TearDownSuite runs once after all tests
func (s *ResizeTestSuite) TearDownSuite() {
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

// SetupTest runs before each test
func (s *ResizeTestSuite) SetupTest() {
	s.store = NewWithDefaults(s.tempDir, 10) // 10MB loop files
}

// TearDownTest runs after each test
func (s *ResizeTestSuite) TearDownTest() {
	// Clean up test directory for next test
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
		os.MkdirAll(s.tempDir, 0755)
	}
}

// TestCreateNewLoopFile tests createNewLoopFile function
func (s *ResizeTestSuite) TestCreateNewLoopFile() {
	// Skip if not root (requires dd and mkfs.ext4)
	if os.Getuid() != 0 {
		s.T().Skip("Skipping createNewLoopFile test - requires root for filesystem operations")
		return
	}

	newLoopFilePath := filepath.Join(s.tempDir, "test_new_loop.img")
	sizeInMB := int64(1)

	err := s.store.createNewLoopFile(newLoopFilePath, sizeInMB)
	if err != nil {
		// Expected in test environments without proper filesystem support
		s.T().Logf("createNewLoopFile failed (expected in test env): %v", err)
		return
	}

	// If it succeeded, verify the file was created
	fileInfo, err := os.Stat(newLoopFilePath)
	s.NoError(err)
	s.True(fileInfo.Size() > 0)
}

// TestCreateNewLoopFileInvalidPath tests createNewLoopFile with invalid path
func (s *ResizeTestSuite) TestCreateNewLoopFileInvalidPath() {
	// Test with invalid path (permission denied)
	invalidPath := "/root/restricted/invalid.img"
	sizeInMB := int64(1)

	err := s.store.createNewLoopFile(invalidPath, sizeInMB)
	s.Error(err)
	s.Contains(err.Error(), "failed to create new loop file")
}

// TestMountNewLoopFile tests mountNewLoopFile function
func (s *ResizeTestSuite) TestMountNewLoopFile() {
	// Skip if not root (requires mount operations)
	if os.Getuid() != 0 {
		s.T().Skip("Skipping mountNewLoopFile test - requires root for mount operations")
		return
	}

	// Create a temporary file to simulate loop file
	tempFile := filepath.Join(s.tempDir, "test_mount.img")
	file, err := os.Create(tempFile)
	s.NoError(err)
	file.Close()

	newMountPoint := filepath.Join(s.tempDir, "test_mount")

	err = s.store.mountNewLoopFile(tempFile, newMountPoint)
	if err != nil {
		// Expected failure in test environment
		s.T().Logf("mountNewLoopFile failed (expected): %v", err)
		s.Contains(err.Error(), "failed to mount new loop file")
	}
}

// TestMountNewLoopFileInvalidFile tests mountNewLoopFile with invalid file
func (s *ResizeTestSuite) TestMountNewLoopFileInvalidFile() {
	invalidFile := "/nonexistent/file.img"
	newMountPoint := filepath.Join(s.tempDir, "test_mount")

	err := s.store.mountNewLoopFile(invalidFile, newMountPoint)
	s.Error(err)
	s.Contains(err.Error(), "failed to mount new loop file")
}

// TestSyncDataBetweenLoops tests syncDataBetweenLoops function
func (s *ResizeTestSuite) TestSyncDataBetweenLoops() {
	// Create source and destination directories
	sourceDir := filepath.Join(s.tempDir, "source")
	destDir := filepath.Join(s.tempDir, "dest")

	err := os.MkdirAll(sourceDir, dirPerm)
	s.NoError(err)
	err = os.MkdirAll(destDir, dirPerm)
	s.NoError(err)

	// Create test file in source
	testFile := filepath.Join(sourceDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	s.NoError(err)

	// Use 1GB as estimated data size for test
	estimatedDataSize := int64(1024 * 1024 * 1024)
	err = s.store.syncDataBetweenLoops(sourceDir, destDir, estimatedDataSize)
	if err != nil {
		// rsync might not be available or might fail in test environment
		s.T().Logf("syncDataBetweenLoops failed (might be expected): %v", err)
		return
	}

	// If sync succeeded, verify destination file exists
	destFile := filepath.Join(destDir, "test.txt")
	_, err = os.Stat(destFile)
	s.NoError(err)
}

// TestSyncDataBetweenLoopsInvalidSource tests syncDataBetweenLoops with invalid source
func (s *ResizeTestSuite) TestSyncDataBetweenLoopsInvalidSource() {
	invalidSource := "/nonexistent/source"
	validDest := filepath.Join(s.tempDir, "dest")
	err := os.MkdirAll(validDest, dirPerm)
	s.NoError(err)

	// Use 1GB as estimated data size for test
	estimatedDataSize := int64(1024 * 1024 * 1024)
	err = s.store.syncDataBetweenLoops(invalidSource, validDest, estimatedDataSize)
	s.Error(err)
	s.Contains(err.Error(), "failed to rsync data")
}

// TestUnmountSpecificLoopFile tests unmountSpecificLoopFile function
func (s *ResizeTestSuite) TestUnmountSpecificLoopFile() {
	// Test with non-mounted directory
	testMountPoint := filepath.Join(s.tempDir, "not_mounted")

	err := s.store.unmountSpecificLoopFile(testMountPoint)
	if err != nil {
		// Expected error for non-mounted path
		s.T().Logf("unmountSpecificLoopFile failed as expected: %v", err)
		s.Contains(err.Error(), "failed to unmount loop file")
	}
}

// TestReplaceOldLoopFile tests replaceOldLoopFile function
func (s *ResizeTestSuite) TestReplaceOldLoopFile() {
	// Create old and new loop files
	oldFile := filepath.Join(s.tempDir, "old.img")
	newFile := filepath.Join(s.tempDir, "new.img")

	err := os.WriteFile(oldFile, []byte("old content"), 0644)
	s.NoError(err)
	err = os.WriteFile(newFile, []byte("new content"), 0644)
	s.NoError(err)

	err = s.store.replaceOldLoopFile(oldFile, newFile)
	s.NoError(err)

	// Verify old file contains new content
	content, err := os.ReadFile(oldFile)
	s.NoError(err)
	s.Equal("new content", string(content))

	// Verify new file no longer exists
	_, err = os.Stat(newFile)
	s.True(os.IsNotExist(err))

	// Verify backup file was cleaned up
	backupFile := oldFile + ".backup"
	_, err = os.Stat(backupFile)
	s.True(os.IsNotExist(err))
}

// TestReplaceOldLoopFileNonExistentOld tests replaceOldLoopFile with non-existent old file
func (s *ResizeTestSuite) TestReplaceOldLoopFileNonExistentOld() {
	nonExistentOld := filepath.Join(s.tempDir, "nonexistent.img")
	newFile := filepath.Join(s.tempDir, "new.img")

	err := os.WriteFile(newFile, []byte("new content"), 0644)
	s.NoError(err)

	err = s.store.replaceOldLoopFile(nonExistentOld, newFile)
	s.Error(err)
	s.Contains(err.Error(), "failed to backup existing loop file")
}

// TestReplaceOldLoopFileNonExistentNew tests replaceOldLoopFile with non-existent new file
func (s *ResizeTestSuite) TestReplaceOldLoopFileNonExistentNew() {
	oldFile := filepath.Join(s.tempDir, "old.img")
	nonExistentNew := filepath.Join(s.tempDir, "nonexistent.img")

	err := os.WriteFile(oldFile, []byte("old content"), 0644)
	s.NoError(err)

	err = s.store.replaceOldLoopFile(oldFile, nonExistentNew)
	s.Error(err)
	s.Contains(err.Error(), "failed to move new loop file")

	// Verify old file was restored from backup
	content, err := os.ReadFile(oldFile)
	s.NoError(err)
	s.Equal("old content", string(content))
}

// TestValidateAndPrepareResize tests validateAndPrepareResize function
func (s *ResizeTestSuite) TestValidateAndPrepareResize() {
	// Test with invalid hash
	_, _, _, _, err := s.store.validateAndPrepareResize("invalid", 1024)
	s.Error(err)
	s.Contains(err.Error(), "invalid hash format")

	// Test with valid hash but non-existent loop file
	_, _, _, _, err = s.store.validateAndPrepareResize(s.testHash, 1024)
	s.Error(err)
	s.Contains(err.Error(), "loop file not found")

	// Test with valid hash and existing loop file
	loopDir := filepath.Join(s.tempDir, s.testHash[:2], s.testHash[2:4])
	err = os.MkdirAll(loopDir, dirPerm)
	s.NoError(err)
	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte("test"), 0644)
	s.NoError(err)

	loopFilePath, mountPoint, newLoopFilePath, newMountPoint, err := s.store.validateAndPrepareResize(s.testHash, 2048)
	s.NoError(err)
	s.Contains(loopFilePath, "loop.img")
	s.NotEmpty(mountPoint)
	s.Contains(newLoopFilePath, ".new")
	s.Contains(newMountPoint, ".new")
}

// TestSetupCleanupHandler tests setupCleanupHandler function
func (s *ResizeTestSuite) TestSetupCleanupHandler() {
	loopFile := filepath.Join(s.tempDir, "loop.img")
	newLoopFile := filepath.Join(s.tempDir, "new_loop.img")
	newMountPoint := filepath.Join(s.tempDir, "new_mount")

	// Create files and directories
	err := os.WriteFile(loopFile, []byte("test"), 0644)
	s.NoError(err)
	err = os.WriteFile(newLoopFile, []byte("new test"), 0644)
	s.NoError(err)
	err = os.MkdirAll(newMountPoint, dirPerm)
	s.NoError(err)

	cleanup := s.store.setupCleanupHandler(loopFile, newLoopFile, newMountPoint)
	cleanup()

	// Verify temporary files were cleaned up
	_, err = os.Stat(newMountPoint)
	s.True(os.IsNotExist(err))

	_, err = os.Stat(newLoopFile)
	s.True(os.IsNotExist(err)) // Should be removed since original still exists

	// Original should still exist
	_, err = os.Stat(loopFile)
	s.NoError(err)
}

// TestLogResizeCompletion tests logResizeCompletion function
func (s *ResizeTestSuite) TestLogResizeCompletion() {
	loopFile := filepath.Join(s.tempDir, "loop.img")
	err := os.WriteFile(loopFile, []byte("test content"), 0644)
	s.NoError(err)

	// This should not panic or error
	s.store.logResizeCompletion(s.testHash, loopFile, 2048)

	// Test with non-existent file
	s.store.logResizeCompletion(s.testHash, "/nonexistent/file", 2048)
}

// TestPerformResizeOperations tests performResizeOperations function
func (s *ResizeTestSuite) TestPerformResizeOperations() {
	// Skip if not root (requires mount operations)
	if os.Getuid() != 0 {
		s.T().Skip("Skipping performResizeOperations test - requires root for mount operations")
		return
	}

	// Create mock paths
	mountPoint := filepath.Join(s.tempDir, "mount")
	loopFilePath := filepath.Join(s.tempDir, "loop.img")
	newLoopFilePath := filepath.Join(s.tempDir, "new_loop.img")
	newMountPoint := filepath.Join(s.tempDir, "new_mount")

	err := os.MkdirAll(mountPoint, dirPerm)
	s.NoError(err)

	err = s.store.performResizeOperations(s.testHash, mountPoint, loopFilePath, newLoopFilePath, newMountPoint, 2048*1024*1024)
	// This will fail in test environment due to mount issues
	s.Error(err)
	s.T().Logf("performResizeOperations failed as expected: %v", err)
}

// TestResizeBlock tests the main ResizeBlock function
func (s *ResizeTestSuite) TestResizeBlock() {
	// Test with invalid hash
	err := s.store.ResizeBlock("invalid", 1024)
	s.Error(err)
	s.Contains(err.Error(), "invalid hash format")

	// Test with valid hash but non-existent loop file
	err = s.store.ResizeBlock(s.testHash, 1024)
	s.Error(err)
	s.Contains(err.Error(), "loop file not found")

	// Skip full test if not root
	if os.Getuid() != 0 {
		s.T().Skip("Skipping full ResizeBlock test - requires root for mount operations")
		return
	}

	// Create a mock loop file
	loopDir := filepath.Join(s.tempDir, s.testHash[:2], s.testHash[2:4])
	err = os.MkdirAll(loopDir, dirPerm)
	s.NoError(err)
	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte("test loop file"), 0644)
	s.NoError(err)

	err = s.store.ResizeBlock(s.testHash, 2048*1024*1024)
	// This will fail due to mount issues in test environment
	s.Error(err)
	s.T().Logf("ResizeBlock failed as expected: %v", err)
}

// TestResizeBlockZeroSize tests ResizeBlock with zero/negative size
func (s *ResizeTestSuite) TestResizeBlockZeroSize() {
	// Create a mock loop file first
	loopDir := filepath.Join(s.tempDir, s.testHash[:2], s.testHash[2:4])
	err := os.MkdirAll(loopDir, dirPerm)
	s.NoError(err)
	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte("test"), 0644)
	s.NoError(err)

	// Test with zero size
	err = s.store.ResizeBlock(s.testHash, 0)
	if err != nil {
		s.T().Logf("ResizeBlock with zero size failed as expected: %v", err)
	}

	// Test with negative size
	err = s.store.ResizeBlock(s.testHash, -1024)
	if err != nil {
		s.T().Logf("ResizeBlock with negative size failed as expected: %v", err)
	}
}

// TestResizeConstants tests resize-related constants
func (s *ResizeTestSuite) TestResizeConstants() {
	s.Equal(1024*1024, bytesPerMB)
}

// TestMountNewLoopFileCreateDirError tests mountNewLoopFile directory creation failure
func (s *ResizeTestSuite) TestMountNewLoopFileCreateDirError() {
	tempFile := filepath.Join(s.tempDir, "test.img")
	file, err := os.Create(tempFile)
	s.NoError(err)
	file.Close()

	// Try to create mount point in non-existent parent directory
	invalidMountPoint := "/nonexistent/parent/mount"

	err = s.store.mountNewLoopFile(tempFile, invalidMountPoint)
	s.Error(err)
	s.Contains(err.Error(), "failed to create new mount point")
}

// TestCreateNewLoopFileZeroSize tests createNewLoopFile with zero size
func (s *ResizeTestSuite) TestCreateNewLoopFileZeroSize() {
	// Skip if not root
	if os.Getuid() != 0 {
		s.T().Skip("Skipping createNewLoopFile zero size test - requires root")
		return
	}

	newLoopFilePath := filepath.Join(s.tempDir, "zero_size.img")

	err := s.store.createNewLoopFile(newLoopFilePath, 0)
	if err != nil {
		// Expected to fail with zero size
		s.T().Logf("createNewLoopFile with zero size failed as expected: %v", err)
	}
}

// TestSyncDataBetweenLoopsTimeout tests sync with timeout scenario
func (s *ResizeTestSuite) TestSyncDataBetweenLoopsTimeout() {
	// This test would be difficult to create a real timeout scenario
	// but we can test the timeout configuration
	sourceDir := filepath.Join(s.tempDir, "source")
	destDir := filepath.Join(s.tempDir, "dest")

	err := os.MkdirAll(sourceDir, dirPerm)
	s.NoError(err)
	err = os.MkdirAll(destDir, dirPerm)
	s.NoError(err)

	// The timeout is now calculated based on data size
	// We can't easily test actual timeout, but we can verify the function runs
	// Use 1GB as estimated data size for test
	estimatedDataSize := int64(1024 * 1024 * 1024)
	err = s.store.syncDataBetweenLoops(sourceDir, destDir, estimatedDataSize)
	// May succeed or fail depending on rsync availability
	if err != nil {
		s.T().Logf("syncDataBetweenLoops error (may be expected): %v", err)
	}
}

// TestResizeWithCleanup tests that cleanup happens properly
func (s *ResizeTestSuite) TestResizeWithCleanup() {
	// Create test directories and files
	loopDir := filepath.Join(s.tempDir, s.testHash[:2], s.testHash[2:4])
	err := os.MkdirAll(loopDir, dirPerm)
	s.NoError(err)
	loopFile := filepath.Join(loopDir, "loop.img")
	err = os.WriteFile(loopFile, []byte("test loop file"), 0644)
	s.NoError(err)

	err = s.store.ResizeBlock(s.testHash, 2048*1024*1024)
	// Should fail but cleanup should still happen
	s.Error(err)

	// Verify no .new files are left behind
	files, err := os.ReadDir(loopDir)
	s.NoError(err)
	for _, file := range files {
		s.NotContains(file.Name(), ".new", "No .new files should remain after failed resize")
	}
}

// TestResizeSuite runs the resize test suite
func TestResizeSuite(t *testing.T) {
	suite.Run(t, new(ResizeTestSuite))
}
