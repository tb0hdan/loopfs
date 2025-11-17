package loop

import (
	"os"
	"strings"

	"loopfs/pkg/log"
	"loopfs/pkg/store"
)

// GetFileInfo retrieves metadata about a stored file.
func (s *Store) GetFileInfo(hash string) (*store.FileInfo, error) {
	hash = strings.ToLower(hash)
	if !s.ValidateHash(hash) {
		log.Error().Str("hash", hash).Msg("Invalid hash format")
		return nil, store.InvalidHashError{Hash: hash}
	}

	loopFilePath := s.getLoopFilePath(hash)

	// Acquire read lock for resize coordination before checking existence
	// This prevents race conditions where a resize operation temporarily renames the file
	resizeLock := s.getResizeLock(loopFilePath)
	resizeLock.RLock()
	defer resizeLock.RUnlock()

	// Check if loop file exists (now protected by read lock)
	if _, err := os.Stat(loopFilePath); os.IsNotExist(err) {
		log.Debug().Str("hash", hash).Str("loop_file", loopFilePath).Msg("Loop file not found")
		return nil, store.FileNotFoundError{Hash: hash}
	} else if err != nil {
		return nil, err
	}

	var fileInfo *store.FileInfo
	// Use withMountedLoopUnlocked since we already hold the resize lock
	err := s.withMountedLoopUnlocked(hash, func() error {
		filePath, err := s.findFileInLoop(hash)
		if err != nil {
			log.Debug().Str("hash", hash).Msg("File not found in loop")
			return err
		}

		osFileInfo, err := os.Stat(filePath)
		if err != nil {
			log.Error().Err(err).Str("file_path", filePath).Msg("Failed to get file info")
			return err
		}

		fileInfo = &store.FileInfo{
			Hash:      hash,
			Size:      osFileInfo.Size(),
			CreatedAt: osFileInfo.ModTime(),
		}

		log.Debug().Str("hash", hash).Int64("size", osFileInfo.Size()).Msg("File info retrieved")
		return nil
	})

	if err != nil {
		return nil, err
	}

	return fileInfo, nil
}
