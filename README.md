# LoopFS - Content Addressable Storage (CAS) File Server

## Project Description

LoopFS is a Content Addressable Storage (CAS) file server that stores and retrieves files based on their SHA256 
cryptographic hash. Linux only.

## Build && Run

```bash
make build
```

```bash
sudo build/casd -storage /data/cas -addr :8080
``` 

Loop images stay mounted for five minutes after the last access to speed up future requests. Override this idle timeout with `-mount-ttl`, for example `-mount-ttl 10m` to cache mounts for ten minutes.

## cas-test helper

The `cmd/cas-test` utility mirrors the legacy `docs/scripts/test_casd.sh` workflow and adds stress scenarios. Build it with `go build ./cmd/cas-test` (or via `make build`) and run it while `casd` is serving traffic. It performs:
- A single end-to-end upload/info/download/delete pass.
- Multiple sequential passes (default 10).
- Concurrent `/file/{hash}/info` requests against the same uploaded file.
- Concurrent `/file/{hash}/info` requests for multiple unique files.
- Parallel full passes for different files (default 10 workers).

Flags like `-server`, `-size`, `-passes`, `-parallel-info`, and `-parallel-full` let you point at alternate clusters, vary the payload size, and tune concurrency.

## API Endpoints

See http://localhost:8080/ for the interactive API documentation.

## Storage implementation

CASd uses a simple file-based storage implementation. Files are stored in a directory structure based on their SHA256 hash.

For example, a file with the SHA256 hash `abcdef1234567890...` would be stored in the following path:

```bash
/data/cas/ab/cd/loopmount/1234567890...
```

Where `/data/cas/ab/cd/loop.img` is the loop file for the content. It gets created on the first upload of the content.

## Documentation

- **[Loop Store Architecture](docs/LOOP_STORE_ARCHITECTURE.md)** - Comprehensive documentation of the loop filesystem-based storage system, including architecture diagrams, data flow charts, and technical implementation details.
- **[Project Notes](docs/PROJECT_NOTES.md)** - Project overview, current implementation status, and development history.
- **[CAS test utility and sample metrics](docs/cas-test-metrics-demo.md)** - Documentation on the cas-test utility, including usage instructions and sample performance metrics.
