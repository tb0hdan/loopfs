package balancer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"loopfs/pkg/models"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/suite"
)

// InfoTestSuite tests the file info functionality
type InfoTestSuite struct {
	suite.Suite
	balancer    *Balancer
	mockBackend *httptest.Server
}

// SetupSuite runs once before all tests
func (s *InfoTestSuite) SetupSuite() {
	// Create a mock backend server
	s.mockBackend = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/info") {
			hash := extractHashFromInfoPath(r.URL.Path)
			switch hash {
			case "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890":
				fileInfo := models.FileInfo{
					Hash:           hash,
					Size:           1024,
					CreatedAt:      time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
					SpaceUsed:      1024,
					SpaceAvailable: 53687091200,
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(fileInfo)
			case "notfound1234567890abcdef1234567890abcdef1234567890abcdef1234567890":
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]string{"error": "File not found"})
			case "large123456789abcdef1234567890abcdef1234567890abcdef1234567890ab":
				fileInfo := models.FileInfo{
					Hash:           hash,
					Size:           1073741824, // 1GB
					CreatedAt:      time.Date(2023, 6, 15, 12, 30, 45, 0, time.UTC),
					SpaceUsed:      1073741824,
					SpaceAvailable: 10737418240,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(fileInfo)
			case "error123456789abcdef1234567890abcdef1234567890abcdef1234567890ab":
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "Internal error"})
			case "badjson123456789abcdef1234567890abcdef1234567890abcdef12345678901":
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("invalid json"))
			default:
				fileInfo := models.FileInfo{
					Hash:      hash,
					Size:      512,
					CreatedAt: time.Now(),
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(fileInfo)
			}
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	backends := []string{s.mockBackend.URL}
	s.balancer = NewBalancer(backends, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)
}

// TearDownSuite runs once after all tests
func (s *InfoTestSuite) TearDownSuite() {
	if s.mockBackend != nil {
		s.mockBackend.Close()
	}
}

// Helper function to extract hash from info path
func extractHashFromInfoPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "file" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// TestFileInfoHandlerSuccess tests successful file info retrieval
func (s *InfoTestSuite) TestFileInfoHandlerSuccess() {
	e := echo.New()
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := s.balancer.FileInfoHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response models.FileInfo
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal(hash, response.Hash)
	s.Equal(int64(1024), response.Size)
	s.Equal(uint64(1024), response.SpaceUsed)
	s.Equal(uint64(53687091200), response.SpaceAvailable)
	s.Equal(time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC), response.CreatedAt)
}

// TestFileInfoHandlerMissingHash tests info request with missing hash parameter
func (s *InfoTestSuite) TestFileInfoHandlerMissingHash() {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/file//info", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues("")

	err := s.balancer.FileInfoHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("Hash parameter is required", response["error"])
}

// TestFileInfoHandlerFileNotFound tests info request when file doesn't exist
func (s *InfoTestSuite) TestFileInfoHandlerFileNotFound() {
	e := echo.New()
	hash := "notfound1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := s.balancer.FileInfoHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusNotFound, rec.Code)

	var response map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("File not found", response["error"])
}

// TestFileInfoHandlerLargeFile tests info for large file
func (s *InfoTestSuite) TestFileInfoHandlerLargeFile() {
	e := echo.New()
	hash := "large123456789abcdef1234567890abcdef1234567890abcdef1234567890ab"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := s.balancer.FileInfoHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response models.FileInfo
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal(hash, response.Hash)
	s.Equal(int64(1073741824), response.Size) // 1GB
	s.Equal(time.Date(2023, 6, 15, 12, 30, 45, 0, time.UTC), response.CreatedAt)
}

