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
│   │   └── loop/            # Loop filesystem storage implementation
│   │       ├── loop.go      # Core loop management and mounting
│   │       ├── upload.go    # File upload to loop filesystems
│   │       ├── download.go  # File download from loop filesystems
│   │       ├── exists.go    # Loop file existence checking
│   │       ├── get_file_info.go # Metadata from loop filesystems
│   │       └── validate_hash.go # Hash validation utilities
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
- **Description**: Returns metadata about a stored file including disk usage information
- **Parameters**:
  - `hash`: 64-character hexadecimal SHA256 hash
- **Response**: JSON with hash, size, creation timestamp, and loop filesystem disk usage
- **Response Fields**:
  - `hash`: SHA256 hash of the file
  - `size`: File size in bytes
  - `created_at`: File creation/modification timestamp
  - `space_used`: Space used in the file's loop filesystem (bytes)
  - `space_available`: Space available in the file's loop filesystem (bytes)
- **Status Codes**:
  - 200: Success - returns metadata
  - 400: Bad request - invalid hash format
  - 404: Not found - file doesn't exist
  - 500: Internal server error

### 5. DELETE `/file/{hash}/delete` - Delete File
- **Description**: Deletes a file from the CAS storage
- **Parameters**:
  - `hash`: 64-character hexadecimal SHA256 hash
- **Response**: JSON with success message and hash
- **Status Codes**:
  - 200: Success - file deleted
  - 400: Bad request - invalid hash format
  - 404: Not found - file doesn't exist
  - 500: Internal server error

## Storage Architecture

Files are stored in a hierarchical directory structure to avoid filesystem limitations with too many files in a single directory:

```
data/
  a1/                          # First 2 chars of hash
    ff/                        # Next 2 chars of hash
      f0ffefb9eace...          # Full hash as filename
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
# Must run as root for loop device operations
sudo ./build/casd -storage /data/cas -addr :8080

# Custom configuration with specific loop file size
sudo ./build/casd -storage /custom/storage -addr :8090 -web ./web -loop-size 2048

# Available command-line options:
# -storage: Storage directory path (default: "/data/cas")
# -web: Web assets directory path (default: "web")
# -addr: Server bind address (default: "127.0.0.1:8080")
# -loop-size: Loop file size in MB (default: 1024)
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
# Returns: {
#   "hash":"abc123...",
#   "size":1024,
#   "created_at":"2025-11-15T12:00:00Z",
#   "space_used":1073741824,
#   "space_available":10737418240
# }
```

4. **Delete a file**:
```bash
curl -X DELETE http://localhost:8080/file/abc123.../delete
# Returns: {"message":"File deleted successfully","hash":"abc123..."}
```

5. **View API Documentation**:
Open http://localhost:8080 in a web browser

## Current Implementation Status (Updated November 16, 2025)

### Project Version: v1.0.1

### Latest Architectural Changes (Commits 18a6bba - "Another bump")

The project has been completely rewritten to implement a **loop filesystem-based storage** approach:

#### Loop File Storage Architecture

**Key Innovation**: LoopFS now uses **Linux loop filesystems** for content storage instead of traditional file storage. This provides:

1. **Loop File Organization**: Each hash prefix creates a shared ext4 loop filesystem
   - Hash `abcdef123...` → `/data/cas/ab/cd/loop.img` (default 1GB ext4 loop file)
   - Multiple files can share the same loop filesystem based on hash prefix
   - Files stored inside mounted loop filesystem with hierarchical structure
   - Automatic mount/unmount during operations with reference counting

2. **Advanced Hash Partitioning**: Multi-level directory structure:
   - **Loop Level**: First 4 chars (`ab/cd`) determine loop file location
   - **Mount Point**: Next 2 chars (`ef`) used for mount point naming (`loopef`)
   - **Internal Level**: Chars 5-8 (`ef/12`) create subdirs inside loop filesystem
   - **File Level**: Remaining chars (from position 9 onwards) become actual filename

3. **Dynamic Filesystem Management**:
   - Loop files created on-demand using `dd` + `mkfs.ext4`
   - Automatic mounting/unmounting with `mount -o loop`
   - Reference counting for concurrent operations on same loop file
   - Per-loop-file creation mutex to prevent race conditions
   - Mount point validation with `mountpoint` command
   - Cleanup of creation mutexes after successful creation

#### Code Architecture Improvements

1. **Modular Loop Implementation** (`pkg/store/loop/`):
   - **`loop.go`**: Core loop filesystem management with reference counting and mounting logic
   - **`upload.go`**: File upload with loop mounting and hierarchical storage
   - **`download.go`**: Temporary file extraction from mounted loops
   - **`delete.go`**: File deletion from loop filesystems
   - **`exists.go`**: Loop file existence checking
   - **`get_file_info.go`**: Metadata retrieval including disk usage from loop filesystems
   - **`validate_hash.go`**: SHA256 hash validation
   - **`disk_usage.go`**: Disk usage statistics for loop filesystems

