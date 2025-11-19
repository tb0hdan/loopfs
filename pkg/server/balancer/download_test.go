package balancer

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/suite"
)

// DownloadTestSuite tests the download functionality
type DownloadTestSuite struct {
	suite.Suite
	balancer    *Balancer
	mockBackend *httptest.Server
}

// SetupSuite runs once before all tests
func (s *DownloadTestSuite) SetupSuite() {
	// Create a mock backend server
	s.mockBackend = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/download"):
			hash := extractHashFromPath(r.URL.Path, "/download")
			switch hash {
			case "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890":
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Length", "17")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("test file content"))
			case "notfound1234567890abcdef1234567890abcdef1234567890abcdef1234567890":
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]string{"error": "File not found"})
			case "image123456789abcdef1234567890abcdef1234567890abcdef1234567890ab":
				w.Header().Set("Content-Type", "image/jpeg")
				w.Header().Set("Content-Length", "7")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("fakeimg"))
			default:
				w.WriteHeader(http.StatusInternalServerError)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	backends := []string{s.mockBackend.URL}
	s.balancer = NewBalancer(backends, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)
}

// TearDownSuite runs once after all tests
func (s *DownloadTestSuite) TearDownSuite() {
	if s.mockBackend != nil {
		s.mockBackend.Close()
	}
}

// Helper function to extract hash from path
func extractHashFromPath(path, suffix string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "file" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// TestDownloadHandlerSuccess tests successful file download
func (s *DownloadTestSuite) TestDownloadHandlerSuccess() {
	e := echo.New()
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := s.balancer.DownloadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Equal("test file content", rec.Body.String())
	s.Equal("application/octet-stream", rec.Header().Get("Content-Type"))
}

// TestDownloadHandlerMissingHash tests download with missing hash parameter
func (s *DownloadTestSuite) TestDownloadHandlerMissingHash() {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/file//download", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues("")

	err := s.balancer.DownloadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("Hash parameter is required", response["error"])
}

// TestDownloadHandlerFileNotFound tests download when file doesn't exist
func (s *DownloadTestSuite) TestDownloadHandlerFileNotFound() {
	e := echo.New()
	hash := "notfound1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := s.balancer.DownloadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusNotFound, rec.Code)

	var response map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("File not found", response["error"])
}

// TestDownloadHandlerDifferentContentTypes tests download with different content types
func (s *DownloadTestSuite) TestDownloadHandlerDifferentContentTypes() {
	e := echo.New()
	hash := "image123456789abcdef1234567890abcdef1234567890abcdef1234567890ab"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := s.balancer.DownloadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Equal("fakeimg", rec.Body.String())
	s.Equal("image/jpeg", rec.Header().Get("Content-Type"))
}

// TestDownloadHandlerMultipleBackends tests download with multiple backends
func (s *DownloadTestSuite) TestDownloadHandlerMultipleBackends() {
	// Create a second backend that doesn't have the file
	mockBackend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockBackend2.Close()

	// Create a third backend that has the file
	mockBackend3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/download") {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("content from backend 3"))
		}
	}))
	defer mockBackend3.Close()

	backends := []string{mockBackend2.URL, mockBackend3.URL}
	balancer := NewBalancer(backends, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)

	e := echo.New()
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.DownloadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Equal("content from backend 3", rec.Body.String())
}

// TestDownloadHandlerAllBackendsUnavailable tests download when all backends are unavailable
func (s *DownloadTestSuite) TestDownloadHandlerAllBackendsUnavailable() {
	backends := []string{"http://nonexistent1:8080", "http://nonexistent2:8080"}
	balancer := NewBalancer(backends, 1, 50*time.Millisecond, 100*time.Millisecond, 1*time.Second)

	e := echo.New()
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.DownloadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusServiceUnavailable, rec.Code)

	var response map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Contains(response["error"], "Download failed")
}

