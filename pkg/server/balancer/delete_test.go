package balancer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/suite"
)

// DeleteTestSuite tests the delete functionality
type DeleteTestSuite struct {
	suite.Suite
	balancer    *Balancer
	mockBackend *httptest.Server
}

// SetupSuite runs once before all tests
func (s *DeleteTestSuite) SetupSuite() {
	// Create a mock backend server
	s.mockBackend = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/delete") {
			hash := extractHashFromDeletePath(r.URL.Path)
			switch hash {
			case "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890":
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				response := map[string]string{
					"message": "File deleted successfully",
					"hash":    hash,
				}
				json.NewEncoder(w).Encode(response)
			case "notfound1234567890abcdef1234567890abcdef1234567890abcdef1234567890":
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]string{"error": "File not found"})
			case "error123456789abcdef1234567890abcdef1234567890abcdef1234567890ab":
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "Delete failed"})
			default:
				w.WriteHeader(http.StatusOK)
				response := map[string]string{
					"message": "File deleted successfully",
					"hash":    hash,
				}
				json.NewEncoder(w).Encode(response)
			}
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	backends := []string{s.mockBackend.URL}
	s.balancer = NewBalancer(backends, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)
}

// TearDownSuite runs once after all tests
func (s *DeleteTestSuite) TearDownSuite() {
	if s.mockBackend != nil {
		s.mockBackend.Close()
	}
}

// Helper function to extract hash from delete path
func extractHashFromDeletePath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "file" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// TestDeleteHandlerSuccess tests successful file deletion
func (s *DeleteTestSuite) TestDeleteHandlerSuccess() {
	e := echo.New()
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := s.balancer.DeleteHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("File deleted successfully", response["message"])
	s.Equal(hash, response["hash"])
}

// TestDeleteHandlerMissingHash tests delete with missing hash parameter
func (s *DeleteTestSuite) TestDeleteHandlerMissingHash() {
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/file//delete", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues("")

	err := s.balancer.DeleteHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, rec.Code)

	var response map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("Hash parameter is required", response["error"])
}

// TestDeleteHandlerFileNotFound tests delete when file doesn't exist
func (s *DeleteTestSuite) TestDeleteHandlerFileNotFound() {
	e := echo.New()
	hash := "notfound1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := s.balancer.DeleteHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusNotFound, rec.Code)

	var response map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("File not found", response["error"])
}

// TestDeleteHandlerMultipleBackends tests delete with multiple backends
func (s *DeleteTestSuite) TestDeleteHandlerMultipleBackends() {
	// Create a second backend that returns not found
	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "File not found"})
	}))
	defer backend2.Close()

	// Create a third backend that succeeds
	backend3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/delete") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			hash := extractHashFromDeletePath(r.URL.Path)
			response := map[string]string{
				"message": "File deleted from backend 3",
				"hash":    hash,
			}
			json.NewEncoder(w).Encode(response)
		}
	}))
	defer backend3.Close()

	backends := []string{s.mockBackend.URL, backend2.URL, backend3.URL}
	balancer := NewBalancer(backends, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)

	e := echo.New()
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.DeleteHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	// Should get response from the first successful backend
	s.Contains([]string{"File deleted successfully", "File deleted from backend 3"}, response["message"])
	s.Equal(hash, response["hash"])
}

// TestDeleteHandlerAllBackendsNotFound tests delete when file not found on all backends
func (s *DeleteTestSuite) TestDeleteHandlerAllBackendsNotFound() {
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
	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.DeleteHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusNotFound, rec.Code)

	var response map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("File not found", response["error"])
}

// TestDeleteHandlerPartialSuccess tests delete when some backends succeed
func (s *DeleteTestSuite) TestDeleteHandlerPartialSuccess() {
	// Create a backend that returns error
	errorBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errorBackend.Close()

	// Create a backend that succeeds
	successBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/delete") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			hash := extractHashFromDeletePath(r.URL.Path)
			response := map[string]string{
				"message": "Deleted successfully",
				"hash":    hash,
			}
			json.NewEncoder(w).Encode(response)
		}
	}))
	defer successBackend.Close()

	backends := []string{errorBackend.URL, successBackend.URL}
	balancer := NewBalancer(backends, 1, 50*time.Millisecond, 100*time.Millisecond, 5*time.Second)

	e := echo.New()
	hash := "partial1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.DeleteHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code) // Should succeed if at least one backend succeeds

	var response map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("Deleted successfully", response["message"])
}

// TestDeleteHandlerAllBackendsUnavailable tests delete when all backends are unavailable
func (s *DeleteTestSuite) TestDeleteHandlerAllBackendsUnavailable() {
	backends := []string{"http://nonexistent1:8080", "http://nonexistent2:8080"}
	balancer := NewBalancer(backends, 1, 50*time.Millisecond, 100*time.Millisecond, 1*time.Second)

	e := echo.New()
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.DeleteHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusServiceUnavailable, rec.Code)

	var response map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Contains(response["error"], "Delete failed")
}