// TestFileInfoHandlerMultipleBackends tests info request with multiple backends
func (s *InfoTestSuite) TestFileInfoHandlerMultipleBackends() {
	// Create a second backend that returns not found
	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer backend2.Close()

	// Create a third backend that succeeds
	backend3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/info") {
			hash := extractHashFromInfoPath(r.URL.Path)
			fileInfo := models.FileInfo{
				Hash:      hash,
				Size:      2048,
				CreatedAt: time.Date(2023, 12, 25, 10, 15, 30, 0, time.UTC),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(fileInfo)
		}
	}))
	defer backend3.Close()

	backends := []string{backend2.URL, backend3.URL}
	balancer := NewBalancer(backends, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)

	e := echo.New()
	hash := "multi12345678901234567890123456789012345678901234567890123456789012"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.FileInfoHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response models.FileInfo
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal(hash, response.Hash)
	s.Equal(int64(2048), response.Size)
	s.Equal(time.Date(2023, 12, 25, 10, 15, 30, 0, time.UTC), response.CreatedAt)
}

// TestFileInfoHandlerAllBackendsNotFound tests info when file not found on all backends
func (s *InfoTestSuite) TestFileInfoHandlerAllBackendsNotFound() {
	// Create backends that all return not found
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer backend1.Close()

	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer backend2.Close()

	backends := []string{backend1.URL, backend2.URL}
	balancer := NewBalancer(backends, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)

	e := echo.New()
	hash := "missing1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.FileInfoHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusNotFound, rec.Code)

	var response map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("File not found", response["error"])
}

// TestFileInfoHandlerAllBackendsUnavailable tests info when all backends are unavailable
func (s *InfoTestSuite) TestFileInfoHandlerAllBackendsUnavailable() {
	backends := []string{"http://nonexistent1:8080", "http://nonexistent2:8080"}
	balancer := NewBalancer(backends, 1, 50*time.Millisecond, 100*time.Millisecond, 1*time.Second)

	e := echo.New()
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.FileInfoHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusServiceUnavailable, rec.Code)

	var response map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Contains(response["error"], "Info request failed")
}

// TestFileInfoHandlerBackendError tests info when backend returns error
func (s *InfoTestSuite) TestFileInfoHandlerBackendError() {
	e := echo.New()
	hash := "error123456789abcdef1234567890abcdef1234567890abcdef1234567890ab"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := s.balancer.FileInfoHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusServiceUnavailable, rec.Code)
}

// TestFileInfoHandlerBadJSON tests info when backend returns invalid JSON
func (s *InfoTestSuite) TestFileInfoHandlerBadJSON() {
	e := echo.New()
	hash := "badjson123456789abcdef1234567890abcdef1234567890abcdef12345678901"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := s.balancer.FileInfoHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusServiceUnavailable, rec.Code)
}

// TestFileInfoHandlerTimeout tests info request with timeout
func (s *InfoTestSuite) TestFileInfoHandlerTimeout() {
	slowBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // Longer than timeout
		fileInfo := models.FileInfo{Hash: "slow", Size: 100}
		json.NewEncoder(w).Encode(fileInfo)
	}))
	defer slowBackend.Close()

	backends := []string{slowBackend.URL}
	balancer := NewBalancer(backends, 1, 50*time.Millisecond, 100*time.Millisecond, 500*time.Millisecond)

	e := echo.New()
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.FileInfoHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusServiceUnavailable, rec.Code)
}

// TestFileInfoHandlerConcurrentRequests tests concurrent info requests
func (s *InfoTestSuite) TestFileInfoHandlerConcurrentRequests() {
	numRequests := 10
	results := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(index int) {
			e := echo.New()
			hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
			req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
			rec := httptest.NewRecorder()
			ctx := e.NewContext(req, rec)
			ctx.SetParamNames("hash")
			ctx.SetParamValues(hash)

			err := s.balancer.FileInfoHandler(ctx)
			results <- err == nil && rec.Code == http.StatusOK
		}(i)
	}

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		success := <-results
		s.True(success)
	}
}

