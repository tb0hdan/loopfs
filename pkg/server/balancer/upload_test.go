package balancer

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"loopfs/pkg/models"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/suite"
)

// UploadTestSuite tests the upload functionality
type UploadTestSuite struct {
	suite.Suite
	balancer       *Balancer
	backendManager *BackendManager
	mockBackend    *httptest.Server
}

// SetupSuite runs once before all tests
func (s *UploadTestSuite) SetupSuite() {
	// Create a mock backend server
	s.mockBackend = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/node/info"):
			nodeInfo := models.NodeInfo{
				Storage: models.StorageInfo{
					Total:     107374182400, // 100GB
					Used:      53687091200,  // 50GB
					Available: 53687091200,  // 50GB
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(nodeInfo)
		case strings.HasSuffix(r.URL.Path, "/file/upload"):
			// Parse multipart form
			err := r.ParseMultipartForm(32 << 20) // 32MB
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			file, header, err := r.FormFile("file")
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "No file provided"})
				return
			}
			defer file.Close()

			// Read file content
			content, err := io.ReadAll(file)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			// Handle different scenarios based on filename
			switch header.Filename {
			case "error.txt":
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "Upload failed"})
			case "large.txt":
				if len(content) > 1024*1024 { // 1MB
					response := models.UploadResponse{Hash: "large12345678901234567890123456789012345678901234567890123456789"}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(response)
				} else {
					w.WriteHeader(http.StatusBadRequest)
				}
			default:
				response := models.UploadResponse{Hash: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(response)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	backends := []string{s.mockBackend.URL}
	s.backendManager = NewBackendManager(backends, 100*time.Millisecond, 5*time.Second)
	s.backendManager.Start()
	time.Sleep(200 * time.Millisecond)
	s.balancer = NewBalancer(s.backendManager, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)
}

// TearDownSuite runs once after all tests
func (s *UploadTestSuite) TearDownSuite() {
	if s.backendManager != nil {
		s.backendManager.Stop()
	}
	if s.mockBackend != nil {
		s.mockBackend.Close()
	}
}

// createMultipartRequest creates a multipart form request for testing
func (s *UploadTestSuite) createMultipartRequest(filename, content string) (*http.Request, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}

	_, err = part.Write([]byte(content))
	if err != nil {
		return nil, err
	}

	err = writer.Close()
	if err != nil {
		return nil, err
	}

	req := httptest.NewRequest(http.MethodPost, "/file/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	return req, nil
}

// TestUploadHandlerSuccess tests successful file upload
func (s *UploadTestSuite) TestUploadHandlerSuccess() {
	req, err := s.createMultipartRequest("test.txt", "test file content")
	s.Require().NoError(err)

	e := echo.New()
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	err = s.balancer.UploadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response models.UploadResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890", response.Hash)
}

// TestUploadHandlerMissingFile tests upload with no file provided
func (s *UploadTestSuite) TestUploadHandlerMissingFile() {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writer.WriteField("notfile", "some data")
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/file/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	e := echo.New()
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	err := s.balancer.UploadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("No file provided", response["error"])
}

// TestUploadHandlerEmptyFile tests upload with empty file
func (s *UploadTestSuite) TestUploadHandlerEmptyFile() {
	req, err := s.createMultipartRequest("empty.txt", "")
	s.Require().NoError(err)

	e := echo.New()
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	err = s.balancer.UploadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
}

// TestUploadHandlerLargeFile tests upload with large file
func (s *UploadTestSuite) TestUploadHandlerLargeFile() {
	largeContent := strings.Repeat("Large file content test data. ", 35000) // About 1MB+
	req, err := s.createMultipartRequest("large.txt", largeContent)
	s.Require().NoError(err)

	e := echo.New()
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	err = s.balancer.UploadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response models.UploadResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Contains(response.Hash, "large")
}