// TestDeleteHandlerBackendError tests delete when backend returns error
func (s *DeleteTestSuite) TestDeleteHandlerBackendError() {
	e := echo.New()
	hash := "error123456789abcdef1234567890abcdef1234567890abcdef1234567890ab"
	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := s.balancer.DeleteHandler(ctx)
	s.NoError(err)
	// Should still return success if no backend explicitly returned success
	s.Equal(http.StatusServiceUnavailable, rec.Code)
}

// TestDeleteHandlerTimeout tests delete with timeout
func (s *DeleteTestSuite) TestDeleteHandlerTimeout() {
	slowBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // Longer than timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer slowBackend.Close()

	backends := []string{slowBackend.URL}
	balancer := NewBalancer(backends, 1, 50*time.Millisecond, 100*time.Millisecond, 500*time.Millisecond)

	e := echo.New()
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.DeleteHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusServiceUnavailable, rec.Code)
}

// TestDeleteHandlerConcurrentRequests tests concurrent delete requests
func (s *DeleteTestSuite) TestDeleteHandlerConcurrentRequests() {
	numRequests := 10
	results := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(index int) {
			e := echo.New()
			hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
			req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
			rec := httptest.NewRecorder()
			ctx := e.NewContext(req, rec)
			ctx.SetParamNames("hash")
			ctx.SetParamValues(hash)

			err := s.balancer.DeleteHandler(ctx)
			results <- err == nil && rec.Code == http.StatusOK
		}(i)
	}

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		success := <-results
		s.True(success)
	}
}

// TestDeleteHandlerDifferentResponseFormats tests delete with different backend response formats
func (s *DeleteTestSuite) TestDeleteHandlerDifferentResponseFormats() {
	// Backend that returns success without JSON body
	noBodyBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/delete") {
			w.WriteHeader(http.StatusOK)
			// No body
		}
	}))
	defer noBodyBackend.Close()

	backends := []string{noBodyBackend.URL}
	balancer := NewBalancer(backends, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)

	e := echo.New()
	hash := "nobody1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.DeleteHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("File deleted successfully", response["message"])
	s.Equal(hash, response["hash"])
}

// TestDeleteHandlerMixedBackendResponses tests delete with backends returning mixed responses
func (s *DeleteTestSuite) TestDeleteHandlerMixedBackendResponses() {
	// Backend 1: Returns not found
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer backend1.Close()

	// Backend 2: Returns success with custom response
	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/delete") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			hash := extractHashFromDeletePath(r.URL.Path)
			response := map[string]interface{}{
				"success":   true,
				"hash":      hash,
				"backend":   "custom-backend-2",
				"timestamp": time.Now().Unix(),
			}
			json.NewEncoder(w).Encode(response)
		}
	}))
	defer backend2.Close()

	// Backend 3: Returns error
	backend3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend3.Close()

	backends := []string{backend1.URL, backend2.URL, backend3.URL}
	balancer := NewBalancer(backends, 1, 50*time.Millisecond, 100*time.Millisecond, 5*time.Second)

	e := echo.New()
	hash := "mixed12345678901234567890123456789012345678901234567890123456789012"
	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.DeleteHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code) // Should succeed because backend2 succeeds

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal(true, response["success"])
	s.Equal("custom-backend-2", response["backend"])
}

// TestDeleteHandlerLongHash tests delete with maximum length hash
func (s *DeleteTestSuite) TestDeleteHandlerLongHash() {
	e := echo.New()
	hash := "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdef"
	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := s.balancer.DeleteHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal(hash, response["hash"])
}

// TestDeleteHandlerResponseBodyForwarding tests that backend response bodies are properly forwarded
func (s *DeleteTestSuite) TestDeleteHandlerResponseBodyForwarding() {
	customBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/delete") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			hash := extractHashFromDeletePath(r.URL.Path)
			response := map[string]interface{}{
				"status":     "deleted",
				"hash":       hash,
				"timestamp":  "2023-01-01T00:00:00Z",
				"backend_id": "backend-001",
				"size_freed": 1024,
			}
			json.NewEncoder(w).Encode(response)
		}
	}))
	defer customBackend.Close()

	backends := []string{customBackend.URL}
	balancer := NewBalancer(backends, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)

	e := echo.New()
	hash := "custom1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodDelete, "/file/"+hash+"/delete", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("hash")
	ctx.SetParamValues(hash)

	err := balancer.DeleteHandler(ctx)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	var response map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	s.NoError(err)
	s.Equal("deleted", response["status"])
	s.Equal("backend-001", response["backend_id"])
	s.Equal(float64(1024), response["size_freed"])
	s.Equal(hash, response["hash"])
}

// TestDeleteSuite runs the delete test suite
func TestDeleteSuite(t *testing.T) {
	suite.Run(t, new(DeleteTestSuite))
}
