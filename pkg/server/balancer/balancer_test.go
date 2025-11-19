package balancer

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"loopfs/pkg/models"

	"github.com/stretchr/testify/suite"
)

// BalancerTestSuite tests the core balancer functionality
type BalancerTestSuite struct {
	suite.Suite
	backends   []string
	mockServer *httptest.Server
	balancer   *Balancer
}

// SetupSuite runs once before all tests
func (s *BalancerTestSuite) SetupSuite() {
	// Create a mock backend server
	s.mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/node/info"):
			nodeInfo := models.NodeInfo{
				Uptime:        "1d 2h 3m",
				UptimeSeconds: 94980,
				LoadAverages: models.LoadAverages{
					Load1:  1.5,
					Load5:  2.0,
					Load15: 1.8,
				},
				Memory: models.MemoryInfo{
					Total:     8589934592, // 8GB
					Used:      4294967296, // 4GB
					Available: 4294967296, // 4GB
				},
				Storage: models.StorageInfo{
					Total:     107374182400, // 100GB
					Used:      53687091200,  // 50GB
					Available: 53687091200,  // 50GB
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(nodeInfo)
		case strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/download"):
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("test file content"))
		case strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/info"):
			fileInfo := models.FileInfo{
				Hash:      "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
				Size:      17,
				CreatedAt: time.Now(),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(fileInfo)
		case strings.Contains(r.URL.Path, "/file/") && strings.HasSuffix(r.URL.Path, "/delete"):
			w.Header().Set("Content-Type", "application/json")
			response := map[string]string{
				"message": "File deleted successfully",
				"hash":    "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			}
			json.NewEncoder(w).Encode(response)
		case strings.HasSuffix(r.URL.Path, "/file/upload"):
			w.Header().Set("Content-Type", "application/json")
			response := models.UploadResponse{
				Hash: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			}
			json.NewEncoder(w).Encode(response)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	s.backends = []string{s.mockServer.URL}
	s.balancer = NewBalancer(s.backends, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)
}

// TearDownSuite runs once after all tests
func (s *BalancerTestSuite) TearDownSuite() {
	if s.mockServer != nil {
		s.mockServer.Close()
	}
}

// TestNewBalancer tests the constructor
func (s *BalancerTestSuite) TestNewBalancer() {
	backends := []string{"http://backend1:8080", "http://backend2:8080"}
	retryMax := 3
	retryWaitMin := 100 * time.Millisecond
	retryWaitMax := 500 * time.Millisecond
	requestTimeout := 30 * time.Second

	balancer := NewBalancer(backends, retryMax, retryWaitMin, retryWaitMax, requestTimeout)

	s.NotNil(balancer)
	s.Equal(backends, balancer.backends)
	s.Equal(retryMax, balancer.retryMax)
	s.Equal(retryWaitMin, balancer.retryWaitMin)
	s.Equal(retryWaitMax, balancer.retryWaitMax)
	s.Equal(requestTimeout, balancer.requestTimeout)
	s.NotNil(balancer.client)
}

// TestGetNodeInfo tests node info retrieval
func (s *BalancerTestSuite) TestGetNodeInfo() {
	nodeInfo, err := s.balancer.getNodeInfo(context.Background(), s.mockServer.URL)
	s.NoError(err)
	s.NotNil(nodeInfo)
	s.Equal("1d 2h 3m", nodeInfo.Uptime)
	s.Equal(int64(94980), nodeInfo.UptimeSeconds)
	s.Equal(1.5, nodeInfo.LoadAverages.Load1)
	s.Equal(uint64(8589934592), nodeInfo.Memory.Total)
	s.Equal(uint64(53687091200), nodeInfo.Storage.Available)
}

// TestGetmodels.NodeInfoInvalidURL tests node info with invalid URL
func (s *BalancerTestSuite) TestGetNodeInfoInvalidURL() {
	_, err := s.balancer.getNodeInfo(context.Background(), "invalid-url")
	s.Error(err)
}

// TestGetmodels.NodeInfoNotFound tests node info when endpoint doesn't exist
func (s *BalancerTestSuite) TestGetNodeInfoNotFound() {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockServer.Close()

	_, err := s.balancer.getNodeInfo(context.Background(), mockServer.URL)
	s.Error(err)
}

// TestGetmodels.NodeInfoInvalidJSON tests node info with invalid JSON response
func (s *BalancerTestSuite) TestGetNodeInfoInvalidJSON() {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("invalid json"))
	}))
	defer mockServer.Close()

	_, err := s.balancer.getNodeInfo(context.Background(), mockServer.URL)
	s.Error(err)
}

// TestSelectBackendForUpload tests backend selection for upload
func (s *BalancerTestSuite) TestSelectBackendForUpload() {
	backend, err := s.balancer.selectBackendForUpload(context.Background(), 1024)
	s.NoError(err)
	s.Equal(s.mockServer.URL, backend)
}

