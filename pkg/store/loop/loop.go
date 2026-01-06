package loop

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"loopfs/pkg/log"
	"loopfs/pkg/store"
)

const (
	minHashLength = 4
	minHashSubDir = 8 // Minimum length for subdirectory structure (4 for loop + 4 for subdirectories)
	hashLength    = 64
	dirPerm       = 0750
	blockSize     = "1M"
	// Size conversion constants.
	bytesToKB            = 1024
	bytesToMB            = 1024 * 1024
	bytesToGB            = 1024 * 1024 * 1024
	dataEstimationFactor = 2 // Factor for estimating actual data size from filesystem size
	// Default timeout values.
	defaultBaseTimeoutSeconds  = 30
	defaultDDTimeoutSeconds    = 60
	defaultMkfsTimeoutSeconds  = 20
	defaultRsyncTimeoutSeconds = 120
	defaultMinLongTimeoutMins  = 5
	defaultMaxLongTimeoutMins  = 30
	defaultMountCacheTTL       = 5 * time.Minute
)

// TimeoutConfig holds configurable timeout settings for loop operations.
type TimeoutConfig struct {
	BaseCommandTimeout time.Duration // Timeout for fast operations (mount, unmount, stat)
	DDTimeoutPerGB     time.Duration // Timeout per GB for dd operations
	MkfsTimeoutPerGB   time.Duration // Timeout per GB for mkfs operations
	RsyncTimeoutPerGB  time.Duration // Timeout per GB for rsync operations
	MinLongOpTimeout   time.Duration // Minimum timeout for long operations
	MaxLongOpTimeout   time.Duration // Maximum timeout for long operations
}

// DefaultTimeoutConfig returns the default timeout configuration.
func DefaultTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{
		BaseCommandTimeout: defaultBaseTimeoutSeconds * time.Second,  // Timeout for fast operations (mount, unmount, stat)
		DDTimeoutPerGB:     defaultDDTimeoutSeconds * time.Second,    // Timeout per GB for dd operations
		MkfsTimeoutPerGB:   defaultMkfsTimeoutSeconds * time.Second,  // Timeout per GB for mkfs operations
		RsyncTimeoutPerGB:  defaultRsyncTimeoutSeconds * time.Second, // Timeout per GB for rsync operations
		MinLongOpTimeout:   defaultMinLongTimeoutMins * time.Minute,  // Minimum timeout for long operations
		MaxLongOpTimeout:   defaultMaxLongTimeoutMins * time.Minute,  // Maximum timeout for long operations
	}
}

// DefaultMountCacheTTL returns the default idle duration to keep mounts active.
func DefaultMountCacheTTL() time.Duration {
	return defaultMountCacheTTL
}

// Store implements the store.Store interface for Loop CAS storage.
type Store struct {
	storageDir         string
	tempDir            string // Directory for temporary files during uploads
	loopFileSize       int64
	timeouts           TimeoutConfig
	mountTTL           time.Duration
	syncOnWrite        bool // Whether to fsync after each file write for durability
	mountLocks         sync.Map // map[string]*sync.Mutex - per-mount-point locks for concurrent mounts
	creationLocks      sync.Map // map[string]*sync.Mutex - uses sync.Map for lock-free access
	refCounts          sync.Map // map[string]*atomic.Int64 - atomic reference counts per mount point
	timerMutex         sync.Mutex
	mountTimers        map[string]*time.Timer
	statusMutex        sync.Mutex
	mountStatuses      map[string]*mountStatus
	quiescenceMutex    sync.Mutex
	quiescenceCond     *sync.Cond // Condition variable for waiting on ref count reaching zero
	deduplicationLocks sync.Map   // map[string]*sync.Mutex - uses sync.Map for lock-free access
	resizeLocks        sync.Map   // map[string]*sync.RWMutex - uses sync.Map for lock-free access
}

type mountStatus struct {
	done chan struct{}
	err  error
}

// New creates a new Loop store with the specified storage directory, loop file size, timeout configuration,
// and mount cache TTL. The temp directory defaults to a "temp" subdirectory within the storage directory.
// syncOnWrite defaults to true for maximum durability.
func New(storageDir string, loopFileSize int64, timeouts TimeoutConfig, mountTTL time.Duration) *Store {
	return NewWithOptions(storageDir, filepath.Join(storageDir, "temp"), loopFileSize, timeouts, mountTTL, true)
}

