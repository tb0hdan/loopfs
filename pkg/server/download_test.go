package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/suite"

	"loopfs/pkg/store"
)

// DownloadTestSuite tests the download functionality
type DownloadTestSuite struct {
	suite.Suite
	server    *CASServer
	mockStore *MockStore
	tempDir   string
}

// SetupSuite runs once before all tests
func (s *DownloadTestSuite) SetupSuite() {
	var err error
	s.tempDir, err = os.MkdirTemp("", "download-test-*")
	s.Require().NoError(err)
}

// TearDownSuite runs once after all tests
func (s *DownloadTestSuite) TearDownSuite() {
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

// SetupTest runs before each test
func (s *DownloadTestSuite) SetupTest() {
	s.mockStore = NewMockStore()
	s.server = NewCASServer(s.tempDir, s.tempDir, "test-v1.0.0", s.mockStore)
	s.server.setupRoutes()
}

// TestDownloadFileSuccess tests successful file download
func (s *DownloadTestSuite) TestDownloadFileSuccess() {
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	content := "test file content for download"
	s.mockStore.files[hash] = []byte(content)

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.downloadFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Equal("application/octet-stream", rec.Header().Get("Content-Type"))
	s.Equal(content, rec.Body.String())
}

// TestDownloadFileNotFound tests download when file doesn't exist
func (s *DownloadTestSuite) TestDownloadFileNotFound() {
	hash := "ffffeeee234567890abcdef1234567890abcdef1234567890abcdef123456789"

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.downloadFile(c)
	s.NoError(err)
	s.Equal(http.StatusNotFound, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("file not found", response["error"])
}

// TestDownloadFileInvalidHash tests download with invalid hash
func (s *DownloadTestSuite) TestDownloadFileInvalidHash() {
	hash := "invalid_hash"

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.downloadFile(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("invalid hash format", response["error"])
}

// TestDownloadFileEmptyContent tests download of empty file
func (s *DownloadTestSuite) TestDownloadFileEmptyContent() {
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	s.mockStore.files[hash] = []byte("")

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.downloadFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Equal("", rec.Body.String())
}

// TestDownloadFileLargeContent tests download of larger file
func (s *DownloadTestSuite) TestDownloadFileLargeContent() {
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	largeContent := make([]byte, 10*1024) // 10KB
	for i := range largeContent {
		largeContent[i] = byte(i % 256)
	}
	s.mockStore.files[hash] = largeContent

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.downloadFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Equal(largeContent, rec.Body.Bytes())
}

// TestDownloadFileBinaryContent tests download of binary content
func (s *DownloadTestSuite) TestDownloadFileBinaryContent() {
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	binaryContent := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD}
	s.mockStore.files[hash] = binaryContent

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.downloadFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Equal(binaryContent, rec.Body.Bytes())
}

// TestDownloadFileUppercaseHash tests download with uppercase hash
func (s *DownloadTestSuite) TestDownloadFileUppercaseHash() {
	lowercaseHash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	uppercaseHash := "ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890"
	content := "test content"
	s.mockStore.files[lowercaseHash] = []byte(content)

	req := httptest.NewRequest(http.MethodGet, "/file/"+uppercaseHash+"/download", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(uppercaseHash)

	err := s.server.downloadFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Equal(content, rec.Body.String())
}

// TestDownloadFileShortHash tests download with hash too short
func (s *DownloadTestSuite) TestDownloadFileShortHash() {
	hash := "abcdef"

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.downloadFile(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("invalid hash format", response["error"])
}

// TestDownloadFileNonHexHash tests download with non-hex characters in hash
func (s *DownloadTestSuite) TestDownloadFileNonHexHash() {
	hash := "gggggg1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.downloadFile(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("invalid hash format", response["error"])
}

// MockStoreDownloadError implements a store that returns errors for DownloadStream
type MockStoreDownloadError struct {
	*MockStore
	errorType string
}

func (m *MockStoreDownloadError) DownloadStream(hash string) (io.ReadCloser, error) {
	switch m.errorType {
	case "not_found":
		return nil, store.FileNotFoundError{Hash: hash}
	case "invalid_hash":
		return nil, store.InvalidHashError{Hash: hash}
	case "generic":
		return nil, io.ErrUnexpectedEOF
	default:
		return m.MockStore.DownloadStream(hash)
	}
}

// TestDownloadFileStoreError tests download when store returns generic error
func (s *DownloadTestSuite) TestDownloadFileStoreError() {
	mockStore := &MockStoreDownloadError{
		MockStore: NewMockStore(),
		errorType: "generic",
	}
	server := NewCASServer(s.tempDir, s.tempDir, "test-v1.0.0", mockStore)
	server.setupRoutes()

	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	c := server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := server.downloadFile(c)
	s.NoError(err)
	s.Equal(http.StatusInternalServerError, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("failed to download file", response["error"])
}

// MockCloseErrorReader implements a ReadCloser that returns error on Close
type MockCloseErrorReader struct {
	*bytes.Reader
}

func (m *MockCloseErrorReader) Close() error {
	return io.ErrUnexpectedEOF
}

// MockStoreCloseError implements a store that returns readers that error on close
type MockStoreCloseError struct {
	*MockStore
}

func (m *MockStoreCloseError) DownloadStream(hash string) (io.ReadCloser, error) {
	data, exists := m.files[hash]
	if !exists {
		return nil, store.FileNotFoundError{Hash: hash}
	}
	return &MockCloseErrorReader{Reader: bytes.NewReader(data)}, nil
}

// TestDownloadFileCloseError tests download when reader close fails
func (s *DownloadTestSuite) TestDownloadFileCloseError() {
	mockStore := &MockStoreCloseError{MockStore: NewMockStore()}
	server := NewCASServer(s.tempDir, s.tempDir, "test-v1.0.0", mockStore)
	server.setupRoutes()

	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	content := "test content"
	mockStore.files[hash] = []byte(content)

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	c := server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := server.downloadFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Equal(content, rec.Body.String())
	// Close error is logged but doesn't affect the response
}

// TestDownloadFileConcurrentRequests tests concurrent download requests
func (s *DownloadTestSuite) TestDownloadFileConcurrentRequests() {
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	content := "concurrent download test content"
	s.mockStore.files[hash] = []byte(content)

	numRequests := 5
	results := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
			rec := httptest.NewRecorder()
			c := s.server.echo.NewContext(req, rec)
			c.SetParamNames("hash")
			c.SetParamValues(hash)

			err := s.server.downloadFile(c)
			success := (err == nil && rec.Code == http.StatusOK && rec.Body.String() == content)
			results <- success
		}()
	}

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		success := <-results
		s.True(success)
	}
}

// TestDownloadFileEmptyHash tests download with empty hash parameter
func (s *DownloadTestSuite) TestDownloadFileEmptyHash() {
	req := httptest.NewRequest(http.MethodGet, "/file//download", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues("")

	err := s.server.downloadFile(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("invalid hash format", response["error"])
}

// TestDownloadFileSpecialCharacters tests download with special characters in hash
func (s *DownloadTestSuite) TestDownloadFileSpecialCharacters() {
	testCases := []struct {
		name string
		hash string
	}{
		{"with_slash", "abcdef/234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
		{"with_space", "abcdef 234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
		{"with_plus", "abcdef+234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
		{"with_equals", "abcdef=234567890abcdef1234567890abcdef1234567890abcdef1234567890"},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			req := httptest.NewRequest(http.MethodGet, "/file/placeholder/download", nil)
			rec := httptest.NewRecorder()
			c := s.server.echo.NewContext(req, rec)
			c.SetParamNames("hash")
			c.SetParamValues(tc.hash)

			err := s.server.downloadFile(c)
			s.NoError(err)
			s.Equal(http.StatusBadRequest, rec.Code)

			var response map[string]interface{}
			err = json.Unmarshal(rec.Body.Bytes(), &response)
			s.NoError(err)
			s.Equal("invalid hash format", response["error"])
		})
	}
}

// TestDownloadFileWithHTTPHeaders tests download with various HTTP headers
func (s *DownloadTestSuite) TestDownloadFileWithHTTPHeaders() {
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	content := "test content with headers"
	s.mockStore.files[hash] = []byte(content)

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", "Test-Agent/1.0")

	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.downloadFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Equal(content, rec.Body.String())
	s.Equal("application/octet-stream", rec.Header().Get("Content-Type"))
}

// TestDownloadFileMixedCaseHash tests download with mixed case hash
func (s *DownloadTestSuite) TestDownloadFileMixedCaseHash() {
	lowercaseHash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	mixedCaseHash := "aBcDeF1234567890AbCdEf1234567890AbCdEf1234567890AbCdEf1234567890"
	content := "mixed case test"
	s.mockStore.files[lowercaseHash] = []byte(content)

	req := httptest.NewRequest(http.MethodGet, "/file/"+mixedCaseHash+"/download", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(mixedCaseHash)

	err := s.server.downloadFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Equal(content, rec.Body.String())
}

// TestDownloadFileErrorHandling tests various error conditions comprehensively
func (s *DownloadTestSuite) TestDownloadFileErrorHandling() {
	testCases := []struct {
		name           string
		hash           string
		setupStore     func(*MockStore)
		expectedStatus int
		expectedError  string
	}{
		{
			"valid_hash_not_found",
			"bbbbbb1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			func(ms *MockStore) {},
			http.StatusNotFound,
			"file not found",
		},
		{
			"empty_hash",
			"",
			func(ms *MockStore) {},
			http.StatusBadRequest,
			"invalid hash format",
		},
		{
			"too_short_hash",
			"abc123",
			func(ms *MockStore) {},
			http.StatusBadRequest,
			"invalid hash format",
		},
		{
			"too_long_hash",
			"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890extra",
			func(ms *MockStore) {},
			http.StatusBadRequest,
			"invalid hash format",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			mockStore := NewMockStore()
			tc.setupStore(mockStore)
			server := NewCASServer(s.tempDir, s.tempDir, "test-v1.0.0", mockStore)
			server.setupRoutes()

			req := httptest.NewRequest(http.MethodGet, "/file/"+tc.hash+"/download", nil)
			rec := httptest.NewRecorder()
			c := server.echo.NewContext(req, rec)
			c.SetParamNames("hash")
			c.SetParamValues(tc.hash)

			err := server.downloadFile(c)
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

// TestDownloadSuite runs the download test suite
func TestDownloadSuite(t *testing.T) {
	suite.Run(t, new(DownloadTestSuite))
}