// TestDownloadHandlerBackendError tests download when backend returns error
func (s *DownloadTestSuite) TestDownloadHandlerBackendError() {
	errorBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errorBackend.Close()

	backends := []string{errorBackend.URL}
	balancer := NewBalancer(backends, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)

	e := echo.New()
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.DownloadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusServiceUnavailable, rec.Code)
}

// TestDownloadHandlerTimeout tests download with timeout
func (s *DownloadTestSuite) TestDownloadHandlerTimeout() {
	slowBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // Longer than timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer slowBackend.Close()

	backends := []string{slowBackend.URL}
	balancer := NewBalancer(backends, 1, 50*time.Millisecond, 100*time.Millisecond, 500*time.Millisecond)

	e := echo.New()
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.DownloadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusServiceUnavailable, rec.Code)
}

// TestDownloadHandlerConcurrentRequests tests concurrent download requests
func (s *DownloadTestSuite) TestDownloadHandlerConcurrentRequests() {
	numRequests := 10
	results := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			e := echo.New()
			hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
			req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
			rec := httptest.NewRecorder()
			ctx := e.NewContext(req, rec)
			ctx.SetParamNames("hash")
			ctx.SetParamValues(hash)

			err := s.balancer.DownloadHandler(ctx)
			results <- err == nil && rec.Code == http.StatusOK
		}()
	}

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		success := <-results
		s.True(success)
	}
}

// TestDownloadHandlerLargeFile tests download of larger content
func (s *DownloadTestSuite) TestDownloadHandlerLargeFile() {
	largeContent := bytes.Repeat([]byte("large file content "), 1000) // About 19KB

	largeFileBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/download") {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			w.Write(largeContent)
		}
	}))
	defer largeFileBackend.Close()

	backends := []string{largeFileBackend.URL}
	balancer := NewBalancer(backends, 3, 100*time.Millisecond, 500*time.Millisecond, 30*time.Second)

	e := echo.New()
	hash := "largef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.DownloadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Equal(largeContent, rec.Body.Bytes())
}

// TestDownloadHandlerHeaderForwarding tests that headers are properly forwarded
func (s *DownloadTestSuite) TestDownloadHandlerHeaderForwarding() {
	headerBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/download") {
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Disposition", "attachment; filename=test.txt")
			w.Header().Set("Cache-Control", "max-age=3600")
			w.Header().Set("ETag", "\"test-etag\"")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("test content"))
		}
	}))
	defer headerBackend.Close()

	backends := []string{headerBackend.URL}
	balancer := NewBalancer(backends, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)

	e := echo.New()
	hash := "header1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.DownloadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Equal("test content", rec.Body.String())
	s.Equal("text/plain", rec.Header().Get("Content-Type"))
	s.Equal("attachment; filename=test.txt", rec.Header().Get("Content-Disposition"))
	s.Equal("max-age=3600", rec.Header().Get("Cache-Control"))
	s.Equal("\"test-etag\"", rec.Header().Get("ETag"))
}

// TestDownloadHandlerPartialBackendFailures tests download when some backends fail
func (s *DownloadTestSuite) TestDownloadHandlerPartialBackendFailures() {
	// First backend returns error
	errorBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errorBackend.Close()

	// Second backend returns not found
	notFoundBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer notFoundBackend.Close()

	// Third backend succeeds
	successBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/download") {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("success content"))
		}
	}))
	defer successBackend.Close()

	backends := []string{errorBackend.URL, notFoundBackend.URL, successBackend.URL}
	balancer := NewBalancer(backends, 1, 50*time.Millisecond, 100*time.Millisecond, 5*time.Second)

	e := echo.New()
	hash := "partial1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/download", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.DownloadHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Equal("success content", rec.Body.String())
}

// TestDownloadSuite runs the download test suite
func TestDownloadSuite(t *testing.T) {
	suite.Run(t, new(DownloadTestSuite))
}