// NewWithTempDir creates a new Loop store with an explicit temp directory for upload staging.
// This allows avoiding temp directory space limitations by using storage directory space.
// syncOnWrite defaults to true for maximum durability.
func NewWithTempDir(storageDir, tempDir string, loopFileSize int64, timeouts TimeoutConfig, mountTTL time.Duration) *Store {
	return NewWithOptions(storageDir, tempDir, loopFileSize, timeouts, mountTTL, true)
}

// NewWithOptions creates a new Loop store with all configuration options.
// syncOnWrite controls whether to call fsync after each file write for durability.
func NewWithOptions(storageDir, tempDir string, loopFileSize int64, timeouts TimeoutConfig, mountTTL time.Duration, syncOnWrite bool) *Store {
	if mountTTL <= 0 {
		mountTTL = defaultMountCacheTTL
	}

	store := &Store{
		storageDir:   storageDir,
		tempDir:      tempDir,
		loopFileSize: loopFileSize,
		timeouts:     timeouts,
		mountTTL:     mountTTL,
		syncOnWrite:  syncOnWrite,
		// creationLocks, deduplicationLocks, resizeLocks, mountLocks, and refCounts are sync.Map, no initialization needed
		mountTimers:   make(map[string]*time.Timer),
		mountStatuses: make(map[string]*mountStatus),
	}
	// Initialize quiescence condition variable with the mutex
	store.quiescenceCond = sync.NewCond(&store.quiescenceMutex)
	return store
}

// NewWithDefaults creates a new Loop store with default timeout configuration.
// syncOnWrite defaults to true for maximum durability.
func NewWithDefaults(storageDir string, loopFileSize int64) *Store {
	return New(storageDir, loopFileSize, DefaultTimeoutConfig(), DefaultMountCacheTTL())
}

// SetSyncOnWrite enables or disables fsync after each file write.
// This can be changed at runtime.
func (s *Store) SetSyncOnWrite(enabled bool) {
	s.syncOnWrite = enabled
}

// UnmountAll unmounts all currently mounted loop images.
// This is called during server shutdown to ensure clean unmounting.
func (s *Store) UnmountAll() error {
	mountPoints := s.collectMountPoints()
	return s.unmountAllMountPoints(mountPoints)
}

