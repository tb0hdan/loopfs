package log

import (
	"os"
	"runtime"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	// Reduced buffer size - we only need the first line which is typically ~25 bytes.
	minStackBufSize = 32
	// Minimum expected stack trace length for valid goroutine info.
	minStackTraceLen = 12
	// Number of characters to skip: "goroutine " (10 chars).
	goroutinePrefixLen = 10
)

var (
	Logger        zerolog.Logger
	goroutinePool sync.Pool // Pool for reusing small stack buffers
)

func init() {
	goroutinePool.New = func() interface{} {
		return make([]byte, minStackBufSize)
	}
}

// getGoroutineIDOptimized extracts the goroutine ID with minimal stack walking.
// This is much faster than the original implementation because:
// 1. Uses smaller buffer (32 bytes vs 64 bytes).
// 2. Reuses buffers via sync.Pool.
// 3. Optimized parsing logic.
func getGoroutineIDOptimized() string {
	bufInterface := goroutinePool.Get()
	buf, ok := bufInterface.([]byte)
	if !ok {
		return "unknown"
	}
	defer goroutinePool.Put(buf) //nolint:staticcheck // buf is a slice, this is the correct usage

	// Get only the minimal stack info needed - this is the key optimization.
	stackLen := runtime.Stack(buf, false)
	if stackLen < minStackTraceLen {
		return "unknown"
	}

	// Fast parse: "goroutine 123 [running]:".
	// Skip "goroutine " (10 chars) and parse digits only.
	idx := goroutinePrefixLen
	if idx >= stackLen {
		return "unknown"
	}

	start := idx
	// Parse digits - most goroutine IDs are 1-6 digits.
	for idx < stackLen && buf[idx] >= '0' && buf[idx] <= '9' {
		idx++
	}

	if idx > start {
		return string(buf[start:idx])
	}
	return "unknown"
}

func init() {
	// Configure zerolog with console writer for colored output
	output := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: "15:04:05",
	}

	Logger = zerolog.New(output).
		Level(zerolog.InfoLevel).
		With().
		Timestamp().
		Logger().
		Hook(zerolog.HookFunc(func(e *zerolog.Event, level zerolog.Level, msg string) {
			e.Str("goid", getGoroutineIDOptimized())
		}))

	// Set global logger
	log.Logger = Logger
}

// Info logs an info message with goroutine ID.
func Info() *zerolog.Event {
	return Logger.Info()
}

// Error logs an error message with goroutine ID.
func Error() *zerolog.Event {
	return Logger.Error()
}

// Warn logs a warning message with goroutine ID.
func Warn() *zerolog.Event {
	return Logger.Warn()
}

// Debug logs a debug message with goroutine ID.
func Debug() *zerolog.Event {
	return Logger.Debug()
}

// Fatal logs a fatal message with goroutine ID and exits.
func Fatal() *zerolog.Event {
	return Logger.Fatal()
}

// SetDebugMode switches the logger to debug level.
func SetDebugMode() {
	Logger = Logger.Level(zerolog.DebugLevel)
	log.Logger = Logger
}
