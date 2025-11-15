package loop

import (
	"os"

	"loopfs/pkg/store"
)

// Exists checks if a file with the given hash exists in storage.
func (s *Store) Exists(hash string) (bool, error) {
	if !s.ValidateHash(hash) {
		return false, store.InvalidHashError{Hash: hash}
	}

	// Check if loop file exists first
	loopFilePath := s.getLoopFilePath(hash)
	if _, err := os.Stat(loopFilePath); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}

	var exists bool
	err := s.withMountedLoop(hash, func() error {
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