// TestSelectBackendForUploadFileTooBig tests upload when file is too big
func (s *BalancerTestSuite) TestSelectBackendForUploadFileTooBig() {
	_, err := s.balancer.selectBackendForUpload(context.Background(), 100*1024*1024*1024) // 100GB file
	s.Error(err)
	s.Contains(err.Error(), "no backend has enough space")
}

// TestSelectBackendForUploadNegativeSize tests upload with negative file size
func (s *BalancerTestSuite) TestSelectBackendForUploadNegativeSize() {
	_, err := s.balancer.selectBackendForUpload(context.Background(), -1)
	s.Error(err)
}

// TestSelectBackendForUploadNoBackends tests upload when no backends are available
func (s *BalancerTestSuite) TestSelectBackendForUploadNoBackends() {
	emptyBalancer := NewBalancer([]string{}, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)
	_, err := emptyBalancer.selectBackendForUpload(context.Background(), 1024)
	s.Error(err)
}

// TestSelectBackendForUploadBackendsUnavailable tests upload when all backends are unavailable
func (s *BalancerTestSuite) TestSelectBackendForUploadBackendsUnavailable() {
	unavailableBackends := []string{"http://nonexistent1:8080", "http://nonexistent2:8080"}
	balancer := NewBalancer(unavailableBackends, 1, 50*time.Millisecond, 100*time.Millisecond, 1*time.Second)
	_, err := balancer.selectBackendForUpload(context.Background(), 1024)
	s.Error(err)
}

// TestSelectBackendForUploadMultipleBackends tests backend selection with multiple backends
func (s *BalancerTestSuite) TestSelectBackendForUploadMultipleBackends() {
	// Create another mock server with less available space
	mockServer2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/node/info") {
			nodeInfo := models.NodeInfo{
				Storage: models.StorageInfo{
					Total:     53687091200, // 50GB
					Used:      48318382080, // 45GB
					Available: 5368709120,  // 5GB (less than first server)
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(nodeInfo)
		}
	}))
	defer mockServer2.Close()

	backends := []string{s.mockServer.URL, mockServer2.URL}
	balancer := NewBalancer(backends, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)

	backend, err := balancer.selectBackendForUpload(context.Background(), 1024)
	s.NoError(err)
	// Should select the first server with more available space
	s.Equal(s.mockServer.URL, backend)
}

// TestSelectBackendForUploadWithTimeout tests backend selection with timeout
func (s *BalancerTestSuite) TestSelectBackendForUploadWithTimeout() {
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/node/info") {
			time.Sleep(2 * time.Second) // Longer than timeout
			nodeInfo := models.NodeInfo{
				Storage: models.StorageInfo{Available: 1000000},
			}
			json.NewEncoder(w).Encode(nodeInfo)
		}
	}))
	defer slowServer.Close()

	backends := []string{slowServer.URL}
	balancer := NewBalancer(backends, 1, 50*time.Millisecond, 100*time.Millisecond, 500*time.Millisecond)

	_, err := balancer.selectBackendForUpload(context.Background(), 1024)
	s.Error(err)
}

// TestBalancerTypes tests the type definitions
func (s *BalancerTestSuite) TestBalancerTypes() {
	// Test models.NodeInfo
	nodeInfo := models.NodeInfo{
		Uptime:        "test",
		UptimeSeconds: 123,
	}
	s.Equal("test", nodeInfo.Uptime)
	s.Equal(int64(123), nodeInfo.UptimeSeconds)

	// Test models.UploadResponse
	uploadResp := models.UploadResponse{Hash: "testhash"}
	s.Equal("testhash", uploadResp.Hash)

	// Test models.FileInfo
	fileInfo := models.FileInfo{
		Hash: "testhash",
		Size: 100,
	}
	s.Equal("testhash", fileInfo.Hash)
	s.Equal(int64(100), fileInfo.Size)
}

// TestBalancerConcurrentRequests tests concurrent backend selection
func (s *BalancerTestSuite) TestBalancerConcurrentRequests() {
	numRequests := 10
	results := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			backend, err := s.balancer.selectBackendForUpload(context.Background(), 1024)
			results <- err == nil && backend == s.mockServer.URL
		}()
	}

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		success := <-results
		s.True(success)
	}
}

// MockClosingReader simulates a reader that fails to close
type MockClosingReader struct {
	*bytes.Reader
	shouldFailClose bool
}

func (m *MockClosingReader) Close() error {
	if m.shouldFailClose {
		return io.ErrUnexpectedEOF
	}
	return nil
}

// TestBalancerErrorHandling tests error handling in the balancer
func (s *BalancerTestSuite) TestBalancerErrorHandling() {
	// Test with server that returns various error codes
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errorServer.Close()

	backends := []string{errorServer.URL}
	balancer := NewBalancer(backends, 1, 50*time.Millisecond, 100*time.Millisecond, 1*time.Second)

	_, err := balancer.selectBackendForUpload(context.Background(), 1024)
	s.Error(err)
}

// TestBalancerSuite runs the balancer test suite
func TestBalancerSuite(t *testing.T) {
	suite.Run(t, new(BalancerTestSuite))
}
