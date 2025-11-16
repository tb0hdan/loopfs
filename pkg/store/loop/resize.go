package loop

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"loopfs/pkg/log"
)

const (
	// Constants for resize operations.
	bytesPerMB       = 1024 * 1024
	rsyncTimeoutMult = 2 // Multiplier for rsync timeout
)

// createNewLoopFile creates and formats a new loop file.
func (s *Store) createNewLoopFile(newLoopFilePath string, sizeInMB int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	// Create the new loop file
	//nolint:gosec // newLoopFilePath is constructed from validated hash, not user input
	cmd := exec.CommandContext(ctx, "dd", "if=/dev/zero",
		"of="+newLoopFilePath,
		"bs="+blockSize,
		fmt.Sprintf("count=%d", sizeInMB))
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Str("new_loop_file", newLoopFilePath).Msg("Failed to create new loop file")
		return fmt.Errorf("failed to create new loop file: %w", err)
	}

	// Format the new loop file with ext4
	ctx2, cancel2 := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel2()
	//nolint:gosec // newLoopFilePath is constructed from validated hash, not user input
	cmd = exec.CommandContext(ctx2, "mkfs.ext4", "-q", newLoopFilePath)
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Str("new_loop_file", newLoopFilePath).Msg("Failed to format new loop file")
		return fmt.Errorf("failed to format new loop file: %w", err)
	}

	return nil
}

// mountNewLoopFile mounts the new loop file.
func (s *Store) mountNewLoopFile(newLoopFilePath, newMountPoint string) error {
	// Create mount point for new loop file
	if err := os.MkdirAll(newMountPoint, dirPerm); err != nil {
		log.Error().Err(err).Str("new_mount_point", newMountPoint).Msg("Failed to create new mount point")
		return fmt.Errorf("failed to create new mount point: %w", err)
	}

	// Mount the new loop file
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	//nolint:gosec // newLoopFilePath and newMountPoint are constructed from validated hash, not user input
	cmd := exec.CommandContext(ctx, "mount", "-o", "loop", newLoopFilePath, newMountPoint)
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Str("new_loop_file", newLoopFilePath).Str("new_mount_point", newMountPoint).
			Msg("Failed to mount new loop file")
		return fmt.Errorf("failed to mount new loop file: %w", err)
	}

	return nil
}

// syncDataBetweenLoops uses rsync to copy data between mounted loop filesystems.
func (s *Store) syncDataBetweenLoops(mountPoint, newMountPoint string) error {
	// Add trailing slashes to ensure directory contents are copied
	sourcePath := mountPoint + "/"
	destPath := newMountPoint + "/"

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout*rsyncTimeoutMult)
	defer cancel()
	//nolint:gosec // sourcePath and destPath are constructed from validated hash, not user input
	cmd := exec.CommandContext(ctx, "rsync", "-avuz", sourcePath, destPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Error().Err(err).Str("source", sourcePath).Str("dest", destPath).
			Str("output", string(output)).Msg("Failed to rsync data to new loop file")
		return fmt.Errorf("failed to rsync data: %w (output: %s)", err, string(output))
	}

	log.Debug().Str("source", sourcePath).Str("dest", destPath).Msg("Data synced successfully")
	return nil
}

// unmountLoopFile unmounts a specific loop file.
func (s *Store) unmountSpecificLoopFile(mountPoint string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	//nolint:gosec // mountPoint is constructed from validated hash, not user input
	cmd := exec.CommandContext(ctx, "umount", mountPoint)
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Str("mount_point", mountPoint).Msg("Failed to unmount loop file")
		return fmt.Errorf("failed to unmount loop file: %w", err)
	}
	return nil
}

// replaceOldLoopFile replaces the old loop file with the new one.
func (s *Store) replaceOldLoopFile(loopFilePath, newLoopFilePath string) error {
	// First, backup the old file just in case
	backupPath := loopFilePath + ".backup"
	if err := os.Rename(loopFilePath, backupPath); err != nil {
		log.Error().Err(err).Str("loop_file", loopFilePath).Str("backup", backupPath).
			Msg("Failed to backup existing loop file")
		return fmt.Errorf("failed to backup existing loop file: %w", err)
	}

	// Move new file to original location
	if err := os.Rename(newLoopFilePath, loopFilePath); err != nil {
		// Try to restore backup
		if restoreErr := os.Rename(backupPath, loopFilePath); restoreErr != nil {
			log.Error().Err(restoreErr).Str("backup", backupPath).Str("loop_file", loopFilePath).
				Msg("Failed to restore backup after failed rename")
		}
		log.Error().Err(err).Str("new_loop_file", newLoopFilePath).Str("loop_file", loopFilePath).
			Msg("Failed to move new loop file to original location")
		return fmt.Errorf("failed to move new loop file: %w", err)
	}

	// Remove backup file
	if err := os.Remove(backupPath); err != nil {
		// Not critical, just log it
		log.Warn().Err(err).Str("backup", backupPath).Msg("Failed to remove backup file after successful resize")
	}

	return nil
}

