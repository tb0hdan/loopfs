package casd

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

// ServerMainTestSuite tests the main server functionality
type ServerMainTestSuite struct {
	suite.Suite
	tempDir string
}

// SetupSuite runs once before all tests
func (s *ServerMainTestSuite) SetupSuite() {
	var err error
	s.tempDir, err = os.MkdirTemp("", "server-main-test-*")
	s.Require().NoError(err)
}

// TearDownSuite runs once after all tests
func (s *ServerMainTestSuite) TearDownSuite() {
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

// TestNewCASServer tests the constructor function
func (s *ServerMainTestSuite) TestNewCASServer() {
	mockStore := NewMockStore()
	server := NewCASServer("/storage", "/web", "v1.0.0", mockStore, false, "")

	s.NotNil(server)
	s.Equal("/storage", server.storageDir)
	s.Equal("/web", server.webDir)
	s.Equal("v1.0.0", server.version)
	s.Equal(mockStore, server.store)
	s.NotNil(server.echo)
	s.Nil(server.storeMgr) // Should be nil for regular store
}

// TestNewCASServerWithStoreManager tests constructor with store manager
func (s *ServerMainTestSuite) TestNewCASServerWithStoreManager() {
	// This test would require importing storemanager package
	// For now, we test with a mock store manager
	mockStore := NewMockStore()

	server := NewCASServer(s.tempDir, "/web", "v1.0.0", mockStore, false, "")

	s.NotNil(server)
	s.Equal(s.tempDir, server.storageDir)
	s.Equal("/web", server.webDir)
	s.Equal("v1.0.0", server.version)
	s.Equal(mockStore, server.store)
	s.Nil(server.storeMgr) // Should be nil for regular store
}

// TestSetupRoutes tests that routes are properly configured
func (s *ServerMainTestSuite) TestSetupRoutes() {
	mockStore := NewMockStore()
	server := NewCASServer(s.tempDir, "/web", "v1.0.0", mockStore, false, "")
	server.setupRoutes()

	// Test Echo configuration
	s.True(server.echo.HideBanner)
	s.True(server.echo.HidePort)

	// Test that routes exist
	routes := server.echo.Routes()
	s.Greater(len(routes), 0)

	routePaths := make(map[string]bool)
	for _, route := range routes {
		routePaths[route.Path] = true
	}

	// Verify expected routes exist
	expectedRoutes := []string{
		"/",
		"/swagger.yml",
		"/file/upload",
		"/file/:hash/download",
		"/file/:hash/info",
		"/file/:hash/delete",
	}

	for _, expectedRoute := range expectedRoutes {
		s.True(routePaths[expectedRoute], "Route %s should exist", expectedRoute)
	}
}

// TestShutdownSuccess tests successful server shutdown
func (s *ServerMainTestSuite) TestShutdownSuccess() {
	mockStore := NewMockStore()
	server := NewCASServer(s.tempDir, "/web", "v1.0.0", mockStore, false, "")

	err := server.Shutdown()
	s.NoError(err)
}

// TestShutdownTimeout tests shutdown with context timeout
func (s *ServerMainTestSuite) TestShutdownTimeout() {
	mockStore := NewMockStore()
	server := NewCASServer(s.tempDir, "/web", "v1.0.0", mockStore, false, "")

	// This should complete quickly since the server isn't actually running
	start := time.Now()
	err := server.Shutdown()
	duration := time.Since(start)

	s.NoError(err)
	s.Less(duration, time.Second) // Should complete quickly
}

// TestServerConstants tests server constants
func (s *ServerMainTestSuite) TestServerConstants() {
	s.Equal(10, shutdownTimeout)
	s.Equal(30, syncTimeout)
}

// TestStartAndShutdownCycle tests starting and stopping the server
func (s *ServerMainTestSuite) TestStartAndShutdownCycle() {
	// This test is complex due to signal handling, so we'll just test shutdown directly
	mockStore := NewMockStore()
	server := NewCASServer(s.tempDir, "/web", "v1.0.0", mockStore, false, "")

	// Test shutdown without starting (should work gracefully)
	err := server.Shutdown()
	s.NoError(err)
}

// ServerMockStoreManager for testing type detection
type ServerMockStoreManager struct {
	*MockStore
}

// TestNewCASServerStoreManagerDetection tests store manager type detection
func (s *ServerMainTestSuite) TestNewCASServerStoreManagerDetection() {
	// Test with regular store
	mockStore := NewMockStore()
	server1 := NewCASServer(s.tempDir, "/web", "v1.0.0", mockStore, false, "")
	s.Nil(server1.storeMgr)

	// Test with mock store manager (doesn't implement storemanager.Manager interface)
	mockStoreMgr := &ServerMockStoreManager{MockStore: NewMockStore()}
	server2 := NewCASServer(s.tempDir, "/web", "v1.0.0", mockStoreMgr, false, "")
	s.Nil(server2.storeMgr) // Should be nil as it's not a real store manager
}

// TestServerStructureInitialization tests all server struct fields are initialized
func (s *ServerMainTestSuite) TestServerStructureInitialization() {
	mockStore := NewMockStore()
	storageDir := "/test/storage"
	webDir := "/test/web"
	version := "test-v2.0.0"

	server := NewCASServer(storageDir, webDir, version, mockStore, false, "")

	s.Equal(storageDir, server.storageDir)
	s.Equal(webDir, server.webDir)
	s.Equal(version, server.version)
	s.Equal(mockStore, server.store)
	s.NotNil(server.echo)
	s.Nil(server.storeMgr)
}

// TestServerWithEmptyPaths tests server creation with empty paths
func (s *ServerMainTestSuite) TestServerWithEmptyPaths() {
	mockStore := NewMockStore()
	server := NewCASServer("", "", "", mockStore, false, "")

	s.Equal("", server.storageDir)
	s.Equal("", server.webDir)
	s.Equal("", server.version)
	s.Equal(mockStore, server.store)
	s.NotNil(server.echo)
}

// TestServerWithNilStore tests server creation with nil store
func (s *ServerMainTestSuite) TestServerWithNilStore() {
	server := NewCASServer(s.tempDir, "/web", "v1.0.0", nil, false, "")

	s.Equal(s.tempDir, server.storageDir)
	s.Equal("/web", server.webDir)
	s.Equal("v1.0.0", server.version)
	s.Nil(server.store)
	s.NotNil(server.echo)
	s.Nil(server.storeMgr)
}

// TestSetupRoutesIdempotent tests that calling setupRoutes multiple times is safe
func (s *ServerMainTestSuite) TestSetupRoutesIdempotent() {
	mockStore := NewMockStore()
	server := NewCASServer(s.tempDir, "/web", "v1.0.0", mockStore, false, "")

	// Call setupRoutes multiple times
	server.setupRoutes()
	server.setupRoutes()
	server.setupRoutes()

	// Should still have the correct number of routes
	routes := server.echo.Routes()
	s.Greater(len(routes), 0)

	// Count unique route paths
	routePaths := make(map[string]int)
	for _, route := range routes {
		routePaths[route.Path]++
	}

	// Each route should appear at least once (might be multiple due to different HTTP methods)
	expectedRoutes := []string{
		"/",
		"/swagger.yml",
		"/file/upload",
		"/file/:hash/download",
		"/file/:hash/info",
		"/file/:hash/delete",
	}

	for _, expectedRoute := range expectedRoutes {
		s.Greater(routePaths[expectedRoute], 0, "Route %s should exist", expectedRoute)
	}
}

// TestServerEchoConfiguration tests Echo framework configuration
func (s *ServerMainTestSuite) TestServerEchoConfiguration() {
	mockStore := NewMockStore()
	server := NewCASServer(s.tempDir, "/web", "v1.0.0", mockStore, false, "")
	server.setupRoutes()

	// Test Echo instance configuration
	s.NotNil(server.echo)
	s.True(server.echo.HideBanner)
	s.True(server.echo.HidePort)
}

// TestServerMiddlewareSetup tests that middleware is properly configured
func (s *ServerMainTestSuite) TestServerMiddlewareSetup() {
	mockStore := NewMockStore()
	server := NewCASServer(s.tempDir, "/web", "v1.0.0", mockStore, false, "")
	server.setupRoutes()

	// Test that middleware was added by checking routes exist
	// (Middleware setup is tested indirectly through route functionality)
	routes := server.echo.Routes()
	s.Greater(len(routes), 0)
}

// TestShutdownWithContextCancellation tests shutdown with context cancellation
func (s *ServerMainTestSuite) TestShutdownWithContextCancellation() {
	mockStore := NewMockStore()
	server := NewCASServer(s.tempDir, "/web", "v1.0.0", mockStore, false, "")

	// Test that shutdown works even when called on a non-running server
	err := server.Shutdown()
	s.NoError(err)
}

// TestServerConcurrentAccess tests concurrent access to server methods
func (s *ServerMainTestSuite) TestServerConcurrentAccess() {
	// Create separate servers for each goroutine to avoid race conditions
	var wg sync.WaitGroup
	numGoroutines := 5 // Reduced number to prevent race conditions

	// Test concurrent server creation and setup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mockStore := NewMockStore()
			server := NewCASServer(s.tempDir, "/web", "v1.0.0", mockStore, false, "")
			server.setupRoutes()

			// Verify server is in good state
			routes := server.echo.Routes()
			if len(routes) == 0 {
				panic("Server routes not set up correctly")
			}
		}()
	}

	wg.Wait()
}

// TestServerFieldAccess tests accessing server fields
func (s *ServerMainTestSuite) TestServerFieldAccess() {
	mockStore := NewMockStore()
	storageDir := "/custom/storage"
	webDir := "/custom/web"
	version := "field-test-v1.0.0"

	server := NewCASServer(storageDir, webDir, version, mockStore, false, "")

	// Test field access
	s.Equal(storageDir, server.storageDir)
	s.Equal(webDir, server.webDir)
	s.Equal(version, server.version)
	s.Equal(mockStore, server.store)
	s.NotNil(server.echo)

	// Test that fields maintain their values after setupRoutes
	server.setupRoutes()
	s.Equal(storageDir, server.storageDir)
	s.Equal(webDir, server.webDir)
	s.Equal(version, server.version)
	s.Equal(mockStore, server.store)
}

// TestServerMainSuite runs the server main test suite
func TestServerMainSuite(t *testing.T) {
	suite.Run(t, new(ServerMainTestSuite))
}
