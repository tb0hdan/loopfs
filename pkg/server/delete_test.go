package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/suite"

	"loopfs/pkg/store"
)

// DeleteTestSuite tests the delete functionality
type DeleteTestSuite struct {
	suite.Suite
	server    *CASServer
	mockStore *MockStore
	tempDir   string
}

// SetupSuite runs once before all tests
func (s *DeleteTestSuite) SetupSuite() {
	var err error
	s.tempDir, err = os.MkdirTemp("", "delete-test-*")
	s.Require().NoError(err)
}

// TearDownSuite runs once after all tests
func (s *DeleteTestSuite) TearDownSuite() {
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

// SetupTest runs before each test
func (s *DeleteTestSuite) SetupTest() {
	s.mockStore = NewMockStore()
	s.server = NewCASServer(s.tempDir, s.tempDir, "test-v1.0.0", s.mockStore)
	s.server.setupRoutes()
}

// TestDeleteFileSuccess tests successful file deletion
func (s *DeleteTestSuite) TestDeleteFileSuccess() {
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	s.mockStore.files[hash] = []byte("test content")

	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.deleteFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("File deleted successfully", response["message"])
	s.Equal(hash, response["hash"])

	// Verify file was actually deleted from mock store
	_, exists := s.mockStore.files[hash]
	s.False(exists)
}

// TestDeleteFileNotFound tests deletion when file doesn't exist
func (s *DeleteTestSuite) TestDeleteFileNotFound() {
	hash := "ffffeeee234567890abcdef1234567890abcdef1234567890abcdef123456789"

	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.deleteFile(c)
	s.NoError(err)
	s.Equal(http.StatusNotFound, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("File not found", response["error"])
}

// TestDeleteFileInvalidHash tests deletion with invalid hash
func (s *DeleteTestSuite) TestDeleteFileInvalidHash() {
	hash := "invalid_hash"

	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.deleteFile(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("Invalid hash format", response["error"])
}

// TestDeleteFileEmptyHash tests deletion with empty hash
func (s *DeleteTestSuite) TestDeleteFileEmptyHash() {
	req := httptest.NewRequest(http.MethodDelete, "/file//delete", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues("")

	err := s.server.deleteFile(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("Invalid hash format", response["error"])
}

// TestDeleteFileShortHash tests deletion with hash too short
func (s *DeleteTestSuite) TestDeleteFileShortHash() {
	hash := "abcdef"

	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.deleteFile(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("Invalid hash format", response["error"])
}

// TestDeleteFileNonHexHash tests deletion with non-hex characters in hash
func (s *DeleteTestSuite) TestDeleteFileNonHexHash() {
	hash := "gggggg1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.deleteFile(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("Invalid hash format", response["error"])
}

// TestDeleteFileUppercaseHash tests deletion with uppercase hash
func (s *DeleteTestSuite) TestDeleteFileUppercaseHash() {
	lowercaseHash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	uppercaseHash := "ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890"
	s.mockStore.files[lowercaseHash] = []byte("test content")

	req := httptest.NewRequest(http.MethodDelete, "/file/"+uppercaseHash+"/delete", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(uppercaseHash)

	err := s.server.deleteFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("File deleted successfully", response["message"])
	s.Equal(lowercaseHash, response["hash"]) // Should be normalized to lowercase

	// Verify file was deleted
	_, exists := s.mockStore.files[lowercaseHash]
	s.False(exists)
}

// TestDeleteFileMixedCaseHash tests deletion with mixed case hash
func (s *DeleteTestSuite) TestDeleteFileMixedCaseHash() {
	lowercaseHash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	mixedCaseHash := "aBcDeF1234567890AbCdEf1234567890AbCdEf1234567890AbCdEf1234567890"
	s.mockStore.files[lowercaseHash] = []byte("test content")

	req := httptest.NewRequest(http.MethodDelete, "/file/"+mixedCaseHash+"/delete", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(mixedCaseHash)

	err := s.server.deleteFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal(lowercaseHash, response["hash"])
}

// TestDeleteFileHashNormalization tests that hash normalization works correctly
func (s *DeleteTestSuite) TestDeleteFileHashNormalization() {
	testCases := []struct {
		name      string
		inputHash string
		storeHash string
	}{
		{"all_lowercase", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
		{"all_uppercase", "ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
		{"mixed_case", "AbCdEf1234567890aBcDeF1234567890AbCdEf1234567890AbCdEf1234567890", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			// Reset mock store for each test
			mockStore := NewMockStore()
			mockStore.files[tc.storeHash] = []byte("test content")
			server := NewCASServer(s.tempDir, s.tempDir, "test-v1.0.0", mockStore)
			server.setupRoutes()

			req := httptest.NewRequest(http.MethodDelete, "/file/"+tc.inputHash+"/delete", nil)
			rec := httptest.NewRecorder()
			c := server.echo.NewContext(req, rec)
			c.SetParamNames("hash")
			c.SetParamValues(tc.inputHash)

			err := server.deleteFile(c)
			s.NoError(err)
			s.Equal(http.StatusOK, rec.Code)

			var response map[string]interface{}
			err = json.Unmarshal(rec.Body.Bytes(), &response)
			s.NoError(err)
			s.Equal(tc.storeHash, response["hash"])
		})
	}
}

// MockStoreDeleteError implements a store that returns specific errors for Delete
type MockStoreDeleteError struct {
	*MockStore
	errorType string
}

func (m *MockStoreDeleteError) Delete(hash string) error {
	switch m.errorType {
	case "not_found":
		return store.FileNotFoundError{Hash: hash}
	case "invalid_hash":
		return store.InvalidHashError{Hash: hash}
	case "generic":
		return io.ErrUnexpectedEOF
	default:
		return m.MockStore.Delete(hash)
	}
}

// TestDeleteFileStoreInvalidHashError tests delete when store returns invalid hash error
func (s *DeleteTestSuite) TestDeleteFileStoreInvalidHashError() {
	mockStore := &MockStoreDeleteError{
		MockStore: NewMockStore(),
		errorType: "invalid_hash",
	}
	server := NewCASServer(s.tempDir, s.tempDir, "test-v1.0.0", mockStore)
	server.setupRoutes()

	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	c := server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := server.deleteFile(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("Invalid hash format", response["error"])
}

// TestDeleteFileStoreGenericError tests delete when store returns generic error
func (s *DeleteTestSuite) TestDeleteFileStoreGenericError() {
	mockStore := &MockStoreDeleteError{
		MockStore: NewMockStore(),
		errorType: "generic",
	}
	server := NewCASServer(s.tempDir, s.tempDir, "test-v1.0.0", mockStore)
	server.setupRoutes()

	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	c := server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := server.deleteFile(c)
	s.NoError(err)
	s.Equal(http.StatusInternalServerError, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("Internal server error", response["error"])
}

// TestDeleteFileSpecialCharacters tests delete with special characters in hash
func (s *DeleteTestSuite) TestDeleteFileSpecialCharacters() {
	testCases := []struct {
		name string
		hash string
	}{
		{"with_slash", "abcdef/234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
		{"with_space", "abcdef 234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
		{"with_plus", "abcdef+234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
		{"with_equals", "abcdef=234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
		{"with_underscore", "abcdef_234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
		{"with_hyphen", "abcdef-234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			req := httptest.NewRequest(http.MethodDelete, "/file/placeholder/delete", nil)
			rec := httptest.NewRecorder()
			c := s.server.echo.NewContext(req, rec)
			c.SetParamNames("hash")
			c.SetParamValues(tc.hash)

			err := s.server.deleteFile(c)
			s.NoError(err)
			s.Equal(http.StatusBadRequest, rec.Code)

			var response map[string]interface{}
			err = json.Unmarshal(rec.Body.Bytes(), &response)
			s.NoError(err)
			s.Equal("Invalid hash format", response["error"])
		})
	}
}

// TestDeleteFileConcurrentRequests tests concurrent delete requests
func (s *DeleteTestSuite) TestDeleteFileConcurrentRequests() {
	numRequests := 5
	results := make(chan bool, numRequests)

	// Create different files for concurrent deletion
	for i := 0; i < numRequests; i++ {
		hash := "abcdef123456789" + string(rune('0'+i)) + "abcdef1234567890abcdef1234567890abcdef123456789" + string(rune('0'+i))
		s.mockStore.files[hash] = []byte("test content " + string(rune('0'+i)))
	}

	for i := 0; i < numRequests; i++ {
		go func(index int) {
			hash := "abcdef123456789" + string(rune('0'+index)) + "abcdef1234567890abcdef1234567890abcdef123456789" + string(rune('0'+index))

			req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
			rec := httptest.NewRecorder()
			c := s.server.echo.NewContext(req, rec)
			c.SetParamNames("hash")
			c.SetParamValues(hash)

			err := s.server.deleteFile(c)
			success := (err == nil && rec.Code == http.StatusOK)
			results <- success
		}(i)
	}

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		success := <-results
		s.True(success)
	}
}

// TestDeleteFileDoubleDelete tests deleting the same file twice
func (s *DeleteTestSuite) TestDeleteFileDoubleDelete() {
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	s.mockStore.files[hash] = []byte("test content")

	// First delete should succeed
	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.deleteFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	// Second delete should fail with not found
	req = httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec = httptest.NewRecorder()
	c = s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err = s.server.deleteFile(c)
	s.NoError(err)
	s.Equal(http.StatusNotFound, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("File not found", response["error"])
}

// TestDeleteFileEdgeCases tests edge cases for hash validation
func (s *DeleteTestSuite) TestDeleteFileEdgeCases() {
	testCases := []struct {
		name           string
		hash           string
		expectedStatus int
		expectedError  string
	}{
		{
			"exactly_64_chars_all_zeros",
			"0000000000000000000000000000000000000000000000000000000000000000",
			http.StatusNotFound,
			"File not found",
		},
		{
			"exactly_64_chars_all_fs",
			"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			http.StatusNotFound,
			"File not found",
		},
		{
			"63_chars",
			"abcdef1234567890abcdef1234567890abcdef1234567890abcdef123456789",
			http.StatusBadRequest,
			"Invalid hash format",
		},
		{
			"65_chars",
			"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890a",
			http.StatusBadRequest,
			"Invalid hash format",
		},
		{
			"null_byte",
			"abcdef1234567890abcdef1234567890abcdef1234567890abcdef12345\x00890",
			http.StatusBadRequest,
			"Invalid hash format",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			req := httptest.NewRequest(http.MethodDelete, "/file/placeholder/delete", nil)
			rec := httptest.NewRecorder()
			c := s.server.echo.NewContext(req, rec)
			c.SetParamNames("hash")
			c.SetParamValues(tc.hash)

			err := s.server.deleteFile(c)
			s.NoError(err)
			s.Equal(tc.expectedStatus, rec.Code)

			if tc.expectedStatus != http.StatusOK {
				var response map[string]interface{}
				err = json.Unmarshal(rec.Body.Bytes(), &response)
				s.NoError(err)
				s.Equal(tc.expectedError, response["error"])
			}
		})
	}
}

// TestDeleteFileJSONResponse tests that the JSON response format is correct
func (s *DeleteTestSuite) TestDeleteFileJSONResponse() {
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	s.mockStore.files[hash] = []byte("test content")

	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.deleteFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	// Verify response is valid JSON
	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)

	// Verify response structure
	s.Contains(response, "message")
	s.Contains(response, "hash")
	s.IsType("", response["message"])
	s.IsType("", response["hash"])
	s.Equal("File deleted successfully", response["message"])
	s.Equal(hash, response["hash"])

	// Verify Content-Type header
	s.Equal("application/json", rec.Header().Get("Content-Type"))
}

// TestDeleteFileValidateHashCalled tests that ValidateHash is properly called
func (s *DeleteTestSuite) TestDeleteFileValidateHashCalled() {
	// Test with a hash that would pass length check but fail hex validation
	hash := "zzzzzz1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.deleteFile(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("Invalid hash format", response["error"])
}

// TestDeleteSuite runs the delete test suite
func TestDeleteSuite(t *testing.T) {
	suite.Run(t, new(DeleteTestSuite))
}