// ensureTempDir creates the temp directory if it doesn't exist.
func (s *Store) ensureTempDir() error {
	if err := os.MkdirAll(s.tempDir, dirPerm); err != nil {
		log.Error().Err(err).Str("temp_dir", s.tempDir).Msg("Failed to create temp directory")
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	return nil
}

// calculateTimeout calculates appropriate timeout for operations based on file size and operation type.
func (s *Store) calculateTimeout(sizeInBytes int64, timeoutPerGB time.Duration) time.Duration {
	if sizeInBytes <= 0 {
		return s.timeouts.MinLongOpTimeout
	}

	sizeInGB := float64(sizeInBytes) / bytesToGB
	timeout := time.Duration(sizeInGB * float64(timeoutPerGB))

	// Ensure timeout is within reasonable bounds
	if timeout < s.timeouts.MinLongOpTimeout {
		timeout = s.timeouts.MinLongOpTimeout
	}
	if timeout > s.timeouts.MaxLongOpTimeout {
		timeout = s.timeouts.MaxLongOpTimeout
	}

	return timeout
}

// getDDTimeout returns appropriate timeout for dd operations based on file size.
func (s *Store) getDDTimeout(sizeInBytes int64) time.Duration {
	return s.calculateTimeout(sizeInBytes, s.timeouts.DDTimeoutPerGB)
}

// getMkfsTimeout returns appropriate timeout for mkfs operations based on file size.
func (s *Store) getMkfsTimeout(sizeInBytes int64) time.Duration {
	return s.calculateTimeout(sizeInBytes, s.timeouts.MkfsTimeoutPerGB)
}

// getRsyncTimeout returns appropriate timeout for rsync operations based on estimated data size.
func (s *Store) getRsyncTimeout(sizeInBytes int64) time.Duration {
	return s.calculateTimeout(sizeInBytes, s.timeouts.RsyncTimeoutPerGB)
}

// getLoopFilePath returns the loop file path for a given hash in hierarchical structure.
func (s *Store) getLoopFilePath(hash string) string {
	if len(hash) < minHashLength {
		return ""
	}
	// Create hierarchical path: storageDir/00/01/loop.img
	dir1 := hash[:2]
	dir2 := hash[2:4]
	loopDir := filepath.Join(s.storageDir, dir1, dir2)
	return filepath.Join(loopDir, "loop.img")
}

// getMountPoint returns the mount point for a given hash based on hash prefix.
// CRITICAL: Must use same prefix as getLoopFilePath to ensure one mount per loop file.
func (s *Store) getMountPoint(hash string) string {
	if len(hash) < minHashLength {
		return ""
	}
	// Create mount point based on SAME hash prefix as loop file: data/ab/cd/loopmount
	// This ensures each loop file has exactly one mount point, preventing corruption
	dir1 := hash[:2]
	dir2 := hash[2:4]
	return filepath.Join(s.storageDir, dir1, dir2, "loopmount")
}

// getFilePath returns the file path within the mounted loop filesystem with hierarchical structure.
func (s *Store) getFilePath(hash string) string {
	if len(hash) < minHashLength {
		return ""
	}
	mountPoint := s.getMountPoint(hash)
	// Create hierarchical path within mount: mountpoint/04/05/06070809...
	// First 4 chars (00/01) are used for loop file path, remaining chars go inside
	if len(hash) < minHashSubDir {
		return ""
	}
	subDir1 := hash[4:6]
	subDir2 := hash[6:8]
	subDir := filepath.Join(subDir1, subDir2)
	// Use remaining hash chars (after first 8: 4 for loop path + 4 for subdirs) as filename
	remainingHash := hash[8:]
	return filepath.Join(mountPoint, subDir, remainingHash)
}

// getCreationMutex returns or creates a mutex for the given loop file path.
// This ensures that only one goroutine can create a loop file at a time.
// Uses sync.Map for lock-free concurrent access.
func (s *Store) getCreationMutex(loopFilePath string) *sync.Mutex {
	value, _ := s.creationLocks.LoadOrStore(loopFilePath, &sync.Mutex{})
	result, ok := value.(*sync.Mutex)
	if !ok {
		// This should never happen as we control what's stored
		result = &sync.Mutex{}
		s.creationLocks.Store(loopFilePath, result)
	}
	return result
}

// cleanupCreationMutex removes the mutex for the given loop file path.
// This prevents memory leaks from the creationLocks map.
// Safe to call even if another goroutine has a reference to the mutex.
func (s *Store) cleanupCreationMutex(loopFilePath string) {
	s.creationLocks.Delete(loopFilePath)
}

// getDeduplicationMutex returns or creates a mutex for the given hash.
// This ensures that only one goroutine can check and create a file for a specific hash at a time.
// Uses sync.Map for lock-free concurrent access.
func (s *Store) getDeduplicationMutex(hash string) *sync.Mutex {
	value, _ := s.deduplicationLocks.LoadOrStore(hash, &sync.Mutex{})
	result, ok := value.(*sync.Mutex)
	if !ok {
		// This should never happen as we control what's stored
		result = &sync.Mutex{}
		s.deduplicationLocks.Store(hash, result)
	}
	return result
}

// cleanupDeduplicationMutex removes the mutex for the given hash.
// This prevents memory leaks from the deduplicationLocks map.
// Safe to call even if another goroutine has a reference to the mutex.
func (s *Store) cleanupDeduplicationMutex(hash string) {
	s.deduplicationLocks.Delete(hash)
}

// getResizeLock returns or creates a resize lock for the given loop file path.
// This ensures that only one resize operation can occur per loop file at a time,
// and coordinates with active file operations through reference counting.
// Uses sync.Map for lock-free concurrent access.
func (s *Store) getResizeLock(loopFilePath string) *sync.RWMutex {
	value, _ := s.resizeLocks.LoadOrStore(loopFilePath, &sync.RWMutex{})
	rwLock, ok := value.(*sync.RWMutex)
	if !ok {
		// This should never happen as we control what's stored
		rwLock = &sync.RWMutex{}
		s.resizeLocks.Store(loopFilePath, rwLock)
	}
	return rwLock
}

// cleanupResizeLock removes the resize lock for the given loop file path.
// This prevents memory leaks from the resizeLocks map.
// Safe to call even if another goroutine has a reference to the lock.
func (s *Store) cleanupResizeLock(loopFilePath string) {
	s.resizeLocks.Delete(loopFilePath)
}

// getMountLock returns or creates a mutex for the given mount point.
// This allows mount/unmount operations on different mount points to proceed in parallel.
// Uses sync.Map for lock-free concurrent access.
func (s *Store) getMountLock(mountPoint string) *sync.Mutex {
	value, _ := s.mountLocks.LoadOrStore(mountPoint, &sync.Mutex{})
	mtxLock, ok := value.(*sync.Mutex)
	if !ok {
		// This should never happen as we control what's stored
		mtxLock = &sync.Mutex{}
		s.mountLocks.Store(mountPoint, mtxLock)
	}
	return mtxLock
}

func newMountStatus() *mountStatus {
	return &mountStatus{done: make(chan struct{})}
}

// signalMountReady notifies waiters that a mount attempt has completed.
func (s *Store) signalMountReady(mountPoint string, mountErr error) {
	s.statusMutex.Lock()
	defer s.statusMutex.Unlock()

	status, exists := s.mountStatuses[mountPoint]
	if !exists {
		return
	}

	status.err = mountErr
	close(status.done)
	delete(s.mountStatuses, mountPoint)
}

// waitForMountReady blocks until the in-progress mount for mountPoint completes.
func (s *Store) waitForMountReady(mountPoint string) error {
	s.statusMutex.Lock()
	status := s.mountStatuses[mountPoint]
	s.statusMutex.Unlock()

	if status == nil {
		return nil
	}

	<-status.done
	return status.err
}

// getOrCreateRefCount returns or creates an atomic counter for the given mount point.
// Uses LoadOrStore for efficient lock-free access.
func (s *Store) getOrCreateRefCount(mountPoint string) *atomic.Int64 {
	value, _ := s.refCounts.LoadOrStore(mountPoint, &atomic.Int64{})
	counter, ok := value.(*atomic.Int64)
	if !ok {
		// This should never happen as we control what's stored
		counter = &atomic.Int64{}
		s.refCounts.Store(mountPoint, counter)
	}
	return counter
}

// incrementRefCount increments the reference count for a mount point.
// Returns true if this is the first reference (mount needed), false otherwise.
// Uses atomic operations for the counter to reduce lock contention.
func (s *Store) incrementRefCount(mountPoint string) bool {
	// Stop any pending unmount timer first
	s.stopMountTimer(mountPoint)

	// Atomically increment the counter
	counter := s.getOrCreateRefCount(mountPoint)
	newCount := counter.Add(1)

	if newCount == 1 {
		// First reference - create mount status
		s.statusMutex.Lock()
		s.mountStatuses[mountPoint] = newMountStatus()
		s.statusMutex.Unlock()
		return true
	}
	return false
}

// decrementRefCount decrements the reference count for a mount point.
// Uses atomic operations for the counter to reduce lock contention.
// Broadcasts to quiescenceCond when ref count reaches zero to wake waiters.
func (s *Store) decrementRefCount(mountPoint string) {
	var unmountNow bool

	// Load the counter - if it doesn't exist, nothing to decrement
	value, exists := s.refCounts.Load(mountPoint)
	if !exists {
		return
	}

	counter, ok := value.(*atomic.Int64)
	if !ok {
		return
	}

	newCount := counter.Add(-1)
	if newCount == 0 {
		// Last reference - clean up and schedule unmount
		s.refCounts.Delete(mountPoint)

		// Signal any waiters (e.g., resize operations) that ref count is zero
		s.quiescenceCond.Broadcast()

		if s.mountTTL <= 0 {
			unmountNow = true
		} else {
			s.scheduleUnmount(mountPoint)
		}
	}

	if unmountNow {
		if err := s.unmountMountPoint(mountPoint); err != nil {
			log.Error().Err(err).Str("mount_point", mountPoint).Msg("Failed to unmount loop file during cleanup")
		}
	}
}

// getCurrentRefCount returns the current reference count for a mount point.
// Used internally for resize coordination.
func (s *Store) getCurrentRefCount(mountPoint string) int {
	value, exists := s.refCounts.Load(mountPoint)
	if !exists {
		return 0
	}

	counter, ok := value.(*atomic.Int64)
	if !ok {
		return 0
	}

	return int(counter.Load())
}

// stopMountTimer stops and removes the unmount timer for a mount point.
// Protected by timerMutex.
func (s *Store) stopMountTimer(mountPoint string) {
	s.timerMutex.Lock()
	defer s.timerMutex.Unlock()

	if timer, exists := s.mountTimers[mountPoint]; exists {
		timer.Stop()
		delete(s.mountTimers, mountPoint)
	}
}

// scheduleUnmount schedules an unmount after the mount TTL expires.
// Protected by timerMutex.
func (s *Store) scheduleUnmount(mountPoint string) {
	if s.mountTTL <= 0 {
		return
	}

	s.timerMutex.Lock()
	defer s.timerMutex.Unlock()

	if timer, exists := s.mountTimers[mountPoint]; exists {
		timer.Stop()
	}

	timer := time.AfterFunc(s.mountTTL, func() {
		s.handleMountTimeout(mountPoint)
	})
	s.mountTimers[mountPoint] = timer
}

func (s *Store) handleMountTimeout(mountPoint string) {
	// Check ref count using atomic operations
	if s.getCurrentRefCount(mountPoint) > 0 {
		return
	}

	// Clean up timer entry
	s.timerMutex.Lock()
	delete(s.mountTimers, mountPoint)
	s.timerMutex.Unlock()

	if err := s.unmountMountPoint(mountPoint); err != nil {
		log.Error().Err(err).Str("mount_point", mountPoint).Msg("Failed to unmount idle loop file")
		return
	}

	log.Debug().Str("mount_point", mountPoint).Dur("idle_ttl", s.mountTTL).Msg("Unmounted idle loop file after inactivity")
}

// findFileInLoop searches for a file in the mounted loop filesystem and returns the actual file path.
// This is needed for download since we need to verify the file exists with the truncated name.
func (s *Store) findFileInLoop(hash string) (string, error) {
	if len(hash) < minHashSubDir {
		return "", store.InvalidHashError{Hash: hash}
	}

	mountPoint := s.getMountPoint(hash)
	subDir1 := hash[4:6]
	subDir2 := hash[6:8]
	subDir := filepath.Join(mountPoint, subDir1, subDir2)

	// The filename should be the remaining hash (after first 8 chars: 4 for loop path + 4 for subdirs)
	expectedFilename := hash[8:]
	filePath := filepath.Join(subDir, expectedFilename)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", store.FileNotFoundError{Hash: hash}
	} else if err != nil {
		return "", err
	}

	return filePath, nil
}

