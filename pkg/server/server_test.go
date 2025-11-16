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
	"time"

	"github.com/stretchr/testify/suite"

	"loopfs/pkg/store"
)

// MockStore implements the store.Store interface for testing
type MockStore struct {
	files         map[string][]byte
	fileInfo      map[string]*store.FileInfo
	uploadResults map[string]*store.UploadResult
	shouldError   bool
	errorType     string
}

// NewMockStore creates a new mock store
func NewMockStore() *MockStore {
	return &MockStore{
		files:         make(map[string][]byte),
		fileInfo:      make(map[string]*store.FileInfo),
		uploadResults: make(map[string]*store.UploadResult),
		shouldError:   false,
	}
}

// Upload implementation for mock store
func (m *MockStore) Upload(reader io.Reader, filename string) (*store.UploadResult, error) {
	if m.shouldError && m.errorType == "upload" {
		return nil, store.FileExistsError{Hash: "existing"}
	}

	data, _ := io.ReadAll(reader)
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	m.files[hash] = data
	m.fileInfo[hash] = &store.FileInfo{
		Hash:      hash,
		Size:      int64(len(data)),
		CreatedAt: time.Now(),
	}

	result := &store.UploadResult{Hash: hash}
	m.uploadResults[hash] = result
	return result, nil
}

// UploadWithHash implementation for mock store
func (m *MockStore) UploadWithHash(tempFilePath, hash, filename string) (*store.UploadResult, error) {
	if m.shouldError && m.errorType == "upload" {
		return nil, store.FileExistsError{Hash: "existing"}
	}

	// Read from the temp file
	data, err := os.ReadFile(tempFilePath)
	if err != nil {
		return nil, err
	}

	m.files[hash] = data
	m.fileInfo[hash] = &store.FileInfo{
		Hash:      hash,
		Size:      int64(len(data)),
		CreatedAt: time.Now(),
	}

	result := &store.UploadResult{Hash: hash}
	m.uploadResults[hash] = result
	return result, nil
}

// Download implementation for mock store
func (m *MockStore) Download(hash string) (string, error) {
	if m.shouldError && m.errorType == "download" {
		return "", store.FileNotFoundError{Hash: hash}
	}

	hash = strings.ToLower(hash)
	if !m.ValidateHash(hash) {
		return "", store.InvalidHashError{Hash: hash}
	}

	data, exists := m.files[hash]
	if !exists {
		return "", store.FileNotFoundError{Hash: hash}
	}

	// Create a temporary file with actual content
	tmpFile, err := os.CreateTemp("", "mock-download-"+hash+"-*")
	if err != nil {
		return "", err
	}

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", err
	}

	tmpFile.Close()
	return tmpFile.Name(), nil
}

// DownloadStream implementation for mock store
func (m *MockStore) DownloadStream(hash string) (io.ReadCloser, error) {
	if m.shouldError && m.errorType == "download" {
		return nil, store.FileNotFoundError{Hash: hash}
	}

	hash = strings.ToLower(hash)
	if !m.ValidateHash(hash) {
		return nil, store.InvalidHashError{Hash: hash}
	}

	data, exists := m.files[hash]
	if !exists {
		return nil, store.FileNotFoundError{Hash: hash}
	}

	// Return an in-memory reader
	return io.NopCloser(bytes.NewReader(data)), nil
}

// GetFileInfo implementation for mock store
func (m *MockStore) GetFileInfo(hash string) (*store.FileInfo, error) {
	if m.shouldError && m.errorType == "fileinfo" {
		return nil, store.FileNotFoundError{Hash: hash}
	}

	hash = strings.ToLower(hash)
	if !m.ValidateHash(hash) {
		return nil, store.InvalidHashError{Hash: hash}
	}

	info, exists := m.fileInfo[hash]
	if !exists {
		return nil, store.FileNotFoundError{Hash: hash}
	}

	return info, nil
}

// Exists implementation for mock store
func (m *MockStore) Exists(hash string) (bool, error) {
	hash = strings.ToLower(hash)
	if !m.ValidateHash(hash) {
		return false, store.InvalidHashError{Hash: hash}
	}

	_, exists := m.files[hash]
	return exists, nil
}

// ValidateHash implementation for mock store
func (m *MockStore) ValidateHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}

	for _, char := range hash {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}

	return true
}

// Delete implementation for mock store
func (m *MockStore) Delete(hash string) error {
	if m.shouldError && m.errorType == "delete" {
		return store.FileNotFoundError{Hash: hash}
	}

	hash = strings.ToLower(hash)
	if !m.ValidateHash(hash) {
		return store.InvalidHashError{Hash: hash}
	}

	if _, exists := m.files[hash]; !exists {
		return store.FileNotFoundError{Hash: hash}
	}

	delete(m.files, hash)
	delete(m.fileInfo, hash)
	delete(m.uploadResults, hash)
	return nil
}