// TestFileInfoHandlerPartialBackendFailures tests info when some backends fail
func (s *InfoTestSuite) TestFileInfoHandlerPartialBackendFailures() {
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
		if strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/info") {
			hash := extractHashFromInfoPath(r.URL.Path)
			fileInfo := models.FileInfo{
				Hash:      hash,
				Size:      4096,
				CreatedAt: time.Date(2023, 3, 15, 9, 30, 0, 0, time.UTC),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(fileInfo)
		}
	}))
	defer successBackend.Close()

	backends := []string{errorBackend.URL, notFoundBackend.URL, successBackend.URL}
	balancer := NewBalancer(backends, 1, 50*time.Millisecond, 100*time.Millisecond, 5*time.Second)

	e := echo.New()
	hash := "partial1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.FileInfoHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response models.FileInfo
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal(hash, response.Hash)
	s.Equal(int64(4096), response.Size)
}

// TestFileInfoHandlerLongHash tests info with maximum length hash
func (s *InfoTestSuite) TestFileInfoHandlerLongHash() {
	e := echo.New()
	hash := "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdef"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := s.balancer.FileInfoHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response models.FileInfo
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal(hash, response.Hash)
	s.Equal(int64(512), response.Size)
}

// TestFileInfoHandlerDifferentSizes tests info for files with different sizes
func (s *InfoTestSuite) TestFileInfoHandlerDifferentSizes() {
	testCases := []struct {
		name     string
		hash     string
		expected int64
	}{
		{"empty_file", "empty12345678901234567890123456789012345678901234567890123456789", 0},
		{"small_file", "small12345678901234567890123456789012345678901234567890123456789", 128},
		{"medium_file", "medium123456789012345678901234567890123456789012345678901234567", 1048576},    // 1MB
		{"large_file", "large12345678901234567890123456789012345678901234567890123456789", 1073741824}, // 1GB
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			// Create a custom backend for this test case
			testBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/info") {
					hash := extractHashFromInfoPath(r.URL.Path)
					fileInfo := models.FileInfo{
						Hash:      hash,
						Size:      tc.expected,
						CreatedAt: time.Now(),
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(fileInfo)
				}
			}))
			defer testBackend.Close()

			backends := []string{testBackend.URL}
			balancer := NewBalancer(backends, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/file/"+tc.hash+"/info", nil)
			rec := httptest.NewRecorder()
			ctx := e.NewContext(req, rec)
			ctx.SetParamNames("hash")
			ctx.SetParamValues(tc.hash)

			err := balancer.FileInfoHandler(ctx)
			s.NoError(err)
			s.Equal(http.StatusOK, rec.Code)

			var response models.FileInfo
			err = json.Unmarshal(rec.Body.Bytes(), &response)
			s.NoError(err)
			s.Equal(tc.hash, response.Hash)
			s.Equal(tc.expected, response.Size)
		})
	}
}

// TestFileInfoHandlerOptionalFields tests info with optional fields
func (s *InfoTestSuite) TestFileInfoHandlerOptionalFields() {
	optionalFieldsBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/info") {
			hash := extractHashFromInfoPath(r.URL.Path)
			fileInfo := models.FileInfo{
				Hash:           hash,
				Size:           8192,
				CreatedAt:      time.Date(2023, 8, 20, 14, 45, 0, 0, time.UTC),
				SpaceUsed:      8192,
				SpaceAvailable: 107374182400,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(fileInfo)
		}
	}))
	defer optionalFieldsBackend.Close()

	backends := []string{optionalFieldsBackend.URL}
	balancer := NewBalancer(backends, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)

	e := echo.New()
	hash := "optional123456789012345678901234567890123456789012345678901234567890"
	req := httptest.NewRequest(http.MethodGet, "/file/"+hash+"/info", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.FileInfoHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response models.FileInfo
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal(hash, response.Hash)
	s.Equal(int64(8192), response.Size)
	s.Equal(uint64(8192), response.SpaceUsed)
	s.Equal(uint64(107374182400), response.SpaceAvailable)
	s.Equal(time.Date(2023, 8, 20, 14, 45, 0, 0, time.UTC), response.CreatedAt)
}

// TestInfoSuite runs the info test suite
func TestInfoSuite(t *testing.T) {
	suite.Run(t, new(InfoTestSuite))
}
