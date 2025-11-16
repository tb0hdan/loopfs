package loop

import (
	"os"
	"strings"
	"syscall"

	"loopfs/pkg/log"
	"loopfs/pkg/store"
)

// GetDiskUsage returns disk space information for a specific file's loop filesystem.
func (s *Store) GetDiskUsage(hash string) (*store.DiskUsage, error) {
	hash = strings.ToLower(hash)

	// Validate hash
	if !s.ValidateHash(hash) {
		return nil, store.InvalidHashError{Hash: hash}
	}

	loopFilePath := s.getLoopFilePath(hash)

	// Acquire read lock for resize coordination before checking existence
	// This prevents race conditions where a resize operation temporarily renames the file
	resizeLock := s.getResizeLock(loopFilePath)
	resizeLock.RLock()
	defer resizeLock.RUnlock()

	// Check if the loop file exists (now protected by read lock)
	if _, err := os.Stat(loopFilePath); os.IsNotExist(err) {
		log.Info().Str("hash", hash).Str("loop_file", loopFilePath).Msg("Loop file not found")
		return nil, store.FileNotFoundError{Hash: hash}
	} else if err != nil {
		return nil, err
	}

	var diskUsage *store.DiskUsage

	// Use withMountedLoopUnlocked since we already hold the resize lock
	err := s.withMountedLoopUnlocked(hash, func() error {
		mountPoint := s.getMountPoint(hash)

		// Get filesystem statistics for this mount point
		var stat syscall.Statfs_t
		if err := syscall.Statfs(mountPoint, &stat); err != nil {
			log.Error().Str("mount_point", mountPoint).Err(err).Msg("Failed to get stats for mount point")
			return err
		}

		// Calculate disk usage for this loop filesystem
		// Note: Bsize is int64 on some systems, so we handle it safely
		var bsize uint64
		if stat.Bsize < 0 {
			// Should not happen in practice, but handle it gracefully
			bsize = 0
		} else {
			bsize = uint64(stat.Bsize) //nolint:gosec // Safe conversion after checking
		}

		// Calculate space for this loop filesystem
		totalSpace := stat.Blocks * bsize
		spaceAvailable := stat.Bavail * bsize
		spaceFree := stat.Bfree * bsize
		spaceUsed := totalSpace - spaceFree

		diskUsage = &store.DiskUsage{
			SpaceUsed:      int64(spaceUsed),      //nolint:gosec // Safe in practice for disk sizes
			SpaceAvailable: int64(spaceAvailable), //nolint:gosec // Safe in practice for disk sizes
			TotalSpace:     int64(totalSpace),     //nolint:gosec // Safe in practice for disk sizes
		}

		log.Debug().
			Str("hash", hash).
			Str("mount_point", mountPoint).
			Int64("used", int64(spaceUsed)).           //nolint:gosec // Safe for logging
			Int64("available", int64(spaceAvailable)). //nolint:gosec // Safe for logging
			Int64("total", int64(totalSpace)).         //nolint:gosec // Safe for logging
			Msg("Loop filesystem stats")

		return nil
	})

	if err != nil {
		return nil, err
	}

	return diskUsage, nil
}