// createLoopFile creates a new loop file and formats it with ext4.
func (s *Store) createLoopFile(hash string) error {
	loopFilePath := s.getLoopFilePath(hash)

	// Create directory structure for loop file
	loopDir := filepath.Dir(loopFilePath)
	if err := os.MkdirAll(loopDir, dirPerm); err != nil {
		log.Error().Err(err).Str("loop_dir", loopDir).Msg("Failed to create loop file directory")
		return err
	}

	// Calculate file size in bytes for timeout calculation
	fileSizeBytes := s.loopFileSize * bytesToMB // loopFileSize is in MB

	// Create the loop file with size-based timeout
	ddTimeout := s.getDDTimeout(fileSizeBytes)
	ctx, cancel := context.WithTimeout(context.Background(), ddTimeout)
	defer cancel()
	//nolint:gosec // loopFilePath is constructed from validated hash, not user input
	cmd := exec.CommandContext(ctx, "dd", "if=/dev/zero",
		"of="+loopFilePath,
		"bs="+blockSize,
		fmt.Sprintf("count=%d", s.loopFileSize))

	log.Debug().
		Str("loop_file", loopFilePath).
		Int64("size_mb", s.loopFileSize).
		Dur("timeout", ddTimeout).
		Msg("Creating loop file with calculated timeout")

	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Str("loop_file", loopFilePath).Dur("timeout", ddTimeout).Msg("Failed to create loop file")
		return err
	}

	// Format with ext4 using size-based timeout
	mkfsTimeout := s.getMkfsTimeout(fileSizeBytes)
	ctx2, cancel2 := context.WithTimeout(context.Background(), mkfsTimeout)
	defer cancel2()
	//nolint:gosec // loopFilePath is constructed from validated hash, not user input
	cmd = exec.CommandContext(ctx2, "mkfs.ext4", "-q", loopFilePath)

	log.Debug().
		Str("loop_file", loopFilePath).
		Int64("size_mb", s.loopFileSize).
		Dur("timeout", mkfsTimeout).
		Msg("Formatting loop file with calculated timeout")

	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Str("loop_file", loopFilePath).Dur("timeout", mkfsTimeout).Msg("Failed to format loop file")
		if removeErr := os.Remove(loopFilePath); removeErr != nil {
			log.Error().Err(removeErr).Str("loop_file", loopFilePath).Msg("Failed to remove loop file during cleanup")
		}
		return err
	}

	log.Debug().
		Str("loop_file", loopFilePath).
		Int64("size_mb", s.loopFileSize).
		Dur("dd_timeout", ddTimeout).
		Dur("mkfs_timeout", mkfsTimeout).
		Msg("Loop file created and formatted")
	return nil
}

