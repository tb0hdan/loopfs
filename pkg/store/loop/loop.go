package loop

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"loopfs/pkg/log"
	"loopfs/pkg/store"
)

const (
	maxLoopDevices = 256 // Maximum number of loop devices allowed
	minHashLength  = 4
	minHashSubDir  = 8 // Minimum length for subdirectory structure (4 for loop + 4 for subdirectories)
	hashLength     = 64
	dirPerm        = 0750
	blockSize      = "1M"
	commandTimeout = 30 * time.Second // Timeout for exec commands
)

// Store implements the store.Store interface for Loop CAS storage.
type Store struct {
	storageDir    string
	loopFileSize  int64
	mountMutex    sync.Mutex
	creationMutex sync.Mutex
	creationLocks map[string]*sync.Mutex
	refCountMutex sync.Mutex
	refCounts     map[string]int
}

// New creates a new Loop store with the specified storage directory and loop file size.
func New(storageDir string, loopFileSize int64) *Store {
	return &Store{
		storageDir:    storageDir,
		loopFileSize:  loopFileSize,
		creationLocks: make(map[string]*sync.Mutex),
		refCounts:     make(map[string]int),
	}
}

// getLoopFilePath returns the loop file path for a given hash in hierarchical structure.
func (s *Store) getLoopFilePath(hash string) string {
	if len(hash) < minHashLength {
		return ""
	}
	// Create hierarchical path: storageDir/00/01/loop.img
	dir1 := hash[:2]
	dir2 := hash[2:4]
	loopDir := filepath.Join(s.storageDir, dir1, dir2)
	return filepath.Join(loopDir, "loop.img")
}

// getMountPoint returns the mount point for a given hash based on hash prefix.
func (s *Store) getMountPoint(hash string) string {
	if len(hash) < minHashLength {
		return ""
	}
	// Create mount point based on hash prefix: data/ab/cd/loopef
	dir1 := hash[:2]
	dir2 := hash[2:4]
	dir3 := hash[4:6]
	return filepath.Join(s.storageDir, dir1, dir2, "loop"+dir3)
}

// getFilePath returns the file path within the mounted loop filesystem with hierarchical structure.
func (s *Store) getFilePath(hash string) string {
	if len(hash) < minHashLength {
		return ""
	}
	mountPoint := s.getMountPoint(hash)
	// Create hierarchical path within mount: mountpoint/02/03/04050607...
	// First 4 chars (00/01) are used for loop file path, remaining chars go inside
	if len(hash) < minHashSubDir {
		return ""
	}
	subDir1 := hash[4:6]
	subDir2 := hash[6:8]
	subDir := filepath.Join(subDir1, subDir2)
	// Use remaining hash chars (after first 8: 4 for loop path + 4 for subdirs) as filename
	remainingHash := hash[8:]
	return filepath.Join(mountPoint, subDir, remainingHash)
}

// getCreationMutex returns or creates a mutex for the given loop file path.
// This ensures that only one goroutine can create a loop file at a time.
func (s *Store) getCreationMutex(loopFilePath string) *sync.Mutex {
	s.creationMutex.Lock()
	defer s.creationMutex.Unlock()

	if mutex, exists := s.creationLocks[loopFilePath]; exists {
		return mutex
	}

	mutex := &sync.Mutex{}
	s.creationLocks[loopFilePath] = mutex
	return mutex
}

// cleanupCreationMutex removes the mutex for the given loop file path if no longer needed.
// This prevents memory leaks from the creationLocks map.
func (s *Store) cleanupCreationMutex(loopFilePath string) {
	s.creationMutex.Lock()
	defer s.creationMutex.Unlock()

	// Check if loop file exists - if it does, we can safely remove the mutex
	// since creation is complete and future operations will find the existing file
	if _, err := os.Stat(loopFilePath); err == nil {
		delete(s.creationLocks, loopFilePath)
	}
}

// incrementRefCount increments the reference count for a mount point.
// Returns true if this is the first reference (mount needed), false otherwise.
func (s *Store) incrementRefCount(mountPoint string) bool {
	s.refCountMutex.Lock()
	defer s.refCountMutex.Unlock()

	s.refCounts[mountPoint]++
	return s.refCounts[mountPoint] == 1
}

