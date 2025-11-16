package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

// SwaggerTestSuite tests the swagger functionality
type SwaggerTestSuite struct {
	suite.Suite
	server    *CASServer
	mockStore *MockStore
	tempDir   string
	webDir    string
}

// SetupSuite runs once before all tests
func (s *SwaggerTestSuite) SetupSuite() {
	var err error
	s.tempDir, err = os.MkdirTemp("", "swagger-test-*")
	s.Require().NoError(err)

	// Create a separate web directory for swagger files
	s.webDir, err = os.MkdirTemp("", "swagger-web-*")
	s.Require().NoError(err)
}

// TearDownSuite runs once after all tests
func (s *SwaggerTestSuite) TearDownSuite() {
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
	if s.webDir != "" {
		os.RemoveAll(s.webDir)
	}
}

// SetupTest runs before each test
func (s *SwaggerTestSuite) SetupTest() {
	s.mockStore = NewMockStore()
	s.server = NewCASServer(s.tempDir, s.webDir, "test-v1.0.0", s.mockStore)
	s.server.setupRoutes()
}

// TearDownTest runs after each test to clean up files
func (s *SwaggerTestSuite) TearDownTest() {
	// Clean up any files in webDir between tests
	tmplPath := filepath.Join(s.webDir, "swagger-ui.html")
	if _, err := os.Stat(tmplPath); err == nil {
		os.RemoveAll(tmplPath)
	}

	specPath := filepath.Join(s.webDir, "swagger.yml")
	if _, err := os.Stat(specPath); err == nil {
		os.RemoveAll(specPath)
	}
}