// mountLoopFile mounts a loop file to its mount point.
// Uses per-mount-point locking to allow parallel mounts to different mount points.
func (s *Store) mountLoopFile(hash string) error {
	loopFilePath := s.getLoopFilePath(hash)
	mountPoint := s.getMountPoint(hash)

	// Use per-mount-point lock instead of global lock
	mountLock := s.getMountLock(mountPoint)
	mountLock.Lock()
	defer mountLock.Unlock()

	// Create mount point directory
	if err := os.MkdirAll(mountPoint, dirPerm); err != nil {
		log.Error().Err(err).Str("mount_point", mountPoint).Msg("Failed to create mount point")
		return err
	}

	// Check if already mounted
	if s.isMounted(mountPoint) {
		log.Debug().Str("mount_point", mountPoint).Msg("Loop file already mounted")
		return nil
	}

	// Mount the loop file using base timeout (mount is fast)
	ctx, cancel := context.WithTimeout(context.Background(), s.timeouts.BaseCommandTimeout)
	defer cancel()
	//nolint:gosec // loopFilePath and mountPoint are constructed from validated hash, not user input
	cmd := exec.CommandContext(ctx, "mount", "-o", "loop", loopFilePath, mountPoint)
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Str("loop_file", loopFilePath).Str("mount_point", mountPoint).Msg("Failed to mount loop file")
		return err
	}

	log.Debug().Str("loop_file", loopFilePath).Str("mount_point", mountPoint).Msg("Loop file mounted")
	return nil
}

