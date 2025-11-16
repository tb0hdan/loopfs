package loop

import (
	"io"
	"os"
	"strings"
	"sync"

	"loopfs/pkg/log"
	"loopfs/pkg/store"
)

// Download retrieves a file by its hash and returns a temporary file path.
func (s *Store) Download(hash string) (string, error) {
	hash = strings.ToLower(hash)
	if !s.ValidateHash(hash) {
		log.Error().Str("hash", hash).Msg("Invalid hash format")
		return "", store.InvalidHashError{Hash: hash}
	}

	loopFilePath := s.getLoopFilePath(hash)

	// Acquire read lock for resize coordination before checking existence
	resizeLock := s.getResizeLock(loopFilePath)
	resizeLock.RLock()
	defer resizeLock.RUnlock()

	// Check if loop file exists (now protected by read lock)
	if _, err := os.Stat(loopFilePath); os.IsNotExist(err) {
		log.Info().Str("hash", hash).Str("loop_file", loopFilePath).Msg("Loop file not found")
		return "", store.FileNotFoundError{Hash: hash}
	} else if err != nil {
		return "", err
	}

	var tempFilePath string
	// Use withMountedLoopUnlocked since we already hold the lock
	err := s.withMountedLoopUnlocked(hash, func() error {
		filePath, err := s.findFileInLoop(hash)
		if err != nil {
			log.Info().Str("hash", hash).Msg("File not found in loop")
			return err
		}

		tempPath, err := s.copyFileToTemp(filePath, hash)
		if err != nil {
			return err
		}

		tempFilePath = tempPath
		return nil
	})

	if err != nil {
		return "", err
	}

	return tempFilePath, nil
}


// copyFileToTemp creates a temporary file and copies the source file content to it.
func (s *Store) copyFileToTemp(filePath, hash string) (string, error) {
	// Create a temporary file to copy the content
	tempFile, err := os.CreateTemp("", "cas-download-*")
	if err != nil {
		log.Error().Err(err).Msg("Failed to create temporary file for download")
		return "", err
	}
	defer func() {
		if err := tempFile.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close temporary download file")
		}
	}()

	// Copy file content to temporary file
	srcFile, err := os.Open(filePath) //nolint:gosec // filePath is constructed from validated hash, not user input
	if err != nil {
		log.Error().Err(err).Str("source_file", filePath).Msg("Failed to open source file for download")
		return "", err
	}
	defer func() {
		if err := srcFile.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to close source file")
		}
	}()

	if _, err := io.Copy(tempFile, srcFile); err != nil {
		log.Error().Err(err).Msg("Failed to copy file for download")
		if removeErr := os.Remove(tempFile.Name()); removeErr != nil {
			log.Error().Err(removeErr).Str("temp_file", tempFile.Name()).Msg("Failed to remove temporary file after copy error")
		}
		return "", err
	}

	tempFilePath := tempFile.Name()
	log.Info().Str("hash", hash).Str("temp_file", tempFilePath).Msg("File copied for download")
	return tempFilePath, nil
}

// streamingReader is a ReadCloser that manages the mount lifecycle for streaming downloads.
type streamingReader struct {
	file       *os.File
	store      *Store
	hash       string
	mountPoint string
	resizeLock *sync.RWMutex // Hold resize read lock for the duration of streaming
}

// Read implements io.Reader.
func (sr *streamingReader) Read(p []byte) (n int, err error) {
	return sr.file.Read(p)
}

// Close implements io.Closer and manages cleanup of the mount and file resources.
func (sr *streamingReader) Close() error {
	// Close the file first
	var fileErr error
	if sr.file != nil {
		fileErr = sr.file.Close()
		if fileErr != nil {
			log.Error().Err(fileErr).Str("hash", sr.hash).Msg("Failed to close file during streaming cleanup")
		}
	}

	// Decrement reference count and unmount if this was the last reference
	shouldUnmount := sr.store.decrementRefCount(sr.mountPoint)
	if shouldUnmount {
		if err := sr.store.unmountLoopFile(sr.hash); err != nil {
			log.Error().Err(err).Str("hash", sr.hash).Msg("Failed to unmount loop file during streaming cleanup")
			if fileErr == nil {
				fileErr = err
			}
		}
	}

	// Release the resize read lock now that we're done with the file
	if sr.resizeLock != nil {
		sr.resizeLock.RUnlock()
	}

	return fileErr
}

