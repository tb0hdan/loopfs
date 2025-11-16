package loop

import (
	"os"
	"strings"

	"loopfs/pkg/log"
	"loopfs/pkg/store"
)

// Delete removes a file with the given hash from storage.
func (s *Store) Delete(hash string) error {
	hash = strings.ToLower(hash)
	if !s.ValidateHash(hash) {
		log.Debug().Str("hash", hash).Msg("Invalid hash format for delete")
		return store.InvalidHashError{Hash: hash}
	}

	// First check if the file exists before attempting to mount
	exists, err := s.Exists(hash)
	if err != nil {
		return err
	}
	if !exists {
		log.Debug().Str("hash", hash).Msg("File not found for delete")
		return store.FileNotFoundError{Hash: hash}
	}

	// Use withMountedLoop to handle mounting/unmounting
	return s.withMountedLoop(hash, func() error {
		filePath, err := s.findFileInLoop(hash)
		if err != nil {
			return err
		}

		// Remove the file
		if err := os.Remove(filePath); err != nil {
			log.Error().Err(err).Str("file_path", filePath).Str("hash", hash).Msg("Failed to delete file")
			return err
		}

		log.Info().Str("hash", hash).Str("file_path", filePath).Msg("File deleted successfully")
		return nil
	})
}
