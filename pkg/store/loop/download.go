package loop

import (
	"io"
	"os"
	"strings"
	"sync"

	"loopfs/pkg/log"
	"loopfs/pkg/store"
)

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

	// Decrement reference count; actual unmount handled via idle timeout
	sr.store.decrementRefCount(sr.mountPoint)

	// Release the resize read lock now that we're done with the file
	if sr.resizeLock != nil {
		sr.resizeLock.RUnlock()
	}

	return fileErr
}

// DownloadStream retrieves a file by its hash and returns a streaming reader.
// The caller must call Close() on the returned reader to clean up resources.
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
		log.Debug().Str("hash", hash).Str("loop_file", loopFilePath).Msg("Loop file not found")
		return nil, store.FileNotFoundError{Hash: hash}
	} else if err != nil {
		resizeLock.RUnlock()
		return nil, err
	}

	// Ensure a loop file exists and create if needed
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
			s.signalMountReady(mountPoint, err)
			// If mount fails, decrement the reference count we just added
			s.decrementRefCount(mountPoint)
			return err
		}
		s.signalMountReady(mountPoint, nil)
	} else {
		if err := s.waitForMountReady(mountPoint); err != nil {
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
		s.cleanupAfterErrorWithLock(mountPoint, resizeLock)
		log.Debug().Str("hash", hash).Msg("File not found in loop")
		return nil, err
	}

	//nolint:gosec // filePath is constructed from validated hash, not user input
	file, err := os.Open(filePath)
	if err != nil {
		s.cleanupAfterErrorWithLock(mountPoint, resizeLock)
		log.Error().Err(err).Str("file_path", filePath).Msg("Failed to open file for streaming download")
		return nil, err
	}

	log.Debug().Str("hash", hash).Str("file_path", filePath).Msg("Started streaming download")

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
func (s *Store) cleanupAfterErrorWithLock(mountPoint string, resizeLock *sync.RWMutex) {
	s.decrementRefCount(mountPoint)
	// Release the resize lock since we're not returning a streaming reader
	if resizeLock != nil {
		resizeLock.RUnlock()
	}
}