// unmountLoopFile unmounts a loop file from its mount point.
func (s *Store) unmountLoopFile(hash string) error {
	mountPoint := s.getMountPoint(hash)
	return s.unmountMountPoint(mountPoint)
}

// unmountMountPoint unmounts a specific mount point.
// Uses per-mount-point locking to allow parallel unmounts to different mount points.
func (s *Store) unmountMountPoint(mountPoint string) error {
	// Use per-mount-point lock instead of global lock
	mountLock := s.getMountLock(mountPoint)
	mountLock.Lock()
	defer mountLock.Unlock()

	// Check if mounted
	if !s.isMounted(mountPoint) {
		log.Debug().Str("mount_point", mountPoint).Msg("Loop file not mounted")
		return nil
	}

	// Unmount using base timeout (unmount is fast)
	ctx, cancel := context.WithTimeout(context.Background(), s.timeouts.BaseCommandTimeout)
	defer cancel()
	//nolint:gosec // mountPoint is constructed from validated hash, not user input
	cmd := exec.CommandContext(ctx, "umount", mountPoint)
	if err := cmd.Run(); err != nil {
		log.Error().Err(err).Str("mount_point", mountPoint).Msg("Failed to unmount loop file")
		return err
	}

	log.Debug().Str("mount_point", mountPoint).Msg("Loop file unmounted")
	return nil
}

// isMounted checks if a mount point is currently mounted using syscall.Statfs.
// This is more efficient than forking a process to run the mountpoint command.
// It compares filesystem IDs between the mount point and its parent directory -
// if they differ, the mount point has a different filesystem mounted.
func (s *Store) isMounted(mountPoint string) bool {
	var stat1, stat2 syscall.Statfs_t

	// Get filesystem ID of the mount point
	if err := syscall.Statfs(mountPoint, &stat1); err != nil {
		return false
	}

	// Get filesystem ID of the parent directory
	parent := filepath.Dir(mountPoint)
	if err := syscall.Statfs(parent, &stat2); err != nil {
		return false
	}

	// If filesystem IDs differ, it's a mount point with something mounted
	return stat1.Fsid != stat2.Fsid
}