// decrementRefCount decrements the reference count for a mount point.
// Returns true if this was the last reference (unmount needed), false otherwise.
func (s *Store) decrementRefCount(mountPoint string) bool {
	s.refCountMutex.Lock()
	defer s.refCountMutex.Unlock()

	if s.refCounts[mountPoint] > 0 {
		s.refCounts[mountPoint]--
		if s.refCounts[mountPoint] == 0 {
			delete(s.refCounts, mountPoint)
			return true
		}
	}
	return false
}

// findFileInLoop searches for a file in the mounted loop filesystem and returns the actual file path.
// This is needed for download since we need to verify the file exists with the truncated name.
func (s *Store) findFileInLoop(hash string) (string, error) {
	if len(hash) < minHashSubDir {
		return "", store.InvalidHashError{Hash: hash}
	}

	mountPoint := s.getMountPoint(hash)
	subDir1 := hash[4:6]
	subDir2 := hash[6:8]
	subDir := filepath.Join(mountPoint, subDir1, subDir2)

	// The filename should be the remaining hash (after first 8 chars: 4 for loop path + 4 for subdirs)
	expectedFilename := hash[8:]
	filePath := filepath.Join(subDir, expectedFilename)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", store.FileNotFoundError{Hash: hash}
	} else if err != nil {
		return "", err
	}

	return filePath, nil
}

// getUsedLoops checks the number of currently used loop devices and returns an error
// if the count exceeds maxLoopDevices.
func (s *Store) getUsedLoops() error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	// Run losetup -l to get the list of loop devices
	cmd := exec.CommandContext(ctx, "losetup", "-l")
	output, err := cmd.Output()
	if err != nil {
		// If losetup fails with exit code 1, it might mean no loop devices are in use
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			// No loop devices in use
			return nil
		}
		log.Error().Err(err).Msg("Failed to check loop devices")
		return fmt.Errorf("failed to check loop devices: %w", err)
	}

	// Count the number of loop devices (excluding the header line)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	count := 0
	firstLine := true
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Skip the header line
		if firstLine && strings.HasPrefix(line, "NAME") {
			firstLine = false
			continue
		}
		count++
	}

	if err := scanner.Err(); err != nil {
		log.Error().Err(err).Msg("Failed to parse loop device list")
		return fmt.Errorf("failed to parse loop device list: %w", err)
	}

	if count >= maxLoopDevices {
		log.Error().Int("current_count", count).Int("max_allowed", maxLoopDevices).
			Msg("Maximum number of loop devices exceeded")
		return fmt.Errorf("maximum number of loop devices (%d) exceeded: currently %d in use",
			maxLoopDevices, count)
	}

	log.Debug().Int("loop_count", count).Int("max_allowed", maxLoopDevices).
		Msg("Loop device count check passed")
	return nil
}

// createLoopFile creates a new loop file and formats it with ext4.
func (s *Store) createLoopFile(hash string) error {
	loopFilePath := s.getLoopFilePath(hash)

	// Create directory structure for loop file
	loopDir := filepath.Dir(loopFilePath)
	if err := os.MkdirAll(loopDir, dirPerm); err != nil {
		log.Error().Err(err).Str("loop_dir", loopDir).Msg("Failed to create loop file directory")
		return err
	}

	// Create the loop file
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	//nolint:gosec // loopFilePath is constructed from validated hash, not user input
	cmd := exec.CommandContext(ctx, "dd", "if=/dev/zero",
		"of="+loopFilePath,
		"bs="+blockSize,
		fmt.Sprintf("count=%d", s.loopFileSize))
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Str("loop_file", loopFilePath).Msg("Failed to create loop file")
		return err
	}

	// Format with ext4
	ctx2, cancel2 := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel2()
	//nolint:gosec // loopFilePath is constructed from validated hash, not user input
	cmd = exec.CommandContext(ctx2, "mkfs.ext4", "-q", loopFilePath)
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Str("loop_file", loopFilePath).Msg("Failed to format loop file")
		if removeErr := os.Remove(loopFilePath); removeErr != nil {
			log.Error().Err(removeErr).Str("loop_file", loopFilePath).Msg("Failed to remove loop file during cleanup")
		}
		return err
	}

	log.Info().Str("loop_file", loopFilePath).Msg("Loop file created and formatted")
	return nil
}