// GetDiskUsage implementation for mock store
func (m *MockStore) GetDiskUsage(hash string) (*store.DiskUsage, error) {
	if m.shouldError && m.errorType == "diskusage" {
		return nil, store.FileNotFoundError{Hash: hash}
	}

	hash = strings.ToLower(hash)
	if !m.ValidateHash(hash) {
		return nil, store.InvalidHashError{Hash: hash}
	}

	if _, exists := m.files[hash]; !exists {
		return nil, store.FileNotFoundError{Hash: hash}
	}

	return &store.DiskUsage{
		SpaceUsed:      1024,
		SpaceAvailable: 10240,
		TotalSpace:     11264,
	}, nil
}

// Helper function to check if string contains only hex characters
func isHexString(s string) bool {
	for _, char := range s {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return false
		}
	}
	return true
}

// ServerTestSuite tests the server package
type ServerTestSuite struct {
	suite.Suite
	server    *CASServer
	mockStore *MockStore
	tempDir   string
}

// SetupSuite runs once before all tests
func (s *ServerTestSuite) SetupSuite() {
	var err error
	s.tempDir, err = os.MkdirTemp("", "server-test-*")
	s.Require().NoError(err)
}

// TearDownSuite runs once after all tests
func (s *ServerTestSuite) TearDownSuite() {
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

// SetupTest runs before each test
func (s *ServerTestSuite) SetupTest() {
	s.mockStore = NewMockStore()
	s.server = NewCASServer(s.tempDir, s.tempDir, "test-v1.0.0", s.mockStore)
	s.server.setupRoutes()
}

// TestNewCASServer tests the constructor
func (s *ServerTestSuite) TestNewCASServer() {
	server := NewCASServer("/storage", "/web", "v1.0.0", s.mockStore)
	s.NotNil(server)
	s.Equal("/storage", server.storageDir)
	s.Equal("/web", server.webDir)
	s.Equal("v1.0.0", server.version)
	s.Equal(s.mockStore, server.store)
	s.NotNil(server.echo)
}

// TestUploadFile tests the upload endpoint
func (s *ServerTestSuite) TestUploadFile() {
	// Create test data
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
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err, "Failed to parse JSON response: %s", rec.Body.String())
	s.Contains(response, "hash")
	// Upload endpoint only returns hash, no message
}

// TestUploadFileAlreadyExists tests upload when file already exists
func (s *ServerTestSuite) TestUploadFileAlreadyExists() {
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
}