// withMountedLoop executes a function with the loop file mounted, ensuring cleanup.
// Uses reference counting to prevent premature unmounting when multiple operations are concurrent.
// Coordinates with resize operations to prevent conflicts.
func (s *Store) withMountedLoop(hash string, callback func() error) error {
	loopFilePath := s.getLoopFilePath(hash)
	mountPoint := s.getMountPoint(hash)

	// Acquire read lock for resize coordination - prevents resize during file operations
	resizeLock := s.getResizeLock(loopFilePath)
	resizeLock.RLock()
	defer resizeLock.RUnlock()

	// Get per-loop-file mutex to synchronize creation
	creationMutex := s.getCreationMutex(loopFilePath)
	creationMutex.Lock()

	// Ensure creation mutex is cleaned up on all paths (success or failure)
	defer s.cleanupCreationMutex(loopFilePath)

	// Check if loop file exists, create if not (synchronized)
	if _, err := os.Stat(loopFilePath); os.IsNotExist(err) {
		if err := s.createLoopFile(hash); err != nil {
			creationMutex.Unlock()
			return err
		}
	} else if err != nil {
		creationMutex.Unlock()
		return err
	}

	creationMutex.Unlock()

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
			// If mount fails, decrement the reference count we just added
			s.decrementRefCount(mountPoint)
			return err
		}
	}

	// Execute the function
	defer s.decrementRefCount(mountPoint)

	return callback()
}

// withMountedLoopUnlocked is an internal helper that performs the same operations as withMountedLoop
// but assumes the resize lock has already been acquired by the caller.
// This is used to avoid double-locking in methods that need to check existence before mounting.
// This version does NOT create the loop file if it doesn't exist.
func (s *Store) withMountedLoopUnlocked(hash string, callback func() error) error {
	loopFilePath := s.getLoopFilePath(hash)
	mountPoint := s.getMountPoint(hash)

	// Get per-loop-file mutex to synchronize mount operations
	creationMutex := s.getCreationMutex(loopFilePath)
	creationMutex.Lock()

	// Ensure creation mutex is cleaned up on all paths (success or failure)
	defer s.cleanupCreationMutex(loopFilePath)

	// Double-check loop file still exists (in case it was deleted after initial check)
	if _, err := os.Stat(loopFilePath); err != nil {
		creationMutex.Unlock()
		if os.IsNotExist(err) {
			return store.FileNotFoundError{Hash: hash}
		}
		return err
	}

	creationMutex.Unlock()

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

	// Execute the function
	defer s.decrementRefCount(mountPoint)

	return callback()
}

// collectMountPoints gathers all mount points that need to be unmounted.
func (s *Store) collectMountPoints() []string {
	// Use a map to avoid duplicates
	mountPointMap := make(map[string]bool)

	// Add mount points with non-zero reference counts from sync.Map
	s.refCounts.Range(func(key, value interface{}) bool {
		mountPoint, keyOK := key.(string)
		counter, valOK := value.(*atomic.Int64)
		if keyOK && valOK && counter.Load() > 0 {
			mountPointMap[mountPoint] = true
		}
		// Clear the entry
		s.refCounts.Delete(key)
		return true
	})

	// Add mount points with scheduled unmount timers (idle mounts)
	s.timerMutex.Lock()
	for mountPoint, timer := range s.mountTimers {
		mountPointMap[mountPoint] = true
		timer.Stop()
	}
	// Clear the timers map
	s.mountTimers = make(map[string]*time.Timer)
	s.timerMutex.Unlock()

	// Convert map to slice
	mountPoints := make([]string, 0, len(mountPointMap))
	for mountPoint := range mountPointMap {
		mountPoints = append(mountPoints, mountPoint)
	}

	return mountPoints
}

// unmountAllMountPoints unmounts a list of mount points.
func (s *Store) unmountAllMountPoints(mountPoints []string) error {
	var lastErr error
	for _, mountPoint := range mountPoints {
		log.Debug().Str("mount_point", mountPoint).Msg("Unmounting loop image during shutdown")
		if err := s.unmountMountPoint(mountPoint); err != nil {
			log.Error().Err(err).Str("mount_point", mountPoint).Msg("Failed to unmount loop image during shutdown")
			lastErr = err
			// Continue unmounting others even if one fails
		}
	}
	return lastErr
}