// TestUploadHandlerInvalidMultipart tests upload with invalid multipart data
func (s *UploadTestSuite) TestUploadHandlerInvalidMultipart() {
	req := httptest.NewRequest(http.MethodPost, "/file/upload", strings.NewReader("invalid multipart data"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=invalid")

	e := echo.New()
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	err := s.balancer.UploadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)
}

// TestUploadHandlerNoBackendSpace tests upload when no backend has enough space
func (s *UploadTestSuite) TestUploadHandlerNoBackendSpace() {
	// Create a backend with very little space
	lowSpaceBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/node/info") {
			nodeInfo := models.NodeInfo{
				Storage: models.StorageInfo{
					Total:     1024,
					Used:      1000,
					Available: 24, // Very little space
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(nodeInfo)
		}
	}))
	defer lowSpaceBackend.Close()

	backends := []string{lowSpaceBackend.URL}
	bm := NewBackendManager(backends, 100*time.Millisecond, 5*time.Second)
	bm.Start()
	defer bm.Stop()
	time.Sleep(200 * time.Millisecond)

	balancer := NewBalancer(bm, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)

	largeContent := strings.Repeat("Large file content ", 1000) // About 19KB
	req, err := s.createMultipartRequest("test.txt", largeContent)
	s.Require().NoError(err)

	e := echo.New()
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	err = balancer.UploadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusServiceUnavailable, rec.Code)

	var response map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Contains(response["error"], "offline")
}

// TestUploadHandlerBackendError tests upload when backend returns error
func (s *UploadTestSuite) TestUploadHandlerBackendError() {
	req, err := s.createMultipartRequest("error.txt", "test content")
	s.Require().NoError(err)

	e := echo.New()
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	err = s.balancer.UploadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusInternalServerError, rec.Code)
}

// TestUploadHandlerBackendUnavailable tests upload when backend is unavailable
func (s *UploadTestSuite) TestUploadHandlerBackendUnavailable() {
	backends := []string{"http://nonexistent:8080"}
	bm := NewBackendManager(backends, 100*time.Millisecond, 500*time.Millisecond)
	bm.Start()
	defer bm.Stop()
	time.Sleep(300 * time.Millisecond)

	balancer := NewBalancer(bm, 1, 50*time.Millisecond, 100*time.Millisecond, 1*time.Second)

	req, err := s.createMultipartRequest("test.txt", "test content")
	s.Require().NoError(err)

	e := echo.New()
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	err = balancer.UploadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusServiceUnavailable, rec.Code)
}

// TestUploadHandlerMultipleBackends tests upload with multiple backends
func (s *UploadTestSuite) TestUploadHandlerMultipleBackends() {
	// Create a second backend with less space
	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/node/info") {
			nodeInfo := models.NodeInfo{
				Storage: models.StorageInfo{
					Total:     53687091200,
					Used:      48318382080,
					Available: 5368709120, // Less space than first backend
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(nodeInfo)
		} else if strings.HasSuffix(r.URL.Path, "/file/upload") {
			response := models.UploadResponse{Hash: "backend2hash1234567890123456789012345678901234567890123456789012"}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}
	}))
	defer backend2.Close()

	backends := []string{s.mockBackend.URL, backend2.URL}
	bm := NewBackendManager(backends, 100*time.Millisecond, 5*time.Second)
	bm.Start()
	defer bm.Stop()
	time.Sleep(200 * time.Millisecond)

	balancer := NewBalancer(bm, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)

	req, err := s.createMultipartRequest("test.txt", "test content")
	s.Require().NoError(err)

	e := echo.New()
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	err = balancer.UploadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response models.UploadResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	// Should upload to the first backend with more space
	s.Equal("abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890", response.Hash)
}

// TestUploadHandlerTimeout tests upload with timeout
func (s *UploadTestSuite) TestUploadHandlerTimeout() {
	slowBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/node/info") {
			nodeInfo := models.NodeInfo{
				Storage: models.StorageInfo{Available: 1000000},
			}
			json.NewEncoder(w).Encode(nodeInfo)
		} else if strings.HasSuffix(r.URL.Path, "/file/upload") {
			time.Sleep(2 * time.Second) // Longer than timeout
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer slowBackend.Close()

	backends := []string{slowBackend.URL}
	bm := NewBackendManager(backends, 100*time.Millisecond, 5*time.Second)
	bm.Start()
	defer bm.Stop()
	time.Sleep(200 * time.Millisecond)

	balancer := NewBalancer(bm, 1, 50*time.Millisecond, 100*time.Millisecond, 500*time.Millisecond)

	req, err := s.createMultipartRequest("test.txt", "test content")
	s.Require().NoError(err)

	e := echo.New()
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	err = balancer.UploadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusServiceUnavailable, rec.Code)
}