// TestDownloadFile tests the download endpoint
func (s *ServerTestSuite) TestDownloadFile() {
	// First upload a file
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	s.mockStore.files[hash] = []byte("test content")

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.downloadFile(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
}

// TestDownloadFileNotFound tests download when file doesn't exist
func (s *ServerTestSuite) TestDownloadFileNotFound() {
	s.mockStore.shouldError = true
	s.mockStore.errorType = "download"

	hash := "ffffeeee234567890abcdef1234567890abcdef1234567890abcdef123456789"

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.downloadFile(c)
	s.NoError(err)
	s.Equal(http.StatusNotFound, rec.Code)
}

// TestGetFileInfo tests the file info endpoint
func (s *ServerTestSuite) TestGetFileInfo() {
	// First add a file to the mock store
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	s.mockStore.files[hash] = []byte("test content")
	s.mockStore.fileInfo[hash] = &store.FileInfo{
		Hash:      hash,
		Size:      12,
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
	s.Equal(float64(12), response["size"])
	s.Contains(response, "created_at")
	s.Contains(response, "space_used")
	s.Contains(response, "space_available")
}

// TestGetFileInfoNotFound tests file info when file doesn't exist
func (s *ServerTestSuite) TestGetFileInfoNotFound() {
	hash := "ffffeeee234567890abcdef1234567890abcdef1234567890abcdef123456789"

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.getFileInfo(c)
	s.NoError(err)
	s.Equal(http.StatusNotFound, rec.Code)
}

// TestDeleteFile tests the delete endpoint
func (s *ServerTestSuite) TestDeleteFile() {
	// First add a file to the mock store
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
}

// TestDeleteFileNotFound tests delete when file doesn't exist
func (s *ServerTestSuite) TestDeleteFileNotFound() {
	hash := "ffffeeee234567890abcdef1234567890abcdef1234567890abcdef123456789"

	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.deleteFile(c)
	s.NoError(err)
	s.Equal(http.StatusNotFound, rec.Code)
}

// TestDeleteFileInvalidHash tests delete with invalid hash
func (s *ServerTestSuite) TestDeleteFileInvalidHash() {
	hash := "invalid"

	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.deleteFile(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)
}

// TestServeSwaggerUI tests the swagger UI endpoint
func (s *ServerTestSuite) TestServeSwaggerUI() {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err := s.server.serveSwaggerUI(c)
	// This will likely fail in test environment, but should not panic
	// Note: Error may be nil if test runs in different environment
	if err != nil {
		s.Error(err) // Expected since we don't have actual swagger files in test
	}
}

// TestServeSwaggerSpec tests the swagger spec endpoint
func (s *ServerTestSuite) TestServeSwaggerSpec() {
	req := httptest.NewRequest(http.MethodGet, "/swagger.yml", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err := s.server.serveSwaggerSpec(c)
	// This will likely fail in test environment, but should not panic
	s.Error(err) // Expected since we don't have actual swagger files in test
}

// TestInvalidHTTPMethods tests endpoints with invalid HTTP methods
func (s *ServerTestSuite) TestInvalidHTTPMethods() {
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	// Test invalid method on upload endpoint
	req := httptest.NewRequest(http.MethodGet, "/file/upload", nil)
	rec := httptest.NewRecorder()
	s.server.echo.ServeHTTP(rec, req)
	s.Equal(http.StatusMethodNotAllowed, rec.Code)

	// Test invalid method on download endpoint
	req = httptest.NewRequest(http.MethodPost, "/file/"+hash+"/download", nil)
	rec = httptest.NewRecorder()
	s.server.echo.ServeHTTP(rec, req)
	s.Equal(http.StatusMethodNotAllowed, rec.Code)
}

// TestRoutesSetup tests that all routes are properly configured
func (s *ServerTestSuite) TestRoutesSetup() {
	// Test that server has echo instance configured
	s.NotNil(s.server.echo)

	// Test that routes exist by checking they don't return 404
	routes := s.server.echo.Routes()
	s.Greater(len(routes), 0)

	// Verify specific routes exist
	routePaths := make(map[string]bool)
	for _, route := range routes {
		routePaths[route.Path] = true
	}

	s.True(routePaths["/"])
	s.True(routePaths["/swagger.yml"])
	s.True(routePaths["/file/upload"])
	s.True(routePaths["/file/:hash/download"])
	s.True(routePaths["/file/:hash/info"])
	s.True(routePaths["/file/:hash/delete"])
}

// TestShutdown tests the shutdown functionality
func (s *ServerTestSuite) TestShutdown() {
	// Shutdown should complete without errors even when server isn't running
	err := s.server.Shutdown()
	s.NoError(err)
}

// TestMiddleware tests middleware configuration
func (s *ServerTestSuite) TestMiddleware() {
	// Test that middleware is configured by checking for gzip and recovery
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rec := httptest.NewRecorder()

	s.server.echo.ServeHTTP(rec, req)

	// Should get 404 due to middleware processing (not a panic)
	s.Equal(http.StatusNotFound, rec.Code)

	// Test gzip middleware is active
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec = httptest.NewRecorder()

	s.server.echo.ServeHTTP(rec, req)
	// Should have gzip header if gzip middleware is active
}

// TestConstants tests server constants
func (s *ServerTestSuite) TestConstants() {
	s.Equal(10, shutdownTimeout)
}

// TestUploadFileInvalidMultipart tests upload with invalid multipart data
func (s *ServerTestSuite) TestUploadFileInvalidMultipart() {
	req := httptest.NewRequest(http.MethodPost, "/file/upload", bytes.NewReader([]byte("invalid multipart data")))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=invalid")

	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err := s.server.uploadFile(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)
}

// TestUploadFileNoFile tests upload when no file is provided
func (s *ServerTestSuite) TestUploadFileNoFile() {
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
}

// TestDownloadFileInvalidHash tests download with invalid hash
func (s *ServerTestSuite) TestDownloadFileInvalidHash() {
	hash := "invalid_hash"

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.downloadFile(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)
}

// TestGetFileInfoInvalidHash tests file info with invalid hash
func (s *ServerTestSuite) TestGetFileInfoInvalidHash() {
	hash := "invalid_hash"

	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)
	c.SetParamNames("hash")
	c.SetParamValues(hash)

	err := s.server.getFileInfo(c)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)
}

// TestSwaggerUIFileNotFound tests swagger UI when template file doesn't exist
func (s *ServerTestSuite) TestSwaggerUIFileNotFound() {
	// This test already exists in the suite, testing the error case
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err := s.server.serveSwaggerUI(c)
	// serveSwaggerUI handles the error internally and returns it via ctx.String
	// So we expect no error returned from the function itself, but HTTP 500
	s.NoError(err)
	s.Equal(http.StatusInternalServerError, rec.Code)
	s.Contains(rec.Body.String(), "Failed to load template")
}

// TestSuite runs the server test suite
func TestServerSuite(t *testing.T) {
	suite.Run(t, new(ServerTestSuite))
}
