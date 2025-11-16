package loop

import (
	"os"
	"strings"

	"loopfs/pkg/store"
)

// Exists checks if a file with the given hash exists in storage.
func (s *Store) Exists(hash string) (bool, error) {
	hash = strings.ToLower(hash)
	if !s.ValidateHash(hash) {
		return false, store.InvalidHashError{Hash: hash}
	}

	loopFilePath := s.getLoopFilePath(hash)

	// Acquire read lock for resize coordination before checking existence
	// This prevents race conditions where a resize operation temporarily renames the file
	resizeLock := s.getResizeLock(loopFilePath)
	resizeLock.RLock()
	defer resizeLock.RUnlock()

	// Check if loop file exists (now protected by read lock)
	if _, err := os.Stat(loopFilePath); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}

	var exists bool
	// Use withMountedLoopUnlocked since we already hold the resize lock
	err := s.withMountedLoopUnlocked(hash, func() error {
		filePath := s.getFilePath(hash)
		if filePath == "" {
			return store.InvalidHashError{Hash: hash}
		}

		_, err := os.Stat(filePath)
		if os.IsNotExist(err) {
			exists = false
			return nil
		}
		if err != nil {
			return err
		}

		exists = true
		return nil
	})

	return exists, err
}
