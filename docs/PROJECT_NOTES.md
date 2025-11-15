# LoopFS - Content Addressable Storage (CAS) File Server

## Project Description

LoopFS is a Content Addressable Storage (CAS) file server that stores and retrieves files based on their SHA256 cryptographic hash. This approach ensures data integrity, eliminates duplicate storage, and provides a unique identifier for each file based on its content.

### Key Features

- **Content-based addressing**: Files are stored and retrieved using their SHA256 hash
- **Deduplication**: Files with identical content are stored only once
- **RESTful API**: Simple HTTP endpoints for file operations
- **OpenAPI/Swagger Documentation**: Interactive API documentation served at root
- **Configurable storage**: Command-line option to specify storage directory
- **Efficient storage structure**: Files organized in a hierarchical directory structure based on hash prefix
- **Modular architecture**: Clean separation between server, storage, and logging components
- **Structured logging**: Comprehensive logging with goroutine IDs using zerolog
- **Graceful shutdown**: Proper server shutdown with filesystem buffer flushing

## Project Structure

```
LoopFS/
├── cmd/
│   └── casd/
│       ├── main.go          # Main entry point
│       └── VERSION          # Version file (embedded)
├── pkg/                     # Core packages
│   ├── server/              # HTTP server implementation
│   │   ├── server.go        # Server setup and lifecycle
│   │   ├── upload.go        # File upload handler
│   │   ├── download.go      # File download handler
│   │   ├── file_info.go     # File metadata handler
│   │   └── swagger.go       # Swagger UI and spec handlers
│   ├── store/               # Storage abstraction
│   │   ├── store.go         # Store interface and types
│   │   └── loop/
│   │       └── loop.go      # Loop storage implementation
│   └── log/
│       └── logger.go        # Structured logging setup
├── web/                     # Web assets
│   ├── swagger-ui.html      # Swagger UI template
│   ├── swagger-ui.tmpl      # Swagger template
│   └── swagger.yml          # OpenAPI specification
├── docs/                    # Documentation
│   ├── PROJECT_NOTES.md     # This file
│   └── scripts/             # Test scripts
├── build/                   # Build output directory
│   ├── casd                 # Compiled binary
│   └── data/                # Default storage directory
├── go.mod                   # Go module definition
├── go.sum                   # Go dependencies lockfile
├── Makefile                 # Build, test, lint commands
├── README.md                # Basic project information
├── CLAUDE.md                # Claude instructions
├── GEMINI.md                # Gemini instructions
└── AGENTS.md                # Agent instructions

```

## API Endpoints

### 1. GET `/` - API Documentation
- Serves interactive Swagger UI with complete API documentation
- Provides a web interface to test all endpoints

### 2. POST `/file/upload` - Upload File
- **Description**: Uploads a file to the CAS storage
- **Request**: Multipart form data with "file" field
- **Response**: JSON with the file's SHA256 hash
- **Status Codes**:
  - 200: Success - returns hash
  - 400: Bad request - missing file parameter
  - 409: Conflict - file already exists
  - 500: Internal server error

### 3. GET `/file/{hash}/download` - Download File
- **Description**: Downloads a file by its SHA256 hash
- **Parameters**:
  - `hash`: 64-character hexadecimal SHA256 hash
- **Response**: File content (binary)
- **Status Codes**:
  - 200: Success - returns file content
  - 400: Bad request - invalid hash format
  - 404: Not found - file doesn't exist

### 4. GET `/file/{hash}/info` - Get File Metadata
- **Description**: Returns metadata about a stored file
- **Parameters**:
  - `hash`: 64-character hexadecimal SHA256 hash
- **Response**: JSON with hash, size, and creation timestamp
- **Status Codes**:
  - 200: Success - returns metadata
  - 400: Bad request - invalid hash format
  - 404: Not found - file doesn't exist
  - 500: Internal server error

## Storage Architecture

Files are stored in a hierarchical directory structure to avoid filesystem limitations with too many files in a single directory:

```
data/
  a1/                          # First 2 chars of hash
    ff/                        # Next 2 chars of hash
      a1fff0ffefb9eace...     # Full hash as filename
```

This structure:
- Distributes files across 65,536 possible directory combinations (256 × 256)
- Prevents directory listing performance issues
- Maintains easy file location based on hash

## Technical Implementation

### Technologies Used
- **Language**: Go 1.25.4
- **Web Framework**: Labstack Echo v4
- **Logging**: Zerolog with console writer and goroutine ID tracking
- **Hashing**: SHA256 (crypto/sha256)
- **Documentation**: OpenAPI 3.0.0 / Swagger UI

### Architecture Overview

The project follows a clean, modular architecture with clear separation of concerns:

- **`cmd/casd/main.go`**: Entry point, handles CLI flags and bootstraps the server
- **`pkg/server/`**: HTTP server implementation with Echo framework
- **`pkg/store/`**: Storage abstraction layer with pluggable implementations
- **`pkg/log/`**: Structured logging with goroutine ID tracking

### Core Components

1. **Main Entry Point** (`cmd/casd/main.go:21-46`):
   - Parses command-line flags (storage, web, port)
   - Creates storage and web directories
   - Initializes loop store and CAS server
   - Starts server with graceful shutdown handling

2. **CASServer** (`pkg/server/server.go:24-40`):
   - Main server struct with configurable storage and web directories
   - Uses dependency injection with store.Store interface
   - Handles graceful shutdown with filesystem buffer flushing
   - Manages Echo HTTP server lifecycle

