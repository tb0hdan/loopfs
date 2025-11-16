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

## API Endpoints

See http://localhost:8080/ for the interactive API documentation.

## Storage implementation

CASd uses a simple file-based storage implementation. Files are stored in a directory structure based on their SHA256 hash.

For example, a file with the SHA256 hash `abcdef1234567890...` would be stored in the following path:

```bash
/data/cas/ab/cd/loopmount/1234567890...
```

Where `/data/cas/ab/cd/loop.img` is the loop file for the content. It gets created on the first upload of the content.
