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
	balancer       *Balancer
	backendManager *BackendManager
	mockBackend    *httptest.Server
}

// SetupSuite runs once before all tests
func (s *DeleteTestSuite) SetupSuite() {
	// Create a mock backend server
	s.mockBackend = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/node/info"):
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"storage":{"available":53687091200}}`))
		case strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/delete"):
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
func (s *DeleteTestSuite) TearDownSuite() {
	if s.backendManager != nil {
		s.backendManager.Stop()
	}
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

// createDeleteBalancerWithBackends creates a balancer with the given backends
func createDeleteBalancerWithBackends(backends []string) (*Balancer, *BackendManager) {
	bm := NewBackendManager(backends, 100*time.Millisecond, 5*time.Second)
	bm.Start()
	time.Sleep(200 * time.Millisecond)
	return NewBalancer(bm, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second), bm
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
	// Create multiple backends
	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/node/info") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"storage":{"available":53687091200}}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "File not found"})
		}
	}))
	defer backend2.Close()

	backend3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/node/info") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"storage":{"available":53687091200}}`))
		} else if strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/delete") {
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
	balancer, bm := createDeleteBalancerWithBackends(backends)
	defer bm.Stop()

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
}

// TestDeleteHandlerAllBackendsNotFound tests delete when file not found on all backends
func (s *DeleteTestSuite) TestDeleteHandlerAllBackendsNotFound() {
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/node/info") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"storage":{"available":53687091200}}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer backend1.Close()

	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/node/info") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"storage":{"available":53687091200}}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer backend2.Close()

	backends := []string{backend1.URL, backend2.URL}
	balancer, bm := createDeleteBalancerWithBackends(backends)
	defer bm.Stop()

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

// TestDeleteHandlerAllBackendsUnavailable tests delete when all backends are offline
func (s *DeleteTestSuite) TestDeleteHandlerAllBackendsUnavailable() {
	backends := []string{"http://nonexistent1:8080", "http://nonexistent2:8080"}
	bm := NewBackendManager(backends, 100*time.Millisecond, 500*time.Millisecond)
	bm.Start()
	defer bm.Stop()
	time.Sleep(300 * time.Millisecond)

	balancer := NewBalancer(bm, 1, 50*time.Millisecond, 100*time.Millisecond, 1*time.Second)

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
	s.Contains(response["error"], "all backends are offline")
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

// TestDeleteSuite runs the delete test suite
func TestDeleteSuite(t *testing.T) {
	suite.Run(t, new(DeleteTestSuite))
}