3. **Store Interface** (`pkg/store/store.go:20-39`):
   - Defines storage operations: Upload, Download, GetFileInfo, Exists, ValidateHash
   - Provides custom error types: FileExistsError, FileNotFoundError, InvalidHashError
   - Enables pluggable storage implementations

4. **Loop Store Implementation** (`pkg/store/loop/loop.go:22-202`):
   - Implements the Store interface for hierarchical file storage
   - Hash-based directory structure (first 2 chars / next 2 chars / full hash)
   - Atomic file operations with temporary files
   - Comprehensive input validation and error handling

5. **HTTP Handlers** (`pkg/server/`):
   - **Upload** (`upload.go:13-61`): Multipart file upload with hash calculation
   - **Download** (`download.go`): File retrieval by hash with streaming
   - **File Info** (`file_info.go`): Metadata retrieval with timestamps
   - **Swagger** (`swagger.go`): Embedded API documentation and UI

6. **Structured Logging** (`pkg/log/logger.go:35-78`):
   - Zerolog-based logging with console output formatting
   - Automatic goroutine ID injection for request tracing
   - Configurable log levels with colored terminal output
   - Global logger instance with consistent formatting

### Processing Flows

1. **File Upload Flow**:
   - Receive multipart form data → Extract file → Create temp file
   - Calculate SHA256 while copying → Check if file exists
   - Create directory structure → Move temp file to final location
   - Return hash or appropriate error response

2. **File Download Flow**:
   - Validate hash format → Check file existence
   - Stream file content directly to client
   - Handle not found and validation errors

3. **Server Lifecycle**:
   - Initialize logger → Parse CLI flags → Create directories
   - Setup store → Create server → Start HTTP listener
   - Handle shutdown signals → Graceful Echo shutdown → Flush filesystem buffers

## Usage

### Development Commands
The project includes a Makefile with common development commands:

```bash
# Run all checks (lint, test, build)
make all

# Build the binary
make build

# Run tests with coverage
make test

# Run linter
make lint

# Install development tools
make tools
```

### Building the Server
```bash
# Using Makefile (recommended)
make build

# Using go build directly
go build -o build/casd cmd/casd/*.go
```

### Running the Server
```bash
# Default configuration (port 8080, build/data storage)
./build/casd

# Custom storage directory and port
./build/casd -storage /custom/storage -port 8090 -web ./web

# Available command-line options:
# -storage: Storage directory path (default: "build/data")
# -web: Web assets directory path (default: "web")
# -port: Server port (default: "8080")
```

### Example Operations

1. **Upload a file**:
```bash
curl -X POST -F "file=@document.pdf" http://localhost:8080/file/upload
# Returns: {"hash":"abc123..."}
```

2. **Download a file**:
```bash
curl http://localhost:8080/file/abc123.../download > downloaded.pdf
```

3. **Get file information**:
```bash
curl http://localhost:8080/file/abc123.../info
# Returns: {"hash":"abc123...","size":1024,"created_at":"2025-11-15T12:00:00Z"}
```

4. **View API Documentation**:
Open http://localhost:8080 in a web browser

## Current Implementation Status

### Recent Architectural Changes (v1.0.0)

The project has been significantly refactored from a single-file implementation to a modular, production-ready architecture:

1. **Modularization**: Code split into logical packages (`pkg/server/`, `pkg/store/`, `pkg/log/`)
2. **Interface-Based Design**: Storage operations abstracted through `store.Store` interface
3. **Professional Logging**: Structured logging with zerolog, goroutine tracking, and consistent formatting
4. **Improved Error Handling**: Custom error types with proper HTTP status mapping
5. **Build Infrastructure**: Comprehensive Makefile with lint, test, and coverage tools
6. **Version Management**: Embedded version string from `cmd/casd/VERSION` file
7. **Web Assets**: Separate directory structure for Swagger UI templates and specifications

### Testing and Quality Assurance

- **Unit Tests**: Test coverage tracking with HTML reports (`build/coverage.html`)
- **Linting**: golangci-lint integration for code quality
- **Build Automation**: Makefile targets for all development workflows
- **Error Handling**: Comprehensive error scenarios with appropriate HTTP responses

### Dependencies

- **Echo v4**: High-performance HTTP web framework
- **Zerolog**: Structured logging with minimal allocation overhead
- **Standard Library**: Crypto (SHA256), OS operations, HTTP handling

## Security Considerations

- Hash validation prevents directory traversal attacks
- File existence check prevents overwriting existing files
- No file execution - purely storage and retrieval
- Content-based addressing ensures data integrity
- Atomic file operations prevent corruption during uploads
- Temporary file handling with proper cleanup

## Future Enhancements

Potential improvements for the CAS server:

1. **Authentication & Authorization**: Add user authentication and access control
2. **Compression**: Automatic compression for stored files
3. **Replication**: Support for distributed storage and replication
4. **Garbage Collection**: Remove orphaned files not referenced by any index
5. **Metadata Storage**: Store additional metadata in a database
6. **Batch Operations**: Support for uploading/downloading multiple files
7. **WebSocket Support**: Real-time notifications for file uploads
8. **Rate Limiting**: Prevent abuse through request throttling
9. **HTTPS Support**: TLS encryption for secure transfers
10. **Metrics & Monitoring**: Prometheus metrics for monitoring