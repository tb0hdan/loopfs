package balancer

import "errors"

var (
	// ErrNoBackendAvailable is returned when no backend is available to handle the request.
	ErrNoBackendAvailable = errors.New("no backend available")

	// ErrAllBackendsDown is returned when all backends are offline.
	ErrAllBackendsDown = errors.New("all backends are offline")

	// ErrNoBackendWithSpace is returned when no backend has enough space for the upload.
	ErrNoBackendWithSpace = errors.New("no backend has enough space")
)
