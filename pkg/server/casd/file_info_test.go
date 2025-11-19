package casd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"loopfs/pkg/models"
	"loopfs/pkg/store"
)

// FileInfoTestSuite tests the file info functionality
type FileInfoTestSuite struct {
	suite.Suite
	server    *CASServer
	mockStore *MockStore
	tempDir   string
}

// SetupSuite runs once before all tests
func (s *FileInfoTestSuite) SetupSuite() {
	var err error
	s.tempDir, err = os.MkdirTemp("", "fileinfo-test-*")
	s.Require().NoError(err)
}

// TearDownSuite runs once after all tests
func (s *FileInfoTestSuite) TearDownSuite() {
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

// SetupTest runs before each test
func (s *FileInfoTestSuite) SetupTest() {
	s.mockStore = NewMockStore()
	s.server = NewCASServer(s.tempDir, s.tempDir, "test-v1.0.0", s.mockStore, false, "")
	s.server.setupRoutes()
}

// TestGetFileInfoSuccess tests successful file info retrieval
func (s *FileInfoTestSuite) TestGetFileInfoSuccess() {
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	content := "test file content"
	s.mockStore.files[hash] = []byte(content)
	s.mockStore.fileInfo[hash] = &models.FileInfo{
		Hash:      hash,
		Size:      int64(len(content)),
		CreatedAt: time.Now(),
	}

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.getFileInfo(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal(hash, response["hash"])
	s.Equal(float64(len(content)), response["size"])
	s.Contains(response, "created_at")
	s.Contains(response, "space_used")
	s.Contains(response, "space_available")
	s.Equal(float64(1024), response["space_used"])
	s.Equal(float64(10240), response["space_available"])
}

// TestGetFileInfoNotFound tests file info when file doesn't exist
func (s *FileInfoTestSuite) TestGetFileInfoNotFound() {
	hash := "ffffeeee234567890abcdef1234567890abcdef1234567890abcdef123456789"

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.getFileInfo(c)
	s.NoError(err)
	s.Equal(http.StatusNotFound, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("file not found", response["error"])
}

// TestGetFileInfoInvalidHash tests file info with invalid hash
func (s *FileInfoTestSuite) TestGetFileInfoInvalidHash() {
	hash := "invalid_hash"

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.getFileInfo(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("invalid hash format", response["error"])
}

// TestGetFileInfoEmptyHash tests file info with empty hash
func (s *FileInfoTestSuite) TestGetFileInfoEmptyHash() {
	req := httptest.NewRequest(http.MethodGet, "/file//info", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues("")

	err := s.server.getFileInfo(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("invalid hash format", response["error"])
}

// TestGetFileInfoWithoutDiskUsage tests file info when disk usage fails
func (s *FileInfoTestSuite) TestGetFileInfoWithoutDiskUsage() {
	mockStore := &MockStoreFileInfoError{
		MockStore: NewMockStore(),
		errorType: "diskusage",
	}
	server := NewCASServer(s.tempDir, s.tempDir, "test-v1.0.0", mockStore, false, "")
	server.setupRoutes()

	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	content := "test content"
	mockStore.files[hash] = []byte(content)
	mockStore.fileInfo[hash] = &models.FileInfo{
		Hash:      hash,
		Size:      int64(len(content)),
		CreatedAt: time.Now(),
	}

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	c := server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := server.getFileInfo(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal(hash, response["hash"])
	s.Equal(float64(len(content)), response["size"])
	s.Contains(response, "created_at")
	// Should not contain disk usage fields when GetDiskUsage fails
	s.NotContains(response, "space_used")
	s.NotContains(response, "space_available")
}

// TestGetFileInfoShortHash tests file info with hash too short
func (s *FileInfoTestSuite) TestGetFileInfoShortHash() {
	hash := "abcdef"

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.getFileInfo(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("invalid hash format", response["error"])
}

// TestGetFileInfoNonHexHash tests file info with non-hex characters in hash
func (s *FileInfoTestSuite) TestGetFileInfoNonHexHash() {
	hash := "gggggg1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.getFileInfo(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("invalid hash format", response["error"])
}

// TestGetFileInfoUppercaseHash tests file info with uppercase hash
func (s *FileInfoTestSuite) TestGetFileInfoUppercaseHash() {
	lowercaseHash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	uppercaseHash := "ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890"
	content := "test content"
	s.mockStore.files[lowercaseHash] = []byte(content)
	s.mockStore.fileInfo[lowercaseHash] = &models.FileInfo{
		Hash:      lowercaseHash,
		Size:      int64(len(content)),
		CreatedAt: time.Now(),
	}

	req := httptest.NewRequest(http.MethodGet, "/file/"+uppercaseHash+"/info", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(uppercaseHash)

	err := s.server.getFileInfo(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal(lowercaseHash, response["hash"])
}

// TestGetFileInfoLargeFile tests file info for larger file
func (s *FileInfoTestSuite) TestGetFileInfoLargeFile() {
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	largeContent := make([]byte, 1024*1024) // 1MB
	s.mockStore.files[hash] = largeContent
	s.mockStore.fileInfo[hash] = &models.FileInfo{
		Hash:      hash,
		Size:      int64(len(largeContent)),
		CreatedAt: time.Now(),
	}

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.getFileInfo(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal(hash, response["hash"])
	s.Equal(float64(1024*1024), response["size"])
}

// TestGetFileInfoEmptyFile tests file info for empty file
func (s *FileInfoTestSuite) TestGetFileInfoEmptyFile() {
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	s.mockStore.files[hash] = []byte("")
	s.mockStore.fileInfo[hash] = &models.FileInfo{
		Hash:      hash,
		Size:      0,
		CreatedAt: time.Now(),
	}

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.getFileInfo(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal(hash, response["hash"])
	s.Equal(float64(0), response["size"])
}

// MockStoreFileInfoError implements a store that returns specific errors
type MockStoreFileInfoError struct {
	*MockStore
	errorType string
}

func (m *MockStoreFileInfoError) UploadWithHash(tempFilePath, hash, filename string) (*models.UploadResponse, error) {
	return m.MockStore.UploadWithHash(tempFilePath, hash, filename)
}

func (m *MockStoreFileInfoError) GetFileInfo(hash string) (*models.FileInfo, error) {
	switch m.errorType {
	case "not_found":
		return nil, store.FileNotFoundError{Hash: hash}
	case "invalid_hash":
		return nil, store.InvalidHashError{Hash: hash}
	case "generic":
		return nil, io.ErrUnexpectedEOF
	default:
		return m.MockStore.GetFileInfo(hash)
	}
}

func (m *MockStoreFileInfoError) GetDiskUsage(hash string) (*models.DiskUsage, error) {
	if m.errorType == "diskusage" {
		return nil, store.FileNotFoundError{Hash: hash}
	}
	return m.MockStore.GetDiskUsage(hash)
}

// TestGetFileInfoStoreError tests file info when store returns generic error
func (s *FileInfoTestSuite) TestGetFileInfoStoreError() {
	mockStore := &MockStoreFileInfoError{
		MockStore: NewMockStore(),
		errorType: "generic",
	}
	server := NewCASServer(s.tempDir, s.tempDir, "test-v1.0.0", mockStore, false, "")
	server.setupRoutes()

	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	c := server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := server.getFileInfo(c)
	s.NoError(err)
	s.Equal(http.StatusInternalServerError, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("failed to get file info", response["error"])
}

// TestGetFileInfoSpecialCharacters tests file info with special characters in hash
func (s *FileInfoTestSuite) TestGetFileInfoSpecialCharacters() {
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
			req := httptest.NewRequest(http.MethodGet, "/file/placeholder/info", nil)
			rec := httptest.NewRecorder()
			c := s.server.echo.NewContext(req, rec)
			c.SetParamNames("hash")
			c.SetParamValues(tc.hash)

			err := s.server.getFileInfo(c)
			s.NoError(err)
			s.Equal(http.StatusBadRequest, rec.Code)

			var response map[string]interface{}
			err = json.Unmarshal(rec.Body.Bytes(), &response)
			s.NoError(err)
			s.Equal("invalid hash format", response["error"])
		})
	}
}

// TestGetFileInfoConcurrentRequests tests concurrent file info requests
func (s *FileInfoTestSuite) TestGetFileInfoConcurrentRequests() {
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	content := "concurrent test content"
	s.mockStore.files[hash] = []byte(content)
	s.mockStore.fileInfo[hash] = &models.FileInfo{
		Hash:      hash,
		Size:      int64(len(content)),
		CreatedAt: time.Now(),
	}

	numRequests := 5
	results := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
			rec := httptest.NewRecorder()
			c := s.server.echo.NewContext(req, rec)
			c.SetParamNames("hash")
			c.SetParamValues(hash)

			err := s.server.getFileInfo(c)
			success := (err == nil && rec.Code == http.StatusOK)
			results <- success
		}()
	}

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		success := <-results
		s.True(success)
	}
}

// TestGetFileInfoJSONFormat tests the JSON response format
func (s *FileInfoTestSuite) TestGetFileInfoJSONFormat() {
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	content := "json format test"
	createdAt := time.Now()
	s.mockStore.files[hash] = []byte(content)
	s.mockStore.fileInfo[hash] = &models.FileInfo{
		Hash:      hash,
		Size:      int64(len(content)),
		CreatedAt: createdAt,
	}

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.getFileInfo(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	// Verify Content-Type
	s.Equal("application/json", rec.Header().Get("Content-Type"))

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)

	// Verify all expected fields are present
	s.Contains(response, "hash")
	s.Contains(response, "size")
	s.Contains(response, "created_at")
	s.Contains(response, "space_used")
	s.Contains(response, "space_available")

	// Verify field types
	s.IsType("", response["hash"])
	s.IsType(float64(0), response["size"])
	s.IsType("", response["created_at"])
	s.IsType(float64(0), response["space_used"])
	s.IsType(float64(0), response["space_available"])

	// Verify created_at is in RFC3339 format
	createdAtStr, ok := response["created_at"].(string)
	s.True(ok)
	_, err = time.Parse(time.RFC3339, createdAtStr)
	s.NoError(err)
}

// TestGetFileInfoEdgeCases tests edge cases for hash validation
func (s *FileInfoTestSuite) TestGetFileInfoEdgeCases() {
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
			"file not found",
		},
		{
			"exactly_64_chars_all_fs",
			"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			http.StatusNotFound,
			"file not found",
		},
		{
			"63_chars",
			"abcdef1234567890abcdef1234567890abcdef1234567890abcdef123456789",
			http.StatusBadRequest,
			"invalid hash format",
		},
		{
			"65_chars",
			"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890a",
			http.StatusBadRequest,
			"invalid hash format",
		},
		{
			"null_byte",
			"abcdef1234567890abcdef1234567890abcdef1234567890abcdef12345\x00890",
			http.StatusBadRequest,
			"invalid hash format",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			req := httptest.NewRequest(http.MethodGet, "/file/placeholder/info", nil)
			rec := httptest.NewRecorder()
			c := s.server.echo.NewContext(req, rec)
			c.SetParamNames("hash")
			c.SetParamValues(tc.hash)

			err := s.server.getFileInfo(c)
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

// TestGetFileInfoMixedCaseHash tests file info with mixed case hash
func (s *FileInfoTestSuite) TestGetFileInfoMixedCaseHash() {
	lowercaseHash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	mixedCaseHash := "aBcDeF1234567890AbCdEf1234567890AbCdEf1234567890AbCdEf1234567890"
	content := "mixed case test"
	s.mockStore.files[lowercaseHash] = []byte(content)
	s.mockStore.fileInfo[lowercaseHash] = &models.FileInfo{
		Hash:      lowercaseHash,
		Size:      int64(len(content)),
		CreatedAt: time.Now(),
	}

	req := httptest.NewRequest(http.MethodGet, "/file/"+mixedCaseHash+"/info", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(mixedCaseHash)

	err := s.server.getFileInfo(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal(lowercaseHash, response["hash"])
}

// TestGetFileInfoDiskUsageFields tests specific disk usage field values
func (s *FileInfoTestSuite) TestGetFileInfoDiskUsageFields() {
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	content := "disk usage test"
	s.mockStore.files[hash] = []byte(content)
	s.mockStore.fileInfo[hash] = &models.FileInfo{
		Hash:      hash,
		Size:      int64(len(content)),
		CreatedAt: time.Now(),
	}

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.getFileInfo(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)

	// Verify disk usage values match mock store expectations
	s.Equal(float64(1024), response["space_used"])
	s.Equal(float64(10240), response["space_available"])
	// Total space is not included in response, only used and available
	s.NotContains(response, "total_space")
}

// TestFileInfoSuite runs the file info test suite
func TestFileInfoSuite(t *testing.T) {
	suite.Run(t, new(FileInfoTestSuite))
}
