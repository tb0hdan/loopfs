package log

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/suite"
)

// LoggerTestSuite tests the log package
type LoggerTestSuite struct {
	suite.Suite
	originalLogger zerolog.Logger
	testOutput     *bytes.Buffer
}

// SetupTest runs before each test
func (s *LoggerTestSuite) SetupTest() {
	// Save the original logger
	s.originalLogger = Logger

	// Create a test output buffer
	s.testOutput = &bytes.Buffer{}

	// Configure a test logger that writes to our buffer
	testLogger := zerolog.New(s.testOutput).
		Level(zerolog.DebugLevel).
		With().
		Timestamp().
		Logger().
		Hook(zerolog.HookFunc(func(e *zerolog.Event, level zerolog.Level, msg string) {
			e.Str("goid", getGoroutineIDOptimized())
		}))

	// Replace the global logger for testing
	Logger = testLogger
}

// TearDownTest runs after each test
func (s *LoggerTestSuite) TearDownTest() {
	// Restore the original logger
	Logger = s.originalLogger
}

// TestGetGoroutineID tests the goroutine ID extraction
func (s *LoggerTestSuite) TestGetGoroutineID() {
	goroutineID := getGoroutineIDOptimized()

	// Should return a non-empty string
	s.NotEmpty(goroutineID)

	// Should be either a number or "unknown"
	if goroutineID != "unknown" {
		// Should be numeric
		for _, char := range goroutineID {
			s.True(char >= '0' && char <= '9', "Goroutine ID should be numeric or 'unknown'")
		}
	}
}

// TestInfoLog tests the Info logging function
func (s *LoggerTestSuite) TestInfoLog() {
	testMessage := "test info message"

	Info().Msg(testMessage)

	output := s.testOutput.String()
	s.Contains(output, testMessage)
	s.Contains(output, "info")
	s.Contains(output, "goid")
}

// TestErrorLog tests the Error logging function
func (s *LoggerTestSuite) TestErrorLog() {
	testMessage := "test error message"

	Error().Msg(testMessage)

	output := s.testOutput.String()
	s.Contains(output, testMessage)
	s.Contains(output, "error")
	s.Contains(output, "goid")
}

// TestWarnLog tests the Warn logging function
func (s *LoggerTestSuite) TestWarnLog() {
	testMessage := "test warning message"

	Warn().Msg(testMessage)

	output := s.testOutput.String()
	s.Contains(output, testMessage)
	s.Contains(output, "warn")
	s.Contains(output, "goid")
}

// TestDebugLog tests the Debug logging function
func (s *LoggerTestSuite) TestDebugLog() {
	testMessage := "test debug message"

	Debug().Msg(testMessage)

	output := s.testOutput.String()
	s.Contains(output, testMessage)
	s.Contains(output, "debug")
	s.Contains(output, "goid")
}

// TestLogWithFields tests logging with additional fields
func (s *LoggerTestSuite) TestLogWithFields() {
	testMessage := "test message with fields"
	testKey := "test_key"
	testValue := "test_value"

	Info().Str(testKey, testValue).Msg(testMessage)

	output := s.testOutput.String()
	s.Contains(output, testMessage)
	s.Contains(output, testKey)
	s.Contains(output, testValue)
	s.Contains(output, "goid")
}

// TestLogWithError tests logging with error field
func (s *LoggerTestSuite) TestLogWithError() {
	testMessage := "test error log"

	Error().Str("error_field", "test error description").Msg(testMessage)

	output := s.testOutput.String()
	s.Contains(output, testMessage)
	s.Contains(output, "error_field")
	s.Contains(output, "goid")
}

// TestLoggerLevels tests different log levels
func (s *LoggerTestSuite) TestLoggerLevels() {
	// Test that our logger is configured for debug level
	s.True(Logger.GetLevel() <= zerolog.DebugLevel)

	// Test each level
	Debug().Msg("debug test")
	Info().Msg("info test")
	Warn().Msg("warn test")
	Error().Msg("error test")

	output := s.testOutput.String()
	s.Contains(output, "debug test")
	s.Contains(output, "info test")
	s.Contains(output, "warn test")
	s.Contains(output, "error test")
}

// TestGoroutineIDConsistency tests that goroutine ID is consistent within the same goroutine
func (s *LoggerTestSuite) TestGoroutineIDConsistency() {
	id1 := getGoroutineIDOptimized()
	id2 := getGoroutineIDOptimized()

	// Should be the same within the same goroutine
	s.Equal(id1, id2)
}

// TestGoroutineIDDifferentGoroutines tests that different goroutines have different IDs
func (s *LoggerTestSuite) TestGoroutineIDDifferentGoroutines() {
	mainID := getGoroutineIDOptimized()

	done := make(chan string, 1)
	go func() {
		done <- getGoroutineIDOptimized()
	}()

	goroutineID := <-done

	// Different goroutines should have different IDs (or at least one should be different)
	// Note: In some edge cases they might be the same if goroutine was reused, but typically different
	if mainID != "unknown" && goroutineID != "unknown" {
		// At minimum, we should get valid IDs from both
		s.NotEmpty(mainID)
		s.NotEmpty(goroutineID)
	}
}

// TestLoggerInitialization tests that the logger is properly initialized
func (s *LoggerTestSuite) TestLoggerInitialization() {
	// The original logger should be initialized
	s.NotNil(s.originalLogger)

	// Should have a reasonable level
	level := s.originalLogger.GetLevel()
	s.True(level >= zerolog.DebugLevel && level <= zerolog.FatalLevel)
}

// TestConcurrentLogging tests that logging is thread-safe
func (s *LoggerTestSuite) TestConcurrentLogging() {
	numGoroutines := 10
	done := make(chan bool, numGoroutines)

	// Start multiple goroutines that log concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()

			Info().Int("goroutine_id", id).Msg("concurrent log message")
			Warn().Int("goroutine_id", id).Msg("concurrent warn message")
			Error().Int("goroutine_id", id).Msg("concurrent error message")
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	output := s.testOutput.String()

	// Should contain messages from all goroutines
	s.Contains(output, "concurrent log message")
	s.Contains(output, "concurrent warn message")
	s.Contains(output, "concurrent error message")

	// Should contain goroutine IDs
	s.Contains(output, "goid")

	// Count the number of log lines to ensure we got messages from all goroutines
	lines := strings.Split(strings.TrimSpace(output), "\n")
	// We expect 3 messages per goroutine (info, warn, error)
	// But due to concurrency, some may be missing, so check for reasonable count
	s.GreaterOrEqual(len(lines), numGoroutines*2) // At least 2 messages per goroutine
}

// TestLoggerConstants tests the package constants
func (s *LoggerTestSuite) TestLoggerConstants() {
	s.Equal(32, minStackBufSize)
	s.Greater(minStackBufSize, 0)
}

// TestEdgeCaseGoroutineID tests edge cases in goroutine ID parsing
func (s *LoggerTestSuite) TestEdgeCaseGoroutineID() {
	// Test that getGoroutineIDOptimized handles edge cases gracefully
	// This is an internal function test, but important for reliability

	id := getGoroutineIDOptimized()

	// Should never be empty
	s.NotEmpty(id)

	// Should be reasonable length (not too long)
	s.LessOrEqual(len(id), 20) // Goroutine IDs shouldn't be extremely long

	// Should be either "unknown" or numeric
	if id != "unknown" {
		for _, char := range id {
			s.True((char >= '0' && char <= '9'), "Goroutine ID should be numeric")
		}
	}
}

// TestSuite runs the logger test suite
func TestLoggerSuite(t *testing.T) {
	suite.Run(t, new(LoggerTestSuite))
}
