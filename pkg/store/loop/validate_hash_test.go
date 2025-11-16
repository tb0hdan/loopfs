package loop

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

// ValidateHashTestSuite tests the ValidateHash functionality
type ValidateHashTestSuite struct {
	suite.Suite
	store *Store
}

// SetupTest runs before each test
func (s *ValidateHashTestSuite) SetupTest() {
	s.store = New("/tmp/test", 10) // temp directory and 10MB loop files
}

// TestValidateHashValid tests ValidateHash with valid hashes
func (s *ValidateHashTestSuite) TestValidateHashValid() {
	validHashes := []string{
		"a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0",
		"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		"0000000000000000000000000000000000000000000000000000000000000000",
	}

	for _, hash := range validHashes {
		s.Run("valid_"+hash[:8], func() {
			result := s.store.ValidateHash(hash)
			s.True(result, "Hash %s should be valid", hash)
		})
	}
}

// TestValidateHashInvalidLength tests ValidateHash with invalid length hashes
func (s *ValidateHashTestSuite) TestValidateHashInvalidLength() {
	invalidHashes := []string{
		"",       // empty
		"a",      // too short
		"abcdef", // too short
		"abcdef1234567890abcdef1234567890abcdef1234567890abcdef123456789",       // 63 chars
		"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890extra", // too long
		"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890123",   // 67 chars
	}

	for _, hash := range invalidHashes {
		s.Run("invalid_length_"+hash, func() {
			result := s.store.ValidateHash(hash)
			s.False(result, "Hash %s should be invalid (wrong length)", hash)
		})
	}
}

// TestValidateHashInvalidCharacters tests ValidateHash with invalid characters
func (s *ValidateHashTestSuite) TestValidateHashInvalidCharacters() {
	invalidHashes := []string{
		"g1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0", // g is invalid
		"A1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0", // uppercase A
		"a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdez0", // z is invalid
		"a1b2c3d4e5f6789012345678!abcdef0123456789abcdef0123456789abcdef0", // ! is invalid
		"a1b2c3d4e5f67890123456789@bcdef0123456789abcdef0123456789abcdef0", // @ is invalid
		"a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcde ",  // space at end
		" 1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0", // space at start
		"a1b2c3d4e5f67890123456789abcdef0123456789abcdef012345 789abcdef0", // space in middle
	}

	for _, hash := range invalidHashes {
		s.Run("invalid_chars_"+hash[:8], func() {
			result := s.store.ValidateHash(hash)
			s.False(result, "Hash %s should be invalid (invalid characters)", hash)
		})
	}
}

// TestValidateHashEdgeCases tests edge cases for ValidateHash
func (s *ValidateHashTestSuite) TestValidateHashEdgeCases() {
	testCases := []struct {
		name     string
		hash     string
		expected bool
	}{
		{"exactly_64_chars", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890", true},
		{"unicode_char", "abcdef1234567890123456789abcdef0123456789abcdef0123456789abcdéf0", false}, // é is not hex
		{"null_byte", "abcdef1234567890123456789abcdef0123456789abcdef0123456789abcd\x000", false},  // null byte
		{"tab_char", "abcdef1234567890123456789abcdef0123456789abcdef0123456789abcd\t0", false},     // tab
		{"newline_char", "abcdef1234567890123456789abcdef0123456789abcdef0123456789abcd\n0", false}, // newline
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			result := s.store.ValidateHash(tc.hash)
			s.Equal(tc.expected, result, "Hash validation for %s should be %v", tc.hash, tc.expected)
		})
	}
}

// TestValidateHashHexBoundaries tests hex character boundaries
func (s *ValidateHashTestSuite) TestValidateHashHexBoundaries() {
	testCases := []struct {
		name     string
		hash     string
		expected bool
	}{
		{"only_0s", "0000000000000000000000000000000000000000000000000000000000000000", true},
		{"only_9s", "9999999999999999999999999999999999999999999999999999999999999999", true},
		{"only_as", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true},
		{"only_fs", "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", true},
		{"with_colon", "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abc:ef0", false}, // : is after 9
		{"with_at", "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abc@ef0", false},    // @ is before A
		{"with_grave", "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abc`ef0", false}, // ` is before a
		{"with_g", "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcgef0", false},     // g is after f
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			result := s.store.ValidateHash(tc.hash)
			s.Equal(tc.expected, result, "Hash validation for %s should be %v", tc.hash, tc.expected)
		})
	}
}

// TestValidateHashPerformance tests ValidateHash performance with many calls
func (s *ValidateHashTestSuite) TestValidateHashPerformance() {
	validHash := "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"
	invalidHash := "invalid_hash"

	// Test valid hash performance
	for i := 0; i < 1000; i++ {
		result := s.store.ValidateHash(validHash)
		s.True(result)
	}

	// Test invalid hash performance
	for i := 0; i < 1000; i++ {
		result := s.store.ValidateHash(invalidHash)
		s.False(result)
	}
}

// TestValidateHashConstants tests that the function uses correct constants
func (s *ValidateHashTestSuite) TestValidateHashConstants() {
	// Test that hashLength constant is used correctly (64 for SHA256)
	exactLength := "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"
	s.Equal(64, len(exactLength))
	s.True(s.store.ValidateHash(exactLength))

	// Test length boundary
	tooShort := exactLength[:63]
	tooLong := exactLength + "0"

	s.False(s.store.ValidateHash(tooShort))
	s.False(s.store.ValidateHash(tooLong))
}

// TestValidateHashCharacterRanges tests all valid character ranges
func (s *ValidateHashTestSuite) TestValidateHashCharacterRanges() {
	// Create hash with all valid digits (0-9)
	digitHash := "0123456789012345678901234567890123456789012345678901234567890123"
	s.True(s.store.ValidateHash(digitHash))

	// Create hash with all valid lowercase letters (a-f)
	letterHash := "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"
	s.True(s.store.ValidateHash(letterHash))

	// Mix of all valid characters
	mixedHash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	s.True(s.store.ValidateHash(mixedHash))
}

// TestValidateHashConcurrency tests concurrent access to ValidateHash
func (s *ValidateHashTestSuite) TestValidateHashConcurrency() {
	validHash := "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"
	invalidHash := "invalid"

	done := make(chan bool, 20)

	// Start multiple goroutines validating hashes
	for i := 0; i < 20; i++ {
		go func(index int) {
			defer func() { done <- true }()

			// Each goroutine validates both valid and invalid hashes
			for j := 0; j < 100; j++ {
				s.True(s.store.ValidateHash(validHash))
				s.False(s.store.ValidateHash(invalidHash))
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 20; i++ {
		<-done
	}
}

// TestValidateHashSuite runs the validate hash test suite
func TestValidateHashSuite(t *testing.T) {
	suite.Run(t, new(ValidateHashTestSuite))
}
