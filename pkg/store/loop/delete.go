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

	// Check if loop file exists first (avoids mount if block doesn't exist at all)
	loopFilePath := s.getLoopFilePath(hash)
	if _, err := os.Stat(loopFilePath); os.IsNotExist(err) {
		log.Debug().Str("hash", hash).Str("loop_file", loopFilePath).Msg("Loop file not found for delete")
		return store.FileNotFoundError{Hash: hash}
	} else if err != nil {
		return err
	}

	// Use withMountedLoop to handle mounting/unmounting and perform delete in single operation
	return s.withMountedLoop(hash, func() error {
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

		log.Info().Str("hash", hash).Str("file_path", filePath).Msg("File deleted successfully")
		return nil
	})
}
