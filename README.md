# LoopFS

**Content Addressable Storage (CAS) server with loop filesystem architecture**

[![Go Version](https://img.shields.io/badge/go-1.25+-blue.svg)](https://golang.org)
[![Platform](https://img.shields.io/badge/platform-linux-green.svg)](https://www.linux.org)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

## Overview

LoopFS is a Content Addressable Storage server that uses SHA256 hashing for data integrity and deduplication. Built with Linux loop filesystems, it provides efficient storage organization.

### Key Features

- **Content-based addressing** using SHA256 cryptographic hashes
- **Zero-duplicate storage** with automatic deduplication
- **Loop filesystem architecture** for optimized storage performance
- **RESTful API** with comprehensive OpenAPI documentation
- **Load balancer** support for horizontal scaling
- **Graceful shutdown** and proper resource management

## Quick Start

### Build

```bash
make build
```

### Run Server

```bash
# Core CAS server (requires root for loop mounting)
sudo ./build/casd -storage /data/cas -addr :8080

# Load balancer (optional)
./build/cas-balancer -backends http://server1:8080,http://server2:8080 -addr :8081
```

### Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `-storage` | `/data/cas` | Storage directory path |
| `-addr` | `127.0.0.1:8080` | Server bind address |
| `-loop-size` | `1024` | Loop file size in MB |
| `-mount-ttl` | `5m` | Mount cache duration |

## API Usage

Interactive API documentation available at: **http://localhost:8080/**

### Core Operations

```bash
# Upload file
curl -X POST -F "file=@document.pdf" http://localhost:8080/file/upload

# Download file
curl http://localhost:8080/file/{hash}/download > file.pdf

# Get file info
curl http://localhost:8080/file/{hash}/info

# Delete file
curl -X DELETE http://localhost:8080/file/{hash}/delete

# Node status
curl http://localhost:8080/node/info
```

## Architecture

LoopFS uses a sophisticated loop filesystem storage approach:

```
/data/cas/ab/cd/loop.img    # 1GB ext4 loop file
    └── mounted at /data/cas/ab/cd/loopef/
        └── ef/12/456789...  # File stored inside loop filesystem
```

**Hash Partitioning**: `abcdef123456789...`
- **Loop path**: `ab/cd` (first 4 chars)
- **Mount point**: `ef` (chars 5-6)
- **Internal structure**: `ef/12` (chars 5-8)
- **Filename**: `456789...` (remaining chars)

## Testing

### Built-in Test Suite

```bash
make test    # Run test suite (67.7% coverage)
make lint    # Code quality checks
```

### Load Testing

```bash
# Build test utility
go build ./cmd/cas-test

# Run comprehensive tests
./cas-test -server http://localhost:8080 -passes 100 -parallel-full 20
```

## Requirements

- **OS**: Linux (loop device support required)
- **Privileges**: Root access (for loop mounting)
- **Go**: 1.25+ (for building)
- **Dependencies**: `dd`, `mkfs.ext4`, `mount`, `umount`, `df`

## Documentation

| Document | Description |
|----------|-------------|
| [Loop Store Architecture](docs/LOOP_STORE_ARCHITECTURE.md) | Technical architecture and design |
| [Project Notes](docs/PROJECT_NOTES.md) | Implementation details and history |
| [Test Metrics](docs/cas-test-metrics-demo.md) | Performance testing guide |

## Contributing

1. Ensure all tests pass: `make test`
2. Run linting: `make lint`
3. Format code: `find ./ -name "*.go" -exec goimports -w {} \;`
4. Update documentation as needed

---

**Note**: LoopFS requires root privileges for loop device operations and is designed for Linux environments.