// TestServeSwaggerUISuccess tests successful swagger UI serving
func (s *SwaggerTestSuite) TestServeSwaggerUISuccess() {
	// Create a valid swagger UI template file
	tmplContent := `<!DOCTYPE html>
<html>
<head>
    <title>{{.Title}}</title>
</head>
<body>
    <div id="swagger-ui">
        <script>
            SwaggerUIBundle({
                url: '{{.SwaggerPath}}',
                dom_id: '#swagger-ui'
            });
        </script>
    </div>
</body>
</html>`

	tmplPath := filepath.Join(s.webDir, "swagger-ui.html")
	err := os.WriteFile(tmplPath, []byte(tmplContent), 0644)
	s.Require().NoError(err)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err = s.server.serveSwaggerUI(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Equal("text/html; charset=UTF-8", rec.Header().Get("Content-Type"))

	// Verify template variables were substituted
	body := rec.Body.String()
	s.Contains(body, "CAS Server API Documentation")
	s.Contains(body, "/swagger.yml")
	s.Contains(body, "<!DOCTYPE html>")
}

// TestServeSwaggerUITemplateNotFound tests swagger UI when template file doesn't exist
func (s *SwaggerTestSuite) TestServeSwaggerUITemplateNotFound() {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err := s.server.serveSwaggerUI(c)
	s.NoError(err) // Function handles error internally
	s.Equal(http.StatusInternalServerError, rec.Code)
	s.Contains(rec.Body.String(), "Failed to load template")
}

// TestServeSwaggerUIInvalidTemplate tests swagger UI with invalid template syntax
func (s *SwaggerTestSuite) TestServeSwaggerUIInvalidTemplate() {
	// Create an invalid template file
	invalidTmpl := `<!DOCTYPE html>
<html>
<head>
    <title>{{.Title}}</title>
</head>
<body>
    <div>{{.InvalidSyntax</div>
</body>
</html>`

	tmplPath := filepath.Join(s.webDir, "swagger-ui.html")
	err := os.WriteFile(tmplPath, []byte(invalidTmpl), 0644)
	s.Require().NoError(err)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err = s.server.serveSwaggerUI(c)
	s.NoError(err)
	s.Equal(http.StatusInternalServerError, rec.Code)
	s.Contains(rec.Body.String(), "Failed to load template")
}

// TestServeSwaggerUITemplateExecutionError tests template execution error
func (s *SwaggerTestSuite) TestServeSwaggerUITemplateExecutionError() {
	// Create a template that will fail during execution
	tmplContent := `<!DOCTYPE html>
<html>
<head>
    <title>{{.NonExistentField}}</title>
</head>
<body>
    <div>{{.Title}}</div>
</body>
</html>`

	tmplPath := filepath.Join(s.webDir, "swagger-ui.html")
	err := os.WriteFile(tmplPath, []byte(tmplContent), 0644)
	s.Require().NoError(err)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err = s.server.serveSwaggerUI(c)
	s.Error(err) // Template execution error should be returned
}

// TestServeSwaggerUIEmptyTemplate tests swagger UI with empty template
func (s *SwaggerTestSuite) TestServeSwaggerUIEmptyTemplate() {
	tmplPath := filepath.Join(s.webDir, "swagger-ui.html")
	err := os.WriteFile(tmplPath, []byte(""), 0644)
	s.Require().NoError(err)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err = s.server.serveSwaggerUI(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Equal("", rec.Body.String())
}

// TestServeSwaggerUIComplexTemplate tests swagger UI with complex template
func (s *SwaggerTestSuite) TestServeSwaggerUIComplexTemplate() {
	tmplContent := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    <link rel="stylesheet" type="text/css" href="swagger-ui-bundle.css" />
    <style>
        html {
            box-sizing: border-box;
            overflow: -moz-scrollbars-vertical;
            overflow-y: scroll;
        }
    </style>
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="swagger-ui-bundle.js"></script>
    <script>
        window.onload = function() {
            SwaggerUIBundle({
                url: '{{.SwaggerPath}}',
                dom_id: '#swagger-ui',
                deepLinking: true,
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIStandalonePreset
                ],
                plugins: [
                    SwaggerUIBundle.plugins.DownloadUrl
                ],
                layout: "StandaloneLayout"
            });
        };
    </script>
</body>
</html>`

	tmplPath := filepath.Join(s.webDir, "swagger-ui.html")
	err := os.WriteFile(tmplPath, []byte(tmplContent), 0644)
	s.Require().NoError(err)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err = s.server.serveSwaggerUI(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	body := rec.Body.String()
	s.Contains(body, "CAS Server API Documentation")
	s.Contains(body, "/swagger.yml")
	s.Contains(body, "swagger-ui-bundle.js")
	s.Contains(body, "SwaggerUIBundle")
}

// TestServeSwaggerSpecSuccess tests successful swagger spec serving
func (s *SwaggerTestSuite) TestServeSwaggerSpecSuccess() {
	// Create a valid swagger spec file
	specContent := `openapi: 3.0.0
info:
  title: CAS Server API
  version: 1.0.0
  description: Content Addressable Storage Server API
paths:
  /file/upload:
    post:
      summary: Upload a file
      responses:
        200:
          description: Successful upload
  /file/{hash}/download:
    get:
      summary: Download a file by hash
      parameters:
        - name: hash
          in: path
          required: true
          schema:
            type: string
      responses:
        200:
          description: File content`

	specPath := filepath.Join(s.webDir, "swagger.yml")
	err := os.WriteFile(specPath, []byte(specContent), 0644)
	s.Require().NoError(err)

	req := httptest.NewRequest(http.MethodGet, "/swagger.yml", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err = s.server.serveSwaggerSpec(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Contains(rec.Body.String(), "openapi: 3.0.0")
	s.Contains(rec.Body.String(), "CAS Server API")
	s.Contains(rec.Body.String(), "/file/upload")
}

// TestServeSwaggerSpecFileNotFound tests swagger spec when file doesn't exist
func (s *SwaggerTestSuite) TestServeSwaggerSpecFileNotFound() {
	req := httptest.NewRequest(http.MethodGet, "/swagger.yml", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err := s.server.serveSwaggerSpec(c)
	s.Error(err) // Echo's ctx.File returns error for missing files
}

// TestServeSwaggerSpecEmptyFile tests swagger spec with empty file
func (s *SwaggerTestSuite) TestServeSwaggerSpecEmptyFile() {
	specPath := filepath.Join(s.webDir, "swagger.yml")
	err := os.WriteFile(specPath, []byte(""), 0644)
	s.Require().NoError(err)

	req := httptest.NewRequest(http.MethodGet, "/swagger.yml", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err = s.server.serveSwaggerSpec(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Equal("", rec.Body.String())
}

// TestServeSwaggerSpecLargeFile tests swagger spec with large file
func (s *SwaggerTestSuite) TestServeSwaggerSpecLargeFile() {
	// Create a large swagger spec (multiple paths and schemas)
	largeSpec := `openapi: 3.0.0
info:
  title: Large CAS Server API
  version: 1.0.0
  description: Content Addressable Storage Server API with many endpoints
paths:`

	// Add many paths to make it large
	for i := 0; i < 100; i++ {
		largeSpec += `
  /endpoint` + string(rune('0'+(i%10))) + `:
    get:
      summary: Test endpoint ` + string(rune('0'+(i%10))) + `
      responses:
        200:
          description: Success`
	}

	specPath := filepath.Join(s.webDir, "swagger.yml")
	err := os.WriteFile(specPath, []byte(largeSpec), 0644)
	s.Require().NoError(err)

	req := httptest.NewRequest(http.MethodGet, "/swagger.yml", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err = s.server.serveSwaggerSpec(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Contains(rec.Body.String(), "Large CAS Server API")
}

// TestServeSwaggerUITemplateVariables tests that template variables are correctly set
func (s *SwaggerTestSuite) TestServeSwaggerUITemplateVariables() {
	tmplContent := `Title: {{.Title}}
SwaggerPath: {{.SwaggerPath}}`

	tmplPath := filepath.Join(s.webDir, "swagger-ui.html")
	err := os.WriteFile(tmplPath, []byte(tmplContent), 0644)
	s.Require().NoError(err)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err = s.server.serveSwaggerUI(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)

	body := rec.Body.String()
	s.Equal("Title: CAS Server API Documentation\nSwaggerPath: /swagger.yml", body)
}

// TestServeSwaggerUINoPermissionToTemplate tests template file permission error
func (s *SwaggerTestSuite) TestServeSwaggerUINoPermissionToTemplate() {
	if os.Getuid() == 0 {
		s.T().Skip("Skipping permission test as root user")
		return
	}

	tmplPath := filepath.Join(s.webDir, "swagger-ui.html")
	err := os.WriteFile(tmplPath, []byte("test"), 0000) // No permissions
	s.Require().NoError(err)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err = s.server.serveSwaggerUI(c)
	s.NoError(err)
	s.Equal(http.StatusInternalServerError, rec.Code)
	s.Contains(rec.Body.String(), "Failed to load template")
}

// TestServeSwaggerSpecNoPermissionToFile tests spec file permission error
func (s *SwaggerTestSuite) TestServeSwaggerSpecNoPermissionToFile() {
	if os.Getuid() == 0 {
		s.T().Skip("Skipping permission test as root user")
		return
	}

	specPath := filepath.Join(s.webDir, "swagger.yml")
	err := os.WriteFile(specPath, []byte("test spec"), 0000) // No permissions
	s.Require().NoError(err)

	req := httptest.NewRequest(http.MethodGet, "/swagger.yml", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err = s.server.serveSwaggerSpec(c)
	s.Error(err) // Should return permission error
}

// TestServeSwaggerUIWithCustomWebDir tests swagger UI with custom web directory
func (s *SwaggerTestSuite) TestServeSwaggerUIWithCustomWebDir() {
	// Create a different web directory
	customWebDir, err := os.MkdirTemp("", "custom-web-*")
	s.Require().NoError(err)
	defer os.RemoveAll(customWebDir)

	// Create template in custom directory
	tmplContent := `Custom Title: {{.Title}}`
	tmplPath := filepath.Join(customWebDir, "swagger-ui.html")
	err = os.WriteFile(tmplPath, []byte(tmplContent), 0644)
	s.Require().NoError(err)

	// Create server with custom web directory
	customServer := NewCASServer(s.tempDir, customWebDir, "custom-v1.0.0", s.mockStore)
	customServer.setupRoutes()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := customServer.echo.NewContext(req, rec)

	err = customServer.serveSwaggerUI(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Contains(rec.Body.String(), "Custom Title: CAS Server API Documentation")
}

// TestServeSwaggerSpecBinaryContent tests swagger spec with binary content
func (s *SwaggerTestSuite) TestServeSwaggerSpecBinaryContent() {
	// Create file with binary content (which shouldn't be valid YAML/JSON but tests file serving)
	binaryContent := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD}
	specPath := filepath.Join(s.webDir, "swagger.yml")
	err := os.WriteFile(specPath, binaryContent, 0644)
	s.Require().NoError(err)

	req := httptest.NewRequest(http.MethodGet, "/swagger.yml", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err = s.server.serveSwaggerSpec(c)
	s.NoError(err)
	s.Equal(http.StatusOK, rec.Code)
	s.Equal(binaryContent, rec.Body.Bytes())
}

// TestServeSwaggerUIConcurrentRequests tests concurrent swagger UI requests
func (s *SwaggerTestSuite) TestServeSwaggerUIConcurrentRequests() {
	tmplContent := `Title: {{.Title}}`
	tmplPath := filepath.Join(s.webDir, "swagger-ui.html")
	err := os.WriteFile(tmplPath, []byte(tmplContent), 0644)
	s.Require().NoError(err)

	numRequests := 5
	results := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			c := s.server.echo.NewContext(req, rec)

			err := s.server.serveSwaggerUI(c)
			success := (err == nil && rec.Code == http.StatusOK)
			results <- success
		}()
	}

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		success := <-results
		s.True(success)
	}
}

// TestServeSwaggerSpecConcurrentRequests tests concurrent swagger spec requests
func (s *SwaggerTestSuite) TestServeSwaggerSpecConcurrentRequests() {
	specContent := "openapi: 3.0.0\ninfo:\n  title: Test API"
	specPath := filepath.Join(s.webDir, "swagger.yml")
	err := os.WriteFile(specPath, []byte(specContent), 0644)
	s.Require().NoError(err)

	numRequests := 5
	results := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodGet, "/swagger.yml", nil)
			rec := httptest.NewRecorder()
			c := s.server.echo.NewContext(req, rec)

			err := s.server.serveSwaggerSpec(c)
			success := (err == nil && rec.Code == http.StatusOK)
			results <- success
		}()
	}

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		success := <-results
		s.True(success)
	}
}

// TestServeSwaggerUITemplateDirectory tests when template path is a directory
func (s *SwaggerTestSuite) TestServeSwaggerUITemplateDirectory() {
	// Create directory instead of file
	tmplPath := filepath.Join(s.webDir, "swagger-ui.html")
	err := os.Mkdir(tmplPath, 0755)
	s.Require().NoError(err)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := s.server.echo.NewContext(req, rec)

	err = s.server.serveSwaggerUI(c)
	s.NoError(err)
	s.Equal(http.StatusInternalServerError, rec.Code)
	s.Contains(rec.Body.String(), "Failed to load template")
}

// TestSwaggerSuite runs the swagger test suite
func TestSwaggerSuite(t *testing.T) {
	suite.Run(t, new(SwaggerTestSuite))
}
