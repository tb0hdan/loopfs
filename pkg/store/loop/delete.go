package loop

import (
	"os"
	"strings"

	"loopfs/pkg/log"
	"loopfs/pkg/store"
)

// Delete removes a file with the given hash from storage.
// Optimized to use a single mount operation instead of separate existence check and delete.
func (s *Store) Delete(hash string) error {
	hash = strings.ToLower(hash)
	if !s.ValidateHash(hash) {
		log.Debug().Str("hash", hash).Msg("Invalid hash format for delete")
		return store.InvalidHashError{Hash: hash}
	}

	loopFilePath := s.getLoopFilePath(hash)

	// Acquire read lock for resize coordination before checking existence
	resizeLock := s.getResizeLock(loopFilePath)
	resizeLock.RLock()
	defer resizeLock.RUnlock()

	// Check if loop file exists (now protected by read lock)
	if _, err := os.Stat(loopFilePath); os.IsNotExist(err) {
		log.Debug().Str("hash", hash).Str("loop_file", loopFilePath).Msg("Loop file not found for delete")
		return store.FileNotFoundError{Hash: hash}
	} else if err != nil {
		return err
	}

	// Use withMountedLoopUnlocked since we already hold the lock
	return s.withMountedLoopUnlocked(hash, func() error {
		filePath, err := s.findFileInLoop(hash)
		if err != nil {
			// If findFileInLoop fails, it likely means file doesn't exist
			if os.IsNotExist(err) {
				log.Debug().Str("hash", hash).Msg("File not found in loop for delete")
				return store.FileNotFoundError{Hash: hash}
			}
			return err
		}

		// Attempt to remove the file - let os.Remove tell us if file doesn't exist
		if err := os.Remove(filePath); err != nil {
			if os.IsNotExist(err) {
				log.Debug().Str("hash", hash).Str("file_path", filePath).Msg("File not found for delete")
				return store.FileNotFoundError{Hash: hash}
			}
			log.Error().Err(err).Str("file_path", filePath).Str("hash", hash).Msg("Failed to delete file")
			return err
		}

		log.Debug().Str("hash", hash).Str("file_path", filePath).Msg("File deleted successfully")
		return nil
	})
}
