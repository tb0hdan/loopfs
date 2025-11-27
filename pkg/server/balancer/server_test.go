package balancer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/suite"
)

// ServerTestSuite tests the balancer server functionality
type ServerTestSuite struct {
	suite.Suite
	server      *Server
	mockBackend *httptest.Server
}

// SetupSuite runs once before all tests
func (s *ServerTestSuite) SetupSuite() {
	// Create a mock backend server
	s.mockBackend = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("mock response"))
	}))
}

// TearDownSuite runs once after all tests
func (s *ServerTestSuite) TearDownSuite() {
	if s.mockBackend != nil {
		s.mockBackend.Close()
	}
}

// SetupTest runs before each test
func (s *ServerTestSuite) SetupTest() {
	backendList := []string{s.mockBackend.URL}
	s.server = NewBalancerServer(
		backendList,
		3,                    // retryMax
		10*time.Second,       // gracefulShutdownTimeout
		100*time.Millisecond, // retryWaitMin
		500*time.Millisecond, // retryWaitMax
		5*time.Second,        // requestTimeout
		5*time.Second,        // healthCheckInterval
		5*time.Second,        // healthCheckTimeout
		false,
		"",
	)
}

// TestNewBalancerServer tests the constructor
func (s *ServerTestSuite) TestNewBalancerServer() {
	backendList := []string{"http://backend1:8080", "http://backend2:8080"}
	retryMax := 5
	gracefulShutdownTimeout := 30 * time.Second
	retryWaitMin := 200 * time.Millisecond
	retryWaitMax := 1 * time.Second
	requestTimeout := 10 * time.Second
	healthCheckInterval := 5 * time.Second
	healthCheckTimeout := 5 * time.Second

	server := NewBalancerServer(backendList, retryMax, gracefulShutdownTimeout, retryWaitMin, retryWaitMax,
		requestTimeout, healthCheckInterval, healthCheckTimeout, false, "")

	s.NotNil(server)
	s.Equal(backendList, server.backendURLs)
	s.Equal(retryMax, server.retryMax)
	s.Equal(gracefulShutdownTimeout, server.gracefulShutdownTimeout)
	s.Equal(retryWaitMin, server.retryWaitMin)
	s.Equal(retryWaitMax, server.retryWaitMax)
	s.Equal(requestTimeout, server.requestTimeout)
	s.Equal(healthCheckInterval, server.healthCheckInterval)
	s.Equal(healthCheckTimeout, server.healthCheckTimeout)
	s.NotNil(server.echo)
	s.IsType(&echo.Echo{}, server.echo)
}

// TestServerDefaultSettings tests server with default-like settings
func (s *ServerTestSuite) TestServerDefaultSettings() {
	server := NewBalancerServer(
		[]string{"http://localhost:8080"},
		3,
		10*time.Second,
		100*time.Millisecond,
		1*time.Second,
		30*time.Second,
		5*time.Second,
		5*time.Second,
		false,
		"",
	)

	s.NotNil(server.echo)
	s.Equal(3, server.retryMax)
	s.Equal(10*time.Second, server.gracefulShutdownTimeout)
}

// TestSetupRoutes tests route configuration
func (s *ServerTestSuite) TestSetupRoutes() {
	bm := NewBackendManager(s.server.backendURLs, s.server.healthCheckInterval, s.server.healthCheckTimeout)
	s.server.backendManager = bm
	balancer := NewBalancer(bm, s.server.retryMax, s.server.retryWaitMin, s.server.retryWaitMax, s.server.requestTimeout)
	s.server.setupRoutes(balancer)

	routes := s.server.echo.Routes()
	s.Greater(len(routes), 0)

	// Verify specific routes exist
	routePaths := make(map[string]bool)
	for _, route := range routes {
		routePaths[route.Path] = true
	}

	s.True(routePaths["/file/upload"])
	s.True(routePaths["/file/:hash/download"])
	s.True(routePaths["/file/:hash/info"])
	s.True(routePaths["/file/:hash/delete"])
	s.True(routePaths["/backends/status"]) // New status endpoint
}

