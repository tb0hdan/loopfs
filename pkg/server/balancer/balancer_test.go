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
	mockServer     *httptest.Server
	backendManager *BackendManager
	balancer       *Balancer
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

	backends := []string{s.mockServer.URL}
	s.backendManager = NewBackendManager(backends, 100*time.Millisecond, 5*time.Second)
	s.backendManager.Start()
	// Give health check time to run
	time.Sleep(200 * time.Millisecond)
	s.balancer = NewBalancer(s.backendManager, 3, 100*time.Millisecond, 500*time.Millisecond, 5*time.Second)
}

// TearDownSuite runs once after all tests
func (s *BalancerTestSuite) TearDownSuite() {
	if s.backendManager != nil {
		s.backendManager.Stop()
	}
	if s.mockServer != nil {
		s.mockServer.Close()
	}
}

// TestNewBalancer tests the constructor
func (s *BalancerTestSuite) TestNewBalancer() {
	backends := []string{"http://backend1:8080", "http://backend2:8080"}
	bm := NewBackendManager(backends, 5*time.Second, 5*time.Second)
	retryMax := 3
	retryWaitMin := 100 * time.Millisecond
	retryWaitMax := 500 * time.Millisecond
	requestTimeout := 30 * time.Second

	balancer := NewBalancer(bm, retryMax, retryWaitMin, retryWaitMax, requestTimeout)

	s.NotNil(balancer)
	s.Equal(bm, balancer.backendManager)
	s.Equal(requestTimeout, balancer.requestTimeout)
	s.NotNil(balancer.client)
}

// TestBackendManagerGetOnlineBackends tests getting online backends
func (s *BalancerTestSuite) TestBackendManagerGetOnlineBackends() {
	backends := s.backendManager.GetOnlineBackends()
	s.Equal(1, len(backends))
	s.Equal(s.mockServer.URL, backends[0])
}

// TestBackendManagerGetBackendForUpload tests backend selection for upload
func (s *BalancerTestSuite) TestBackendManagerGetBackendForUpload() {
	backend, err := s.backendManager.GetBackendForUpload(1024)
	s.NoError(err)
	s.Equal(s.mockServer.URL, backend)
}

// TestBackendManagerGetBackendForUploadFileTooBig tests upload when file is too big
func (s *BalancerTestSuite) TestBackendManagerGetBackendForUploadFileTooBig() {
	_, err := s.backendManager.GetBackendForUpload(100 * 1024 * 1024 * 1024) // 100GB file
	s.Error(err)
	s.Equal(ErrNoBackendAvailable, err)
}

// TestBackendManagerNoBackends tests with no backends
func (s *BalancerTestSuite) TestBackendManagerNoBackends() {
	emptyManager := NewBackendManager([]string{}, 5*time.Second, 5*time.Second)
	_, err := emptyManager.GetBackendForUpload(1024)
	s.Error(err)
	s.Equal(ErrNoBackendAvailable, err)
}

// TestBackendManagerBackendsUnavailable tests when all backends are unavailable
func (s *BalancerTestSuite) TestBackendManagerBackendsUnavailable() {
	unavailableBackends := []string{"http://nonexistent1:8080", "http://nonexistent2:8080"}
	bm := NewBackendManager(unavailableBackends, 100*time.Millisecond, 500*time.Millisecond)
	bm.Start()
	defer bm.Stop()

	// Wait for health checks to mark backends as offline
	time.Sleep(300 * time.Millisecond)

	_, err := bm.GetBackendForUpload(1024)
	s.Error(err)
}

// TestBackendManagerMultipleBackends tests backend selection with multiple backends
func (s *BalancerTestSuite) TestBackendManagerMultipleBackends() {
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
	bm := NewBackendManager(backends, 100*time.Millisecond, 5*time.Second)
	bm.Start()
	defer bm.Stop()

	// Wait for health checks
	time.Sleep(200 * time.Millisecond)

	backend, err := bm.GetBackendForUpload(1024)
	s.NoError(err)
	// Should select the first server with more available space
	s.Equal(s.mockServer.URL, backend)
}

// TestBackendManagerWithTimeout tests backend selection with timeout
func (s *BalancerTestSuite) TestBackendManagerWithTimeout() {
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
	bm := NewBackendManager(backends, 100*time.Millisecond, 500*time.Millisecond)
	bm.Start()
	defer bm.Stop()

	// Wait for health checks to fail
	time.Sleep(300 * time.Millisecond)

	_, err := bm.GetBackendForUpload(1024)
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

// TestBackendManagerConcurrentRequests tests concurrent backend selection
func (s *BalancerTestSuite) TestBackendManagerConcurrentRequests() {
	numRequests := 10
	results := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			backend, err := s.backendManager.GetBackendForUpload(1024)
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

// TestBackendManagerErrorHandling tests error handling in the backend manager
func (s *BalancerTestSuite) TestBackendManagerErrorHandling() {
	// Test with server that returns various error codes
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errorServer.Close()

	backends := []string{errorServer.URL}
	bm := NewBackendManager(backends, 100*time.Millisecond, 1*time.Second)
	bm.Start()
	defer bm.Stop()

	// Wait for health checks to mark as offline
	time.Sleep(300 * time.Millisecond)

	_, err := bm.GetBackendForUpload(1024)
	s.Error(err)
}

// TestBackendManagerMarkBackendDead tests marking a backend as dead
func (s *BalancerTestSuite) TestBackendManagerMarkBackendDead() {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/node/info") {
			nodeInfo := models.NodeInfo{
				Storage: models.StorageInfo{Available: 1000000},
			}
			json.NewEncoder(w).Encode(nodeInfo)
		}
	}))
	defer mockServer.Close()

	backends := []string{mockServer.URL}
	bm := NewBackendManager(backends, 5*time.Second, 5*time.Second) // Long interval so we control when it checks
	bm.Start()
	defer bm.Stop()

	// Wait for initial health check
	time.Sleep(200 * time.Millisecond)

	// Backend should be online
	s.True(bm.HasOnlineBackends())

	// Mark backend as dead
	bm.MarkBackendDead(mockServer.URL, context.DeadlineExceeded)

	// Backend should now be offline
	s.False(bm.HasOnlineBackends())
}

// TestBackendManagerGetAllBackendStatus tests getting all backend statuses
func (s *BalancerTestSuite) TestBackendManagerGetAllBackendStatus() {
	statuses := s.backendManager.GetAllBackendStatus()
	s.Equal(1, len(statuses))
	s.Equal(s.mockServer.URL, statuses[0].URL)
	s.True(statuses[0].Online)
}

// TestBackendManagerBackendCount tests the backend count
func (s *BalancerTestSuite) TestBackendManagerBackendCount() {
	count := s.backendManager.BackendCount()
	s.Equal(1, count)
}

// TestBalancerSuite runs the balancer test suite
func TestBalancerSuite(t *testing.T) {
	suite.Run(t, new(BalancerTestSuite))
}
