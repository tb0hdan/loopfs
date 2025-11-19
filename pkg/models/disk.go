package models

// DiskUsage represents disk space information.
type DiskUsage struct {
	SpaceUsed      int64 `json:"space_used"`      // Bytes used
	SpaceAvailable int64 `json:"space_available"` // Bytes available
	TotalSpace     int64 `json:"total_space"`     // Total bytes
}
