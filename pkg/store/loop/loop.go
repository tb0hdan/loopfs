package loop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"loopfs/pkg/log"
	"loopfs/pkg/store"
)

const (
	minHashLength = 4
	minHashSubDir = 8 // Minimum length for subdirectory structure (4 for loop + 4 for subdirs)
	hashLength    = 64
	dirPerm       = 0750
	blockSize     = "1M"
)

// Store implements the store.Store interface for Loop CAS storage.
type Store struct {
	storageDir   string
	loopFileSize int64
	mountMutex   sync.Mutex
}

// New creates a new Loop store with the specified storage directory and loop file size.
func New(storageDir string, loopFileSize int64) *Store {
	return &Store{
		storageDir:   storageDir,
		loopFileSize: loopFileSize,
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
	// Create mount point based on hash prefix: data/loop00/01
	dir1 := hash[:2]
	dir2 := hash[2:4]
	return filepath.Join(s.storageDir, fmt.Sprintf("loop%s", dir1), dir2)
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
	//nolint:gosec // loopFilePath is constructed from validated hash, not user input
	cmd := exec.Command("dd", "if=/dev/zero",
		fmt.Sprintf("of=%s", loopFilePath),
		fmt.Sprintf("bs=%s", blockSize),
		fmt.Sprintf("count=%d", s.loopFileSize))
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Str("loop_file", loopFilePath).Msg("Failed to create loop file")
		return err
	}

	// Format with ext4
	//nolint:gosec // loopFilePath is constructed from validated hash, not user input
	cmd = exec.Command("mkfs.ext4", "-q", loopFilePath)
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
	//nolint:gosec // loopFilePath and mountPoint are constructed from validated hash, not user input
	cmd := exec.Command("mount", "-o", "loop", loopFilePath, mountPoint)
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
	//nolint:gosec // mountPoint is constructed from validated hash, not user input
	cmd := exec.Command("umount", mountPoint)
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Str("mount_point", mountPoint).Msg("Failed to unmount loop file")
		return err
	}

	log.Info().Str("mount_point", mountPoint).Msg("Loop file unmounted")
	return nil
}

// isMounted checks if a mount point is currently mounted.
func (s *Store) isMounted(mountPoint string) bool {
	cmd := exec.Command("mountpoint", "-q", mountPoint)
	return cmd.Run() == nil
}

// withMountedLoop executes a function with the loop file mounted, ensuring cleanup.
func (s *Store) withMountedLoop(hash string, callback func() error) error {
	loopFilePath := s.getLoopFilePath(hash)

	// Check if loop file exists, create if not
	if _, err := os.Stat(loopFilePath); os.IsNotExist(err) {
		if err := s.createLoopFile(hash); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	// Mount the loop file
	if err := s.mountLoopFile(hash); err != nil {
		return err
	}

	// Execute the function
	defer func() {
		if err := s.unmountLoopFile(hash); err != nil {
			log.Error().Err(err).Str("hash", hash).Msg("Failed to unmount loop file during cleanup")
		}
	}()

	return callback()
}