// TestUploadHandlerSpecialCharacters tests upload with special characters in filename
func (s *UploadTestSuite) TestUploadHandlerSpecialCharacters() {
	req, err := s.createMultipartRequest("file with spaces & symbols!@#.txt", "test content")
	s.Require().NoError(err)

	e := echo.New()
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	err = s.balancer.UploadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
}

// TestUploadHandlerBinaryContent tests upload with binary content
func (s *UploadTestSuite) TestUploadHandlerBinaryContent() {
	binaryContent := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "binary.bin")
	s.Require().NoError(err)
	_, err = part.Write(binaryContent)
	s.Require().NoError(err)
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/file/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	e := echo.New()
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	err = s.balancer.UploadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
}

// TestUploadHandlerConcurrentRequests tests concurrent upload requests
func (s *UploadTestSuite) TestUploadHandlerConcurrentRequests() {
	numRequests := 5
	results := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(index int) {
			content := "concurrent test content " + string(rune('0'+index))
			req, err := s.createMultipartRequest("concurrent.txt", content)
			if err != nil {
				results <- false
				return
			}

			e := echo.New()
			rec := httptest.NewRecorder()
			ctx := e.NewContext(req, rec)

			err = s.balancer.UploadHandler(ctx)
			results <- err == nil && rec.Code == http.StatusOK
		}(i)
	}

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		success := <-results
		s.True(success)
	}
}

// TestUploadHandlerDifferentContentTypes tests upload with various content types
func (s *UploadTestSuite) TestUploadHandlerDifferentContentTypes() {
	testCases := []struct {
		filename string
		content  string
	}{
		{"test.txt", "text content"},
		{"test.json", `{"key": "value"}`},
		{"test.xml", `<?xml version="1.0"?><root>data</root>`},
		{"test.csv", "col1,col2,col3\nval1,val2,val3"},
	}

	for _, tc := range testCases {
		s.Run(tc.filename, func() {
			req, err := s.createMultipartRequest(tc.filename, tc.content)
			s.Require().NoError(err)

			e := echo.New()
			rec := httptest.NewRecorder()
			ctx := e.NewContext(req, rec)

			err = s.balancer.UploadHandler(ctx)
			s.NoError(err)
			s.Equal(http.StatusOK, rec.Code)

			var response models.UploadResponse
			err = json.Unmarshal(rec.Body.Bytes(), &response)
			s.NoError(err)
			s.NotEmpty(response.Hash)
		})
	}
}

// TestUploadHandlerResponseForwarding tests that backend responses are properly forwarded
func (s *UploadTestSuite) TestUploadHandlerResponseForwarding() {
	customBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/node/info") {
			nodeInfo := models.NodeInfo{
				Storage: models.StorageInfo{Available: 1000000},
			}
			json.NewEncoder(w).Encode(nodeInfo)
		} else if strings.HasSuffix(r.URL.Path, "/file/upload") {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Custom-Header", "custom-value")
			w.WriteHeader(http.StatusCreated) // Different status code
			response := map[string]interface{}{
				"hash":    "custom1234567890abcdef1234567890abcdef1234567890abcdef123456789",
				"message": "Upload successful",
				"size":    12,
			}
			json.NewEncoder(w).Encode(response)
		}
	}))
	defer customBackend.Close()

	backends := []string{customBackend.URL}
	bm := NewBackendManager(backends, 100*time.Millisecond, 5*time.Second)
	bm.Start()
	defer bm.Stop()
	time.Sleep(200 * time.Millisecond)

	balancer := NewBalancer(bm, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)

	req, err := s.createMultipartRequest("test.txt", "test content")
	s.Require().NoError(err)

	e := echo.New()
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	err = balancer.UploadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusCreated, rec.Code) // Should forward the custom status code

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Contains(response["hash"], "custom")
	s.Equal("Upload successful", response["message"])
	s.Equal(float64(12), response["size"])
}

// TestUploadSuite runs the upload test suite
func TestUploadSuite(t *testing.T) {
	suite.Run(t, new(UploadTestSuite))
}
