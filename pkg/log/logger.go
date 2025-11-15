package log

import (
	"os"
	"runtime"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	stackBufSize = 64
)

var Logger zerolog.Logger

// getGoroutineID extracts the goroutine ID from the stack trace.
func getGoroutineID() string {
	buf := make([]byte, stackBufSize)
	buf = buf[:runtime.Stack(buf, false)]

	// Parse "goroutine 123 [running]:"
	start := 10 // skip "goroutine "
	end := start
	for end < len(buf) && buf[end] != ' ' {
		end++
	}

	if end > start {
		return string(buf[start:end])
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
			e.Str("goid", getGoroutineID())
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