// DownloadStream retrieves a file by its hash and returns a streaming reader.
// The caller must call Close() on the returned reader to cleanup resources.
func (s *Store) DownloadStream(hash string) (io.ReadCloser, error) {
	hash = strings.ToLower(hash)
	if !s.ValidateHash(hash) {
		log.Error().Str("hash", hash).Msg("Invalid hash format")
		return nil, store.InvalidHashError{Hash: hash}
	}

	loopFilePath := s.getLoopFilePath(hash)
	mountPoint := s.getMountPoint(hash)

	// Acquire resize read lock that will be held for the duration of streaming
	// and released when the streaming reader is closed
	resizeLock := s.getResizeLock(loopFilePath)
	resizeLock.RLock()
	// Note: RUnlock is called in streamingReader.Close()

	// Check if loop file exists under lock protection
	if _, err := os.Stat(loopFilePath); os.IsNotExist(err) {
		resizeLock.RUnlock()
		log.Info().Str("hash", hash).Str("loop_file", loopFilePath).Msg("Loop file not found")
		return nil, store.FileNotFoundError{Hash: hash}
	} else if err != nil {
		resizeLock.RUnlock()
		return nil, err
	}

	// Ensure loop file exists and create if needed
	if err := s.ensureLoopFileExistsUnlocked(hash); err != nil {
		resizeLock.RUnlock()
		return nil, err
	}

	if err := s.prepareMountForStreaming(hash, mountPoint); err != nil {
		resizeLock.RUnlock()
		return nil, err
	}

	return s.openStreamingReaderWithLock(hash, mountPoint, resizeLock)
}

// ensureLoopFileExistsUnlocked handles loop file creation assuming resize lock is already held.
func (s *Store) ensureLoopFileExistsUnlocked(hash string) error {
	loopFilePath := s.getLoopFilePath(hash)

	// Get per-loop-file mutex to synchronize creation
	creationMutex := s.getCreationMutex(loopFilePath)
	creationMutex.Lock()
	defer creationMutex.Unlock()

	// Check if loop file exists, create if not (synchronized)
	if _, err := os.Stat(loopFilePath); os.IsNotExist(err) {
		if err := s.createLoopFile(hash); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	// Clean up the creation mutex to prevent memory leaks
	s.cleanupCreationMutex(loopFilePath)
	return nil
}

// prepareMountForStreaming handles mounting with reference counting.
func (s *Store) prepareMountForStreaming(hash, mountPoint string) error {
	// Increment reference count - mount only if this is the first reference
	shouldMount := s.incrementRefCount(mountPoint)
	if shouldMount {
		if err := s.mountLoopFile(hash); err != nil {
			// If mount fails, decrement the reference count we just added
			s.decrementRefCount(mountPoint)
			return err
		}
	}
	return nil
}

// openStreamingReaderWithLock opens the file and creates the streaming reader with resize lock.
func (s *Store) openStreamingReaderWithLock(hash, mountPoint string, resizeLock *sync.RWMutex) (io.ReadCloser, error) {
	// Find and open the file within the mounted loop filesystem
	filePath, err := s.findFileInLoop(hash)
	if err != nil {
		s.cleanupAfterErrorWithLock(hash, mountPoint, resizeLock)
		log.Info().Str("hash", hash).Msg("File not found in loop")
		return nil, err
	}

	//nolint:gosec // filePath is constructed from validated hash, not user input
	file, err := os.Open(filePath)
	if err != nil {
		s.cleanupAfterErrorWithLock(hash, mountPoint, resizeLock)
		log.Error().Err(err).Str("file_path", filePath).Msg("Failed to open file for streaming download")
		return nil, err
	}

	log.Info().Str("hash", hash).Str("file_path", filePath).Msg("Started streaming download")

	// Return the streaming reader that will manage cleanup and lock release
	return &streamingReader{
		file:       file,
		store:      s,
		hash:       hash,
		mountPoint: mountPoint,
		resizeLock: resizeLock,
	}, nil
}

// cleanupAfterErrorWithLock handles cleanup when streaming setup fails, including lock release.
func (s *Store) cleanupAfterErrorWithLock(hash, mountPoint string, resizeLock *sync.RWMutex) {
	shouldUnmount := s.decrementRefCount(mountPoint)
	if shouldUnmount {
		if unmountErr := s.unmountLoopFile(hash); unmountErr != nil {
			log.Error().Err(unmountErr).Str("hash", hash).Msg("Failed to unmount loop file after error")
		}
	}
	// Release the resize lock since we're not returning a streaming reader
	if resizeLock != nil {
		resizeLock.RUnlock()
	}
}