2. **Enhanced Server Requirements**:
   - **Root Access Required**: Must run as root for loop mounting operations
   - **Linux-Only**: Depends on Linux loop device functionality
   - **Storage Path Change**: Default storage changed to `/data/cas`
   - **Address Binding**: Server now binds to configurable address (`:8080`)

3. **Advanced Configuration**:
   - **Loop File Size**: Configurable via `-loop-size` flag (default: 1GB)
   - **Storage Directory**: Configurable via `-storage` flag (default: `/data/cas`)
   - **Address Binding**: Configurable via `-addr` flag (default: `127.0.0.1:8080`)

#### Technical Implementation Details

- **Loop File Creation**: `dd if=/dev/zero of=loop.img bs=1M count=<size>; mkfs.ext4 -q loop.img`
- **Mount Operations**: `mount -o loop /path/to/loop.img /mount/point`
- **File Storage**: Files stored as `/mount/point/ef/12/<remaining_hash>` within loop filesystem
- **Cleanup**: Reference-counted unmounting after operations with proper error handling
- **Concurrency Protection**:
  - Global mount mutex for mount/unmount operations
  - Per-loop-file creation mutex to prevent duplicate creation
  - Reference counting for managing concurrent access to same loop file
  - Creation mutex cleanup after successful loop file creation
- **Command Timeout**: 30-second timeout for all exec commands (dd, mkfs.ext4, mount, umount)
- **Constants**:
  - `maxLoopDevices`: 65535 (Linux kernel limit)
  - `minHashLength`: 4 (minimum for loop file path)
  - `minHashSubDir`: 8 (minimum for subdirectory structure)
  - `hashLength`: 64 (standard SHA256 hex length)
  - `dirPerm`: 0750 (directory permissions)
  - `blockSize`: 1M (dd block size)

### Testing and Quality Assurance

- **Test Coverage**: Currently 0.0% - tests need implementation for loop filesystem operations
- **Build System**: Makefile supports `build`, `test`, `lint`, and `tools` targets
- **Binary Output**: Compiled to `build/casd` (approximately 15MB executable)
- **Dependencies**: Go 1.22.3, Echo v4.12.0, Zerolog v1.33.0, standard Linux utilities
- **Linting**: Uses golangci-lint for code quality checks

### Operational Requirements

- **Operating System**: Linux only (requires loop device support)
- **Kernel Requirements**: Linux kernel with loop device support enabled
- **Privileges**: Must run as root (loop mounting requires elevated privileges)
- **Disk Space**: Each hash prefix combination (first 4 chars) creates a loop file (default 1GB, configurable)
- **Mount Points**: Dynamic mount point creation under `/data/cas/ab/cd/loopef/`
- **File Descriptors**: Adequate ulimit for handling multiple mounted filesystems
- **System Commands Required**:
  - `dd`: For creating loop files
  - `mkfs.ext4`: For formatting loop filesystems
  - `mount`: For mounting loop filesystems
  - `umount`: For unmounting loop filesystems
  - `mountpoint`: For checking mount status
  - `df`: For disk usage statistics

## Security Considerations

- Hash validation prevents directory traversal attacks
- File existence check prevents overwriting existing files
- No file execution - purely storage and retrieval
- Content-based addressing ensures data integrity
- Atomic file operations prevent corruption during uploads
- Temporary file handling with proper cleanup
- Loop filesystem isolation provides additional security boundary
- Mount operations require root privileges (security through privilege separation)
- Input validation with gosec linting for command injection prevention
- Directory permissions set to 0750 for restricted access

## Future Enhancements

Potential improvements for the CAS server:

1. **Authentication & Authorization**: Add user authentication and access control
2. **Compression**: Automatic compression for stored files within loop filesystems
3. **Replication**: Support for distributed storage and replication of loop files
4. **Garbage Collection**: Clean up empty or orphaned loop filesystems
5. **Metadata Storage**: Store additional metadata in a database
6. **Batch Operations**: Support for uploading/downloading multiple files
7. **WebSocket Support**: Real-time notifications for file uploads
8. **Rate Limiting**: Prevent abuse through request throttling
9. **HTTPS Support**: TLS encryption for secure transfers
10. **Metrics & Monitoring**: Prometheus metrics for monitoring
11. **Loop File Management**: Dynamic resizing of loop filesystems based on usage
12. **Alternative Filesystems**: Support for other filesystems (btrfs, xfs) besides ext4
13. **Non-root Operation**: Investigate user namespace mounting for non-root operation
14. **Performance Optimization**: Implement caching layer for frequently accessed files
15. **Backup & Recovery**: Automated backup of loop filesystems