// mountLoopFile mounts a loop file to its mount point.
func (s *Store) mountLoopFile(hash string) error {
	s.mountMutex.Lock()
	defer s.mountMutex.Unlock()

	// Check loop device limit before proceeding
	if err := s.getUsedLoops(); err != nil {
		return err
	}

	loopFilePath := s.getLoopFilePath(hash)
	mountPoint := s.getMountPoint(hash)

	// Create mount point directory
	if err := os.MkdirAll(mountPoint, dirPerm); err != nil {
		log.Error().Err(err).Str("mount_point", mountPoint).Msg("Failed to create mount point")
		return err
	}

	// Check if already mounted
	if s.isMounted(mountPoint) {
		log.Debug().Str("mount_point", mountPoint).Msg("Loop file already mounted")
		return nil
	}

	// Mount the loop file
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	//nolint:gosec // loopFilePath and mountPoint are constructed from validated hash, not user input
	cmd := exec.CommandContext(ctx, "mount", "-o", "loop", loopFilePath, mountPoint)
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Str("loop_file", loopFilePath).Str("mount_point", mountPoint).Msg("Failed to mount loop file")
		return err
	}

	log.Info().Str("loop_file", loopFilePath).Str("mount_point", mountPoint).Msg("Loop file mounted")
	return nil
}

// unmountLoopFile unmounts a loop file from its mount point.
func (s *Store) unmountLoopFile(hash string) error {
	s.mountMutex.Lock()
	defer s.mountMutex.Unlock()

	mountPoint := s.getMountPoint(hash)

	// Check if mounted
	if !s.isMounted(mountPoint) {
		log.Debug().Str("mount_point", mountPoint).Msg("Loop file not mounted")
		return nil
	}

	// Unmount
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	//nolint:gosec // mountPoint is constructed from validated hash, not user input
	cmd := exec.CommandContext(ctx, "umount", mountPoint)
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Str("mount_point", mountPoint).Msg("Failed to unmount loop file")
		return err
	}

	log.Info().Str("mount_point", mountPoint).Msg("Loop file unmounted")
	return nil
}

// isMounted checks if a mount point is currently mounted.
func (s *Store) isMounted(mountPoint string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "mountpoint", "-q", mountPoint)
	return cmd.Run() == nil
}

// withMountedLoop executes a function with the loop file mounted, ensuring cleanup.
// Uses reference counting to prevent premature unmounting when multiple operations are concurrent.
func (s *Store) withMountedLoop(hash string, callback func() error) error {
	loopFilePath := s.getLoopFilePath(hash)
	mountPoint := s.getMountPoint(hash)

	// Get per-loop-file mutex to synchronize creation
	creationMutex := s.getCreationMutex(loopFilePath)
	creationMutex.Lock()

	// Check if loop file exists, create if not (synchronized)
	if _, err := os.Stat(loopFilePath); os.IsNotExist(err) {
		if err := s.createLoopFile(hash); err != nil {
			creationMutex.Unlock()
			return err
		}
	} else if err != nil {
		creationMutex.Unlock()
		return err
	}

	creationMutex.Unlock()

	// Clean up the creation mutex to prevent memory leaks
	s.cleanupCreationMutex(loopFilePath)

	// Increment reference count - mount only if this is the first reference
	shouldMount := s.incrementRefCount(mountPoint)
	if shouldMount {
		if err := s.mountLoopFile(hash); err != nil {
			// If mount fails, decrement the reference count we just added
			s.decrementRefCount(mountPoint)
			return err
		}
	}

	// Execute the function
	defer func() {
		// Decrement reference count - unmount only if this was the last reference
		shouldUnmount := s.decrementRefCount(mountPoint)
		if shouldUnmount {
			if err := s.unmountLoopFile(hash); err != nil {
				log.Error().Err(err).Str("hash", hash).Msg("Failed to unmount loop file during cleanup")
			}
		}
	}()

	return callback()
}