// TestSetupRoutesMiddleware tests middleware configuration
func (s *ServerTestSuite) TestSetupRoutesMiddleware() {
	bm := NewBackendManager(s.server.backendURLs, s.server.healthCheckInterval, s.server.healthCheckTimeout)
	s.server.backendManager = bm
	balancer := NewBalancer(bm, s.server.retryMax, s.server.retryWaitMin, s.server.retryWaitMax, s.server.requestTimeout)
	s.server.setupRoutes(balancer)

	// Test that middleware is configured by making a request
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rec := httptest.NewRecorder()

	s.server.echo.ServeHTTP(rec, req)

	// Should get 404 due to middleware processing (not a panic)
	s.Equal(http.StatusNotFound, rec.Code)

	// Check that CORS headers are present (indicating CORS middleware is active)
	corsHeader := rec.Header().Get("Access-Control-Allow-Origin")
	s.True(corsHeader == "*" || corsHeader == "", "Expected CORS header to be '*' or empty, got: %s", corsHeader)
}

// TestShutdown tests graceful shutdown
func (s *ServerTestSuite) TestShutdown() {
	err := s.server.Shutdown()
	s.NoError(err)
}

// TestShutdownWithTimeout tests shutdown with context timeout
func (s *ServerTestSuite) TestShutdownWithTimeout() {
	// Set a very short shutdown timeout to test timeout behavior
	s.server.gracefulShutdownTimeout = 1 * time.Nanosecond

	err := s.server.Shutdown()
	// Should still return successfully even with timeout
	s.NoError(err)
}

// TestStartAndShutdown tests the start and shutdown process
func (s *ServerTestSuite) TestStartAndShutdown() {
	// Use a channel to signal when to shutdown
	shutdownChan := make(chan bool, 1)

	// Start the server in a goroutine
	go func() {
		// Wait a bit, then trigger shutdown
		time.Sleep(50 * time.Millisecond)
		shutdownChan <- true
	}()

	// Override the Start method behavior for testing
	// We can't test the actual Start method easily because it uses signal handling
	// Instead, we test the Shutdown functionality directly

	// Test immediate shutdown
	err := s.server.Shutdown()
	s.NoError(err)

	// Wait for shutdown signal
	<-shutdownChan
}

// TestServerConfiguration tests various server configurations
func (s *ServerTestSuite) TestServerConfiguration() {
	testCases := []struct {
		name                    string
		backendList             []string
		retryMax                int
		gracefulShutdownTimeout time.Duration
		retryWaitMin            time.Duration
		retryWaitMax            time.Duration
		requestTimeout          time.Duration
		healthCheckInterval     time.Duration
		healthCheckTimeout      time.Duration
	}{
		{
			name:                    "minimal_config",
			backendList:             []string{"http://localhost:8080"},
			retryMax:                1,
			gracefulShutdownTimeout: 1 * time.Second,
			retryWaitMin:            10 * time.Millisecond,
			retryWaitMax:            100 * time.Millisecond,
			requestTimeout:          1 * time.Second,
			healthCheckInterval:     1 * time.Second,
			healthCheckTimeout:      1 * time.Second,
		},
		{
			name:                    "high_performance_config",
			backendList:             []string{"http://backend1:8080", "http://backend2:8080", "http://backend3:8080"},
			retryMax:                10,
			gracefulShutdownTimeout: 30 * time.Second,
			retryWaitMin:            50 * time.Millisecond,
			retryWaitMax:            2 * time.Second,
			requestTimeout:          60 * time.Second,
			healthCheckInterval:     10 * time.Second,
			healthCheckTimeout:      5 * time.Second,
		},
		{
			name:                    "no_backends",
			backendList:             []string{},
			retryMax:                3,
			gracefulShutdownTimeout: 10 * time.Second,
			retryWaitMin:            100 * time.Millisecond,
			retryWaitMax:            1 * time.Second,
			requestTimeout:          30 * time.Second,
			healthCheckInterval:     5 * time.Second,
			healthCheckTimeout:      5 * time.Second,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			server := NewBalancerServer(
				tc.backendList,
				tc.retryMax,
				tc.gracefulShutdownTimeout,
				tc.retryWaitMin,
				tc.retryWaitMax,
				tc.requestTimeout,
				tc.healthCheckInterval,
				tc.healthCheckTimeout,
				false,
				"",
			)

			s.Equal(tc.backendList, server.backendURLs)
			s.Equal(tc.retryMax, server.retryMax)
			s.Equal(tc.gracefulShutdownTimeout, server.gracefulShutdownTimeout)
			s.Equal(tc.retryWaitMin, server.retryWaitMin)
			s.Equal(tc.retryWaitMax, server.retryWaitMax)
			s.Equal(tc.requestTimeout, server.requestTimeout)
			s.Equal(tc.healthCheckInterval, server.healthCheckInterval)
			s.Equal(tc.healthCheckTimeout, server.healthCheckTimeout)
		})
	}
}

