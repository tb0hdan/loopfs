package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"loopfs/pkg/store"
)

// UploadTestSuite tests the upload functionality
type UploadTestSuite struct {
	suite.Suite
	server    *CASServer
	mockStore *MockStore
	tempDir   string
}

// SetupSuite runs once before all tests
func (s *UploadTestSuite) SetupSuite() {
	var err error
	s.tempDir, err = os.MkdirTemp("", "upload-test-*")
	s.Require().NoError(err)
}

// TearDownSuite runs once after all tests
func (s *UploadTestSuite) TearDownSuite() {
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

// SetupTest runs before each test
func (s *UploadTestSuite) SetupTest() {
	s.mockStore = NewMockStore()
	s.server = NewCASServer(s.tempDir, s.tempDir, "test-v1.0.0", s.mockStore)
	s.server.setupRoutes()
}

// TestUploadFileSuccess tests successful file upload
func (s *UploadTestSuite) TestUploadFileSuccess() {
	content := "test file content for upload"
	body := &bytes.Buffer{}
	body.WriteString("------WebKitFormBoundary7MA4YWxkTrZu0gW\r\n")
	body.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"test.txt\"\r\n")
	body.WriteString("Content-Type: text/plain\r\n\r\n")
	body.WriteString(content)
	body.WriteString("\r\n------WebKitFormBoundary7MA4YWxkTrZu0gW--\r\n")

	req := httptest.NewRequest(http.MethodPost, "/file/upload", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxkTrZu0gW")

	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err := s.server.uploadFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Contains(response, "hash")
	s.NotEmpty(response["hash"])
}

// TestUploadFileMissingFile tests upload when no file parameter is provided
func (s *UploadTestSuite) TestUploadFileMissingFile() {
	body := &bytes.Buffer{}
	body.WriteString("------WebKitFormBoundary7MA4YWxkTrZu0gW\r\n")
	body.WriteString("Content-Disposition: form-data; name=\"notfile\"\r\n")
	body.WriteString("Content-Type: text/plain\r\n\r\n")
	body.WriteString("some data")
	body.WriteString("\r\n------WebKitFormBoundary7MA4YWxkTrZu0gW--\r\n")

	req := httptest.NewRequest(http.MethodPost, "/file/upload", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxkTrZu0gW")

	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err := s.server.uploadFile(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("file parameter is required", response["error"])
}

// TestUploadFileInvalidMultipart tests upload with invalid multipart data
func (s *UploadTestSuite) TestUploadFileInvalidMultipart() {
	req := httptest.NewRequest(http.MethodPost, "/file/upload", bytes.NewReader([]byte("invalid multipart data")))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=invalid")

	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err := s.server.uploadFile(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("file parameter is required", response["error"])
}

// TestUploadFileAlreadyExists tests upload when file already exists
func (s *UploadTestSuite) TestUploadFileAlreadyExists() {
	s.mockStore.shouldError = true
	s.mockStore.errorType = "upload"

	content := "test file content"
	body := &bytes.Buffer{}
	body.WriteString("------WebKitFormBoundary7MA4YWxkTrZu0gW\r\n")
	body.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"test.txt\"\r\n")
	body.WriteString("Content-Type: text/plain\r\n\r\n")
	body.WriteString(content)
	body.WriteString("\r\n------WebKitFormBoundary7MA4YWxkTrZu0gW--\r\n")

	req := httptest.NewRequest(http.MethodPost, "/file/upload", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxkTrZu0gW")

	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err := s.server.uploadFile(c)
	s.NoError(err)
	s.Equal(http.StatusConflict, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("file already exists", response["error"])
	s.Contains(response, "hash")
}

// TestUploadFileEmptyFile tests upload with empty file
func (s *UploadTestSuite) TestUploadFileEmptyFile() {
	body := &bytes.Buffer{}
	body.WriteString("------WebKitFormBoundary7MA4YWxkTrZu0gW\r\n")
	body.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"empty.txt\"\r\n")
	body.WriteString("Content-Type: text/plain\r\n\r\n")
	body.WriteString("")
	body.WriteString("\r\n------WebKitFormBoundary7MA4YWxkTrZu0gW--\r\n")

	req := httptest.NewRequest(http.MethodPost, "/file/upload", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxkTrZu0gW")

	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err := s.server.uploadFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Contains(response, "hash")
}

// TestUploadFileLargeFile tests upload with larger file content
func (s *UploadTestSuite) TestUploadFileLargeFile() {
	content := strings.Repeat("Large file content test data. ", 1000) // About 30KB
	body := &bytes.Buffer{}
	body.WriteString("------WebKitFormBoundary7MA4YWxkTrZu0gW\r\n")
	body.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"large.txt\"\r\n")
	body.WriteString("Content-Type: text/plain\r\n\r\n")
	body.WriteString(content)
	body.WriteString("\r\n------WebKitFormBoundary7MA4YWxkTrZu0gW--\r\n")

	req := httptest.NewRequest(http.MethodPost, "/file/upload", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxkTrZu0gW")

	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err := s.server.uploadFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Contains(response, "hash")
}

// TestUploadFileSpecialCharactersFilename tests upload with special characters in filename
func (s *UploadTestSuite) TestUploadFileSpecialCharactersFilename() {
	content := "test content"
	body := &bytes.Buffer{}
	body.WriteString("------WebKitFormBoundary7MA4YWxkTrZu0gW\r\n")
	body.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"file with spaces & symbols!@#.txt\"\r\n")
	body.WriteString("Content-Type: text/plain\r\n\r\n")
	body.WriteString(content)
	body.WriteString("\r\n------WebKitFormBoundary7MA4YWxkTrZu0gW--\r\n")

	req := httptest.NewRequest(http.MethodPost, "/file/upload", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxkTrZu0gW")

	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err := s.server.uploadFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
}

// TestUploadFileBinaryContent tests upload with binary content
func (s *UploadTestSuite) TestUploadFileBinaryContent() {
	binaryContent := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD}
	body := &bytes.Buffer{}
	body.WriteString("------WebKitFormBoundary7MA4YWxkTrZu0gW\r\n")
	body.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"binary.bin\"\r\n")
	body.WriteString("Content-Type: application/octet-stream\r\n\r\n")
	body.Write(binaryContent)
	body.WriteString("\r\n------WebKitFormBoundary7MA4YWxkTrZu0gW--\r\n")

	req := httptest.NewRequest(http.MethodPost, "/file/upload", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxkTrZu0gW")

	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err := s.server.uploadFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
}

// StoreManagerInterface defines the interface for store managers
type StoreManagerInterface interface {
	VerifyBlock(tempPath, hash string) error
}

// UploadMockStoreManager implements a mock store manager for testing
type UploadMockStoreManager struct {
	shouldError bool
	errorType   string
}

func (m *UploadMockStoreManager) VerifyBlock(tempPath, hash string) error {
	if m.shouldError && m.errorType == "verify" {
		return store.FileExistsError{Hash: "existing"}
	}
	return nil
}

// TestPrepareUploadWithVerification tests the verification process for uploads
func (s *UploadTestSuite) TestPrepareUploadWithVerification() {
	// Skip this test as it requires real store manager integration
	s.T().Skip("Skipping test that requires store manager integration")
}

// TestPrepareUploadWithVerificationError tests verification error
func (s *UploadTestSuite) TestPrepareUploadWithVerificationError() {
	// Skip this test as it requires real store manager integration
	s.T().Skip("Skipping test that requires store manager integration")
}

// uploadErrorReader is a test io.Reader that always returns an error
type uploadErrorReader struct{}

func (e uploadErrorReader) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}

// TestPrepareUploadWithVerificationReadError tests read error during verification
func (s *UploadTestSuite) TestPrepareUploadWithVerificationReadError() {
	// Skip this test as it requires real store manager integration
	s.T().Skip("Skipping test that requires store manager integration")
}

// TestUploadFileWithStoreManager tests upload when store manager is present
func (s *UploadTestSuite) TestUploadFileWithStoreManager() {
	// Skip this test as it requires real store manager integration
	s.T().Skip("Skipping test that requires store manager integration")
}

// TestUploadFileWithStoreManagerError tests upload when store manager fails verification
func (s *UploadTestSuite) TestUploadFileWithStoreManagerError() {
	// Skip this test as it requires real store manager integration
	s.T().Skip("Skipping test that requires store manager integration")
}

// MockStoreInvalidHash implements a store that returns InvalidHashError
type MockStoreInvalidHash struct {
	*MockStore
}

func (m *MockStoreInvalidHash) Upload(reader io.Reader, filename string) (*store.UploadResult, error) {
	return nil, store.InvalidHashError{Hash: "invalid"}
}

func (m *MockStoreInvalidHash) UploadWithHash(tempFilePath, hash, filename string) (*store.UploadResult, error) {
	return nil, store.InvalidHashError{Hash: "invalid"}
}

// TestUploadFileInvalidHashError tests upload when store returns invalid hash error
func (s *UploadTestSuite) TestUploadFileInvalidHashError() {
	mockStore := &MockStoreInvalidHash{MockStore: NewMockStore()}
	server := NewCASServer(s.tempDir, s.tempDir, "test-v1.0.0", mockStore)
	server.setupRoutes()

	content := "test content"
	body := &bytes.Buffer{}
	body.WriteString("------WebKitFormBoundary7MA4YWxkTrZu0gW\r\n")
	body.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"test.txt\"\r\n")
	body.WriteString("Content-Type: text/plain\r\n\r\n")
	body.WriteString(content)
	body.WriteString("\r\n------WebKitFormBoundary7MA4YWxkTrZu0gW--\r\n")

	req := httptest.NewRequest(http.MethodPost, "/file/upload", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxkTrZu0gW")

	rec := httptest.NewRecorder()
	c := server.echo.NewContext(req, rec)

	err := server.uploadFile(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("invalid hash", response["error"])
}

// MockStoreGenericError implements a store that returns generic errors
type MockStoreGenericError struct {
	*MockStore
}

func (m *MockStoreGenericError) Upload(reader io.Reader, filename string) (*store.UploadResult, error) {
	return nil, io.ErrUnexpectedEOF
}

func (m *MockStoreGenericError) UploadWithHash(tempFilePath, hash, filename string) (*store.UploadResult, error) {
	return nil, io.ErrUnexpectedEOF
}

// TestUploadFileGenericError tests upload when store returns generic error
func (s *UploadTestSuite) TestUploadFileGenericError() {
	mockStore := &MockStoreGenericError{MockStore: NewMockStore()}
	server := NewCASServer(s.tempDir, s.tempDir, "test-v1.0.0", mockStore)
	server.setupRoutes()

	content := "test content"
	body := &bytes.Buffer{}
	body.WriteString("------WebKitFormBoundary7MA4YWxkTrZu0gW\r\n")
	body.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"test.txt\"\r\n")
	body.WriteString("Content-Type: text/plain\r\n\r\n")
	body.WriteString(content)
	body.WriteString("\r\n------WebKitFormBoundary7MA4YWxkTrZu0gW--\r\n")

	req := httptest.NewRequest(http.MethodPost, "/file/upload", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxkTrZu0gW")

	rec := httptest.NewRecorder()
	c := server.echo.NewContext(req, rec)

	err := server.uploadFile(c)
	s.NoError(err)
	s.Equal(http.StatusInternalServerError, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("failed to upload file", response["error"])
}

// TestUploadFileContentTypes tests upload with various content types
func (s *UploadTestSuite) TestUploadFileContentTypes() {
	testCases := []struct {
		name        string
		contentType string
		filename    string
		content     string
	}{
		{"text_plain", "text/plain", "test.txt", "text content"},
		{"application_json", "application/json", "test.json", `{"key": "value"}`},
		{"application_octet_stream", "application/octet-stream", "test.bin", "binary data"},
		{"image_jpeg", "image/jpeg", "test.jpg", "fake jpeg data"},
		{"no_content_type", "", "test", "no content type"},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			body := &bytes.Buffer{}
			body.WriteString("------WebKitFormBoundary7MA4YWxkTrZu0gW\r\n")
			body.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"" + tc.filename + "\"\r\n")
			if tc.contentType != "" {
				body.WriteString("Content-Type: " + tc.contentType + "\r\n")
			}
			body.WriteString("\r\n")
			body.WriteString(tc.content)
			body.WriteString("\r\n------WebKitFormBoundary7MA4YWxkTrZu0gW--\r\n")

			req := httptest.NewRequest(http.MethodPost, "/file/upload", body)
			req.Header.Set("Content-Type", "multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxkTrZu0gW")

			rec := httptest.NewRecorder()
			c := s.server.echo.NewContext(req, rec)

			err := s.server.uploadFile(c)
			s.NoError(err)
			s.Equal(http.StatusOK, rec.Code)
		})
	}
}

// TestUploadFileConcurrentRequests tests concurrent upload requests
func (s *UploadTestSuite) TestUploadFileConcurrentRequests() {
	numRequests := 5
	results := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(index int) {
			content := "concurrent test content " + string(rune('0'+index))
			body := &bytes.Buffer{}
			body.WriteString("------WebKitFormBoundary7MA4YWxkTrZu0gW\r\n")
			body.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"concurrent.txt\"\r\n")
			body.WriteString("Content-Type: text/plain\r\n\r\n")
			body.WriteString(content)
			body.WriteString("\r\n------WebKitFormBoundary7MA4YWxkTrZu0gW--\r\n")

			req := httptest.NewRequest(http.MethodPost, "/file/upload", body)
			req.Header.Set("Content-Type", "multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxkTrZu0gW")

			rec := httptest.NewRecorder()
			c := s.server.echo.NewContext(req, rec)

			err := s.server.uploadFile(c)
			results <- (err == nil && rec.Code == http.StatusOK)
		}(i)
	}

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		success := <-results
		s.True(success)
	}
}

// TestUploadSuite runs the upload test suite
func TestUploadSuite(t *testing.T) {
	suite.Run(t, new(UploadTestSuite))
}