// validateAndPrepareResize validates the resize request and prepares paths.
func (s *Store) validateAndPrepareResize(hash string, newSize int64) (string, string, string, string, error) {
	// Validate hash
	if !s.ValidateHash(hash) {
		log.Error().Str("hash", hash).Msg("Invalid hash format for resize")
		return "", "", "", "", fmt.Errorf("invalid hash format: %s", hash)
	}

	loopFilePath := s.getLoopFilePath(hash)
	mountPoint := s.getMountPoint(hash)

	// Check if loop file exists
	if _, err := os.Stat(loopFilePath); os.IsNotExist(err) {
		log.Error().Str("hash", hash).Str("loop_file", loopFilePath).Msg("Loop file not found for resize")
		return "", "", "", "", fmt.Errorf("loop file not found: %s", loopFilePath)
	}

	// Create temporary paths for new loop file
	newLoopFilePath := loopFilePath + ".new"
	newMountPoint := mountPoint + ".new"

	log.Info().
		Str("hash", hash).
		Int64("new_size_mb", newSize/bytesPerMB).
		Str("loop_file", loopFilePath).
		Msg("Starting block resize operation")

	return loopFilePath, mountPoint, newLoopFilePath, newMountPoint, nil
}

// setupCleanupHandler sets up cleanup for temporary files.
func (s *Store) setupCleanupHandler(loopFilePath, newLoopFilePath, newMountPoint string) func() {
	return func() {
		// Clean up temporary mount point if it exists
		if _, err := os.Stat(newMountPoint); err == nil {
			if removeErr := os.RemoveAll(newMountPoint); removeErr != nil {
				log.Warn().Err(removeErr).Str("new_mount_point", newMountPoint).
					Msg("Failed to clean up temporary mount point")
			}
		}
		// Clean up temporary loop file if operation failed
		if _, err := os.Stat(newLoopFilePath); err == nil {
			if _, origErr := os.Stat(loopFilePath); origErr == nil {
				// Original still exists, so operation failed - remove the new file
				if removeErr := os.Remove(newLoopFilePath); removeErr != nil {
					log.Warn().Err(removeErr).Str("new_loop_file", newLoopFilePath).
						Msg("Failed to clean up temporary loop file")
				}
			}
		}
	}
}

// logResizeCompletion logs the final resize completion status.
func (s *Store) logResizeCompletion(hash, loopFilePath string, newSize int64) {
	if fileInfo, err := os.Stat(loopFilePath); err != nil {
		log.Warn().Err(err).Str("loop_file", loopFilePath).Msg("Failed to stat resized loop file")
	} else {
		log.Info().
			Str("hash", hash).
			Int64("new_size_bytes", fileInfo.Size()).
			Int64("requested_size_bytes", newSize).
			Msg("Block resize completed successfully")
	}
}

// performResizeOperations performs the main resize operations steps.
func (s *Store) performResizeOperations(hash, mountPoint, loopFilePath, newLoopFilePath, newMountPoint string, newSize int64) error {
	// Step 2: Create and format new image file
	sizeInMB := newSize / bytesPerMB
	if sizeInMB <= 0 {
		sizeInMB = 1 // Minimum 1MB
	}
	if err := s.createNewLoopFile(newLoopFilePath, sizeInMB); err != nil {
		return err
	}

	// Step 3: Mount new image file
	if err := s.mountNewLoopFile(newLoopFilePath, newMountPoint); err != nil {
		return err
	}
	defer func() {
		if err := s.unmountSpecificLoopFile(newMountPoint); err != nil {
			log.Error().Err(err).Str("new_mount_point", newMountPoint).
				Msg("Failed to unmount new loop file after resize")
		}
	}()

	// Step 4: Sync data between loops
	if err := s.syncDataBetweenLoops(mountPoint, newMountPoint); err != nil {
		return err
	}

	// Step 5: Unmount both loops
	if err := s.unmountSpecificLoopFile(newMountPoint); err != nil {
		return err
	}
	if err := s.unmountLoopFile(hash); err != nil {
		return fmt.Errorf("failed to unmount existing loop file: %w", err)
	}

	// Step 6: Replace old file with new one
	return s.replaceOldLoopFile(loopFilePath, newLoopFilePath)
}

// ResizeBlock resizes a loop block to accommodate more data.
// It performs the following steps:
// 1. Mounts the existing loop image
// 2. Creates a new image file with the specified size
// 3. Uses rsync to copy data from the existing to the new image
// 4. Unmounts both images
// 5. Moves the new image over the old one.
func (s *Store) ResizeBlock(hash string, newSize int64) error {
	// Validate and prepare
	loopFilePath, mountPoint, newLoopFilePath, newMountPoint, err := s.validateAndPrepareResize(hash, newSize)
	if err != nil {
		return err
	}

	// Set up cleanup handler
	defer s.setupCleanupHandler(loopFilePath, newLoopFilePath, newMountPoint)()

	// Step 1: Mount existing loop image
	if err := s.mountLoopFile(hash); err != nil {
		log.Error().Err(err).Str("hash", hash).Msg("Failed to mount existing loop file for resize")
		return fmt.Errorf("failed to mount existing loop file: %w", err)
	}
	defer func() {
		if err := s.unmountLoopFile(hash); err != nil {
			log.Error().Err(err).Str("hash", hash).Msg("Failed to unmount existing loop file after resize")
		}
	}()

	// Perform main operations
	if err := s.performResizeOperations(hash, mountPoint, loopFilePath, newLoopFilePath, newMountPoint, newSize); err != nil {
		return err
	}

	// Log completion
	s.logResizeCompletion(hash, loopFilePath, newSize)

	return nil
}