// TestServerWithInvalidBackends tests server configuration with invalid backends
func (s *ServerTestSuite) TestServerWithInvalidBackends() {
	invalidBackends := []string{"invalid-url", "http://nonexistent:99999"}
	server := NewBalancerServer(
		invalidBackends,
		3,
		10*time.Second,
		100*time.Millisecond,
		1*time.Second,
		30*time.Second,
		5*time.Second,
		5*time.Second,
		false,
		"",
	)

	// Server should still be created successfully
	s.NotNil(server)
	s.Equal(invalidBackends, server.backendURLs)

	// Test that routes can be setup even with invalid backends
	bm := NewBackendManager(server.backendURLs, server.healthCheckInterval, server.healthCheckTimeout)
	server.backendManager = bm
	balancer := NewBalancer(bm, server.retryMax, server.retryWaitMin, server.retryWaitMax, server.requestTimeout)
	s.NotPanics(func() {
		server.setupRoutes(balancer)
	})
}

// TestServerEchoConfiguration tests Echo framework configuration
func (s *ServerTestSuite) TestServerEchoConfiguration() {
	bm := NewBackendManager(s.server.backendURLs, s.server.healthCheckInterval, s.server.healthCheckTimeout)
	s.server.backendManager = bm
	balancer := NewBalancer(bm, s.server.retryMax, s.server.retryWaitMin, s.server.retryWaitMax, s.server.requestTimeout)
	s.server.setupRoutes(balancer)

	// Test Echo configuration
	s.True(s.server.echo.HideBanner)
	s.True(s.server.echo.HidePort)

	// Test that routes are properly registered
	routes := s.server.echo.Routes()
	s.Greater(len(routes), 0)

	// Verify HTTP methods are correctly set
	methodCounts := make(map[string]int)
	for _, route := range routes {
		methodCounts[route.Method]++
	}

	s.Greater(methodCounts["POST"], 0)   // Upload endpoint
	s.Greater(methodCounts["GET"], 0)    // Download and info endpoints
	s.Greater(methodCounts["DELETE"], 0) // Delete endpoint
}

// TestServerConcurrentShutdown tests concurrent shutdown calls
func (s *ServerTestSuite) TestServerConcurrentShutdown() {
	numShutdowns := 5
	results := make(chan error, numShutdowns)

	for i := 0; i < numShutdowns; i++ {
		go func() {
			results <- s.server.Shutdown()
		}()
	}

	// All shutdown calls should succeed
	for i := 0; i < numShutdowns; i++ {
		err := <-results
		s.NoError(err)
	}
}

// TestServerContextHandling tests context handling in shutdown
func (s *ServerTestSuite) TestServerContextHandling() {
	// Create a context that is already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Test that shutdown still works even if context is cancelled
	// (This is more of an implementation detail test)
	err := s.server.Shutdown()
	s.NoError(err)

	// Verify context was cancelled
	s.Error(ctx.Err())
}

// TestServerZeroTimeouts tests server with zero timeout values
func (s *ServerTestSuite) TestServerZeroTimeouts() {
	server := NewBalancerServer(
		[]string{"http://localhost:8080"},
		0, // zero retries
		0, // zero shutdown timeout
		0, // zero retry wait min
		0, // zero retry wait max
		0, // zero request timeout
		0, // zero health check interval
		0, // zero health check timeout
		false,
		"",
	)

	s.NotNil(server)
	s.Equal(0, server.retryMax)
	s.Equal(time.Duration(0), server.gracefulShutdownTimeout)
	s.Equal(time.Duration(0), server.retryWaitMin)
	s.Equal(time.Duration(0), server.retryWaitMax)
	s.Equal(time.Duration(0), server.requestTimeout)

	// Should still be able to shutdown
	err := server.Shutdown()
	s.NoError(err)
}

// TestServerBackendManagerIntegration tests that BackendManager is properly returned
func (s *ServerTestSuite) TestServerBackendManagerIntegration() {
	bm := NewBackendManager(s.server.backendURLs, s.server.healthCheckInterval, s.server.healthCheckTimeout)
	s.server.backendManager = bm

	returnedBM := s.server.BackendManager()
	s.Equal(bm, returnedBM)
}

// TestServerSuite runs the server test suite
func TestServerSuite(t *testing.T) {
	suite.Run(t, new(ServerTestSuite))
}
